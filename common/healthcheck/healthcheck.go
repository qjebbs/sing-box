package healthcheck

import (
	"context"
	"errors"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/batch"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ adapter.Service = (*HealthCheck)(nil)

// errors
var (
	ErrNoNetWork = errors.New("no network")
)

// HealthCheck is the health checker for balancers
type HealthCheck struct {
	Storage *Storages

	router         adapter.Router
	logger         log.Logger
	globalHistory  *urltest.HistoryStorage
	providers      []adapter.Provider
	providersByTag map[string]adapter.Provider
	detourOf       adapter.Outbound

	options *option.HealthCheckOptions

	cancel context.CancelFunc
}

// New creates a new HealthPing with settings.
//
// The globalHistory is optional and is only used to sync latency history
// between different health checkers. Each HealthCheck will maintain its own
// history storage since different ones can have different check destinations,
// sampling numbers, etc.
func New(
	router adapter.Router,
	providers []adapter.Provider, providersByTag map[string]adapter.Provider,
	options *option.HealthCheckOptions, logger log.Logger,
) *HealthCheck {
	if options == nil {
		options = &option.HealthCheckOptions{}
	}
	if options.Destination == "" {
		//goland:noinspection HttpUrlsUsage
		options.Destination = "http://www.gstatic.com/generate_204"
	}
	if options.Interval < option.Duration(10*time.Second) {
		options.Interval = option.Duration(10 * time.Second)
	}
	if options.Sampling <= 0 {
		options.Sampling = 10
	}
	var globalHistory *urltest.HistoryStorage
	if clashServer := router.ClashServer(); clashServer != nil {
		globalHistory = clashServer.HistoryStorage()
	}
	return &HealthCheck{
		router:         router,
		logger:         logger,
		globalHistory:  globalHistory,
		providers:      providers,
		providersByTag: providersByTag,
		options:        options,
		Storage: NewStorages(
			options.Sampling,
			time.Duration(options.Sampling+1)*time.Duration(options.Interval),
		),
	}
}

// Start starts the health check service, implements adapter.Service
func (h *HealthCheck) Start() error {
	if h.cancel != nil {
		return nil
	}
	if h.options.DetourOf != "" {
		outbound, ok := h.router.Outbound(h.options.DetourOf)
		if !ok {
			return E.New("detour_of: outbound not found: ", h.options.DetourOf)
		}
		h.detourOf = outbound
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		// wait for all providers to be ready
		for _, p := range h.providers {
			p.Wait()
		}
		go h.checkLoop(ctx)
		go h.cleanupLoop(ctx, 8*time.Hour)
	}()
	return nil
}

// Close stops the health check service, implements adapter.Service
func (h *HealthCheck) Close() error {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	return nil
}

// ReportFailure reports a failure of the node
func (h *HealthCheck) ReportFailure(outbound adapter.Outbound) {
	if _, ok := outbound.(adapter.OutboundGroup); ok {
		return
	}
	tag := outbound.Tag()
	history := h.Storage.Latest(tag)
	if history == nil || history.Delay != Failed {
		// don't put more failed records if it's known failed,
		// or it will interferes with the max_fail assertion
		h.Storage.Put(tag, Failed)
	}
}

func (h *HealthCheck) checkLoop(ctx context.Context) {
	go h.CheckAll()
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go h.CheckAll()
		}
	}
}

// CheckAll performs checks for nodes of all providers
func (h *HealthCheck) CheckAll() {
	batch, _ := batch.New(context.Background(), batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	ctx := NewContext(h.options.Connectivity)
	for _, provider := range h.providers {
		h.checkProvider(ctx, batch, provider)
	}
	batch.Wait()
}

// CheckProvider performs checks for nodes of the provider
func (h *HealthCheck) CheckProvider(tag string) {
	provider, ok := h.providersByTag[tag]
	if !ok {
		return
	}
	batch, _ := batch.New(context.Background(), batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	ctx := NewContext(h.options.Connectivity)
	h.checkProvider(ctx, batch, provider)
	batch.Wait()
}

// CheckOutbound performs check for the specified node
func (h *HealthCheck) CheckOutbound(tag string) (uint16, error) {
	ctx := NewContext(h.options.Connectivity)
	outbound, ok := h.outbound(tag)
	if !ok {
		return 0, E.New("outbound not found")
	}
	outbound, err := adapter.RealOutbound(h.router, outbound)
	if err != nil {
		return 0, err
	}
	return h.checkOutbound(ctx, outbound)
}

func (h *HealthCheck) checkProvider(ctx *Context, batch *batch.Batch[uint16], provider adapter.Provider) {
	for _, outbound := range provider.Outbounds() {
		outbound, err := adapter.RealOutbound(h.router, outbound)
		if err != nil {
			continue
		}
		tag := outbound.Tag()
		batch.Go(
			tag,
			func() (uint16, error) {
				if ctx.Checked(tag) {
					return 0, nil
				}
				h.checkOutbound(ctx, outbound)
				return 0, nil
			},
		)
	}
}

func (h *HealthCheck) outbound(tag string) (adapter.Outbound, bool) {
	for _, provider := range h.providers {
		outbound, ok := provider.Outbound(tag)
		if ok {
			return outbound, ok
		}
	}
	return nil, false
}

// checkOutbound performs a check for the specified outbound unconditionally,
// which means:
// It performs a check to the target as is, no matter it's a single outbound
// or an outbound group, call adapter.RealOutbound() to get the real outbound
// before calling this function if you need to check the real outbound other
// than a group.
// It will check the outbound again even if it is already checked according to
// the context, take care of the checked status before calling this function if
// you don't want to check again.
//
// It returns the RTT of the check, and reports the checked status and network
// connectivity to the ctx.
func (h *HealthCheck) checkOutbound(ctx *Context, outbound adapter.Outbound) (uint16, error) {
	tag := outbound.Tag()
	ctx.ReportChecked(tag)
	testCtx, cancel := context.WithTimeout(context.Background(), C.TCPTimeout)
	defer cancel()
	if h.detourOf != nil {
		// always check if the detour chain redirectable, because if the detour
		// chain contains outbound groups (though it's rare), the real nodes of
		// the chain may be changed from time to time
		if !checkRedirectable(h.detourOf) {
			h.logger.Warn(
				"detour_of: ignored due to no redirectable node found in the detour chain of '",
				h.options.DetourOf,
				"', forgot to add 'detour_redir' to the chain?",
			)
		} else {
			testCtx = dialer.WithRedirectDialer(testCtx, outbound)
			outbound = h.detourOf
		}
	}
	testCtx = log.ContextWithOverrideLevel(testCtx, log.LevelDebug)
	t, err := urltest.URLTest(testCtx, h.options.Destination, outbound)
	if err == nil {
		rtt := RTT(t)
		h.logger.Debug("outbound ", tag, " available: ", rtt)
		ctx.ReportConnected()
		h.Storage.Put(tag, rtt)
		if h.globalHistory != nil {
			h.globalHistory.StoreURLTestHistory(tag, &urltest.History{
				Time:  time.Now(),
				Delay: t,
			})
		}
		return t, nil
	}
	if !ctx.Connected() {
		return 0, ErrNoNetWork
	}
	h.logger.Debug("outbound ", tag, " unavailable: ", err)
	h.Storage.Put(tag, Failed)
	if h.globalHistory != nil {
		h.globalHistory.StoreURLTestHistory(tag, &urltest.History{
			Time:  time.Now(),
			Delay: 0,
		})
	}
	return 0, err
}

func (h *HealthCheck) cleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.cleanup()
		}
	}
}

func (h *HealthCheck) cleanup() {
	for _, tag := range h.Storage.List() {
		if _, ok := h.outbound(tag); !ok {
			h.Storage.Delete(tag)
		}
	}
}
