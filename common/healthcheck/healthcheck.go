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
	"github.com/sagernet/sing/service/pause"
)

var (
	_ adapter.Service                 = (*HealthCheck)(nil)
	_ adapter.InterfaceUpdateListener = (*HealthCheck)(nil)
)

// errors
var (
	ErrNoNetWork = errors.New("no network")
)

// HealthCheck is the health checker for balancers
type HealthCheck struct {
	Storage *Storages

	pauseManager pause.Manager

	router         adapter.Router
	logger         log.Logger
	globalHistory  *urltest.HistoryStorage
	providers      []adapter.Provider
	providersByTag map[string]adapter.Provider
	detourOf       []adapter.Outbound

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
	ctx context.Context,
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
		pauseManager: pause.ManagerFromContext(ctx),
	}
}

// Start starts the health check service, implements adapter.Service
func (h *HealthCheck) Start() error {
	if h.cancel != nil {
		return nil
	}
	if len(h.options.DetourOf) > 0 {
		for _, tag := range h.options.DetourOf {
			outbound, ok := h.router.Outbound(tag)
			if !ok {
				return E.New("detour_of: outbound not found: ", tag)
			}
			h.detourOf = append(h.detourOf, outbound)
		}
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

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (h *HealthCheck) InterfaceUpdated() {
	if h == nil {
		return
	}
	// h.logger.Info("[InterfaceUpdated]: CheckAll()")
	go h.CheckAll(context.Background())
	return
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
	go h.CheckAll(ctx)
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		h.pauseManager.WaitActive()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go h.CheckAll(ctx)
		}
	}
}

// CheckAll performs checks for nodes of all providers
func (h *HealthCheck) CheckAll(ctx context.Context) (map[string]uint16, error) {
	batch, _ := batch.New(ctx, batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	meta := NewMetaData(ctx, h.options.Connectivity)
	for _, provider := range h.providers {
		h.checkProvider(meta, batch, provider)
	}
	return convertResult(batch.WaitAndGetResult())
}

func convertResult(m map[string]batch.Result[uint16], err error) (map[string]uint16, error) {
	if err != nil {
		return nil, err
	}
	r := make(map[string]uint16)
	for k, v := range m {
		r[k] = v.Value
	}
	return r, nil
}

// CheckProvider performs checks for nodes of the provider
func (h *HealthCheck) CheckProvider(ctx context.Context, tag string) (map[string]uint16, error) {
	provider, ok := h.providersByTag[tag]
	if !ok {
		return nil, E.New("provider [", tag, "] not found")
	}
	batch, _ := batch.New(ctx, batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	meta := NewMetaData(ctx, h.options.Connectivity)
	h.checkProvider(meta, batch, provider)
	return convertResult(batch.WaitAndGetResult())
}

// CheckOutbound performs check for the specified node
func (h *HealthCheck) CheckOutbound(ctx context.Context, tag string) (uint16, error) {
	meta := NewMetaData(ctx, h.options.Connectivity)
	outbound, ok := h.outbound(tag)
	if !ok {
		return 0, E.New("outbound not found")
	}
	outbound, err := adapter.RealOutbound(h.router, outbound)
	if err != nil {
		return 0, err
	}
	return h.checkOutbound(meta, outbound)
}

func (h *HealthCheck) checkProvider(meta *MetaData, batch *batch.Batch[uint16], provider adapter.Provider) {
	for _, outbound := range provider.Outbounds() {
		outbound, err := adapter.RealOutbound(h.router, outbound)
		if err != nil {
			continue
		}
		tag := outbound.Tag()
		if meta.Checked(tag) {
			continue
		}
		meta.ReportChecked(tag)
		batch.Go(
			tag,
			func() (uint16, error) {
				return h.checkOutbound(meta, outbound)
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
func (h *HealthCheck) checkOutbound(meta *MetaData, outbound adapter.Outbound) (uint16, error) {
	tag := outbound.Tag()
	meta.ReportChecked(tag)
	testCtx, cancel := context.WithTimeout(meta.Context, C.TCPTimeout)
	defer cancel()
	if len(h.detourOf) > 0 {
		testCtx = dialer.WithChainRedirects(testCtx, makeOutboundChain(h.detourOf, outbound))
		outbound = h.detourOf[0]
	}
	testCtx = log.ContextWithOverrideLevel(testCtx, log.LevelDebug)
	t, err := urltest.URLTest(testCtx, h.options.Destination, outbound)
	if err == nil {
		rtt := RTT(t)
		h.logger.Debug("outbound ", tag, " available: ", rtt)
		meta.ReportConnected()
		h.Storage.Put(tag, rtt)
		if h.globalHistory != nil {
			h.globalHistory.StoreURLTestHistory(tag, &urltest.History{
				Time:  time.Now(),
				Delay: t,
			})
		}
		return t, nil
	}
	if !meta.Connected() {
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
		h.pauseManager.WaitActive()
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

func makeOutboundChain(detourOf []adapter.Outbound, node adapter.Outbound) []adapter.Outbound {
	chain := make([]adapter.Outbound, len(detourOf)+1)
	copy(chain, detourOf)
	chain[len(detourOf)] = node
	return chain
}
