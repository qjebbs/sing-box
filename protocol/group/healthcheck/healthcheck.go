package healthcheck

import (
	"context"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/batch"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

var (
	_ adapter.SimpleLifecycle         = (*HealthCheck)(nil)
	_ adapter.InterfaceUpdateListener = (*HealthCheck)(nil)
)

// HealthCheck is the health checker for balancers
type HealthCheck struct {
	Storage *Storages

	ctx            context.Context
	router         adapter.Router
	om             adapter.OutboundManager
	logger         log.ContextLogger
	pauseManager   pause.Manager
	globalHistory  adapter.URLTestHistoryStorage
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
	outbound adapter.OutboundManager,
	providers []adapter.Provider,
	options *option.HealthCheckOptions, logger log.ContextLogger,
) *HealthCheck {
	if options == nil {
		options = &option.HealthCheckOptions{}
	}
	if options.Destination == "" {
		options.Destination = "https://www.gstatic.com/generate_204"
	}
	if options.Interval < badoption.Duration(10*time.Second) {
		options.Interval = badoption.Duration(10 * time.Second)
	}
	if options.Sampling <= 0 {
		options.Sampling = 10
	}
	providersByTag := make(map[string]adapter.Provider)
	for _, provider := range providers {
		providersByTag[provider.Tag()] = provider
	}
	var history adapter.URLTestHistoryStorage
	if history = service.FromContext[adapter.URLTestHistoryStorage](ctx); history != nil {
	} else if clashServer := service.FromContext[adapter.ClashServer](ctx); clashServer != nil {
		history = clashServer.HistoryStorage()
	} else {
		history = urltest.NewHistoryStorage()
	}
	return &HealthCheck{
		ctx:            ctx,
		om:             outbound,
		logger:         logger,
		globalHistory:  history,
		providers:      providers,
		providersByTag: providersByTag,
		options:        options,
		Storage: NewStorages(
			options.Sampling,
			time.Duration(options.Sampling+1)*time.Duration(options.Interval),
		),
		pauseManager: service.FromContext[pause.Manager](ctx),
	}
}

// Start starts the health check service, implements adapter.Service
func (h *HealthCheck) Start() error {
	if h.cancel != nil {
		return nil
	}
	if len(h.options.DetourOf) > 0 {
		if h.om == nil {
			return E.New("missing outbound manager")
		}
		detour := newDetourVar()
		h.detourOf = make([]adapter.Outbound, len(h.options.DetourOf))
		for i := len(h.options.DetourOf) - 1; i >= 0; i-- {
			tag := h.options.DetourOf[i]
			outbound, err := h.om.DupOverrideDetour(h.ctx, h.router, tag, h.logger, detour)
			if err != nil {
				return E.Cause(err, "detour_of")
			}
			h.detourOf[i] = outbound
			detour = outbound
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
	for _, detour := range h.detourOf {
		common.Close(detour)
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
		// cannot determine which node in the group is failed, so just ignore the report
		return
	}
	tag := outbound.Tag()
	// MUST Update instead of Put, since Put will add a new history
	// which affects the max_fail assertion in balancers.
	h.Storage.Update(tag, Failed)
}

func (h *HealthCheck) checkLoop(ctx context.Context) {
	go h.checkAll(ctx, true)
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		h.pauseManager.WaitActive()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go h.checkAll(ctx, true)
		}
	}
}

// CheckAll performs checks for nodes of all providers
func (h *HealthCheck) CheckAll(ctx context.Context) (map[string]uint16, error) {
	return h.checkAll(ctx, false)
}

func (h *HealthCheck) checkAll(ctx context.Context, scheduled bool) (map[string]uint16, error) {
	batch, _ := batch.New(ctx, batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	meta := NewMetaData()
	meta.scheduled = scheduled
	for _, provider := range h.providers {
		err := h.checkProviderBatch(ctx, meta, batch, provider)
		if err != nil {
			return nil, err
		}
	}
	return h.waitProcessResult(batch, meta)
}

// CheckProvider performs checks for nodes of the provider
func (h *HealthCheck) CheckProvider(ctx context.Context, tag string) (map[string]uint16, error) {
	provider, ok := h.providersByTag[tag]
	if !ok {
		return nil, E.New("provider [", tag, "] not found")
	}
	batch, _ := batch.New(ctx, batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	meta := NewMetaData()
	err := h.checkProviderBatch(ctx, meta, batch, provider)
	if err != nil {
		return nil, err
	}
	return h.waitProcessResult(batch, meta)
}

// CheckOutbound performs check for the specified node
func (h *HealthCheck) CheckOutbound(ctx context.Context, tag string) (uint16, error) {
	outbound, ok := h.outbound(tag)
	if !ok {
		return 0, E.New("outbound [", tag, "] not found")
	}
	outbound, err := adapter.RealOutbound(outbound)
	if err != nil {
		return 0, err
	}
	t, err := h.checkOutbound(ctx, outbound)
	if h.globalHistory != nil {
		h.globalHistory.StoreURLTestHistory(tag, &adapter.URLTestHistory{
			Time:  time.Now(),
			Delay: t,
		})
	}
	h.Storage.Update(tag, RTT(t))
	return t, err
}

func (h *HealthCheck) checkProviderBatch(ctx context.Context, meta *MetaData, batch *batch.Batch[uint16], provider adapter.Provider) error {
	for _, outbound := range provider.Outbounds() {
		err := h.checkOutboundBatch(ctx, meta, batch, outbound)
		if err != nil {
			return err
		}
	}
	return nil
}

// checkOutboundBatch assigns a check task to the batch for the specified outbound
func (h *HealthCheck) checkOutboundBatch(ctx context.Context, meta *MetaData, batch *batch.Batch[uint16], outbound adapter.Outbound) error {
	real, err := adapter.RealOutbound(outbound)
	if err != nil {
		return err
	}
	tag := real.Tag()
	if meta.Checked(tag) {
		return nil
	}
	meta.ReportChecked(tag)
	batch.Go(
		tag,
		func() (uint16, error) {
			t, err := h.checkOutbound(ctx, real)
			if err != nil {
				// ignore error so the failure can be returned by the batch
				return 0, nil
			}
			meta.ReportSuccess()
			return t, nil
		},
	)
	return nil
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

func (h *HealthCheck) checkOutbound(ctx context.Context, outbound adapter.Outbound) (uint16, error) {
	tag := outbound.Tag()
	testCtx, cancel := context.WithTimeout(ctx, C.TCPTimeout)
	defer cancel()
	testCtx = log.ContextWithOverrideLevel(testCtx, log.LevelDebug)
	if len(h.detourOf) > 0 {
		testCtx = contextWithDetourVar(testCtx, outbound)
		outbound = h.detourOf[0]
	}
	t, err := urltest.URLTest(testCtx, h.options.Destination, outbound)
	if err != nil {
		h.logger.Debug("outbound ", tag, " unavailable: ", err)
		return 0, err
	}
	rtt := RTT(t)
	h.logger.Debug("outbound ", tag, " available: ", rtt)
	return t, nil
}

func (h *HealthCheck) waitProcessResult(batch *batch.Batch[uint16], meta *MetaData) (map[string]uint16, error) {
	m, err := batch.WaitAndGetResult()
	if err != nil {
		return nil, err
	}
	r := make(map[string]uint16)
	for tag, v := range m {
		r[tag] = v.Value
		// always update global history for display usage,
		// so that user can see the latest failure status
		if h.globalHistory != nil {
			h.globalHistory.StoreURLTestHistory(tag, &adapter.URLTestHistory{
				Time:  time.Now(),
				Delay: v.Value,
			})
		}
		// ignore all-failed result, since it doesn't contribute to the
		// objective to tell which nodes are better
		if meta.AnySuccess() {
			if meta.scheduled {
				h.Storage.Put(tag, RTT(v.Value))
			} else {
				h.Storage.Update(tag, RTT(v.Value))
			}
		}
	}
	return r, nil
}

func (h *HealthCheck) cleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
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
