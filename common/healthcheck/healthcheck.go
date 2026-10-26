package healthcheck

import (
	"context"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/batch"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

var (
	_ adapter.SimpleLifecycle         = (*HealthCheck)(nil)
	_ adapter.InterfaceUpdateListener = (*HealthCheck)(nil)
	_ adapter.HealthCheck             = (*HealthCheck)(nil)
)

// HealthCheck is the health checker for balancers
type HealthCheck struct {
	ctx          context.Context
	router       adapter.Router
	om           adapter.OutboundManager
	logger       log.ContextLogger
	pauseManager pause.Manager
	options      *option.HealthCheckOptions

	Storage         *Storages
	mergedProviders *mergedProvider
	cancel          context.CancelFunc
	detourOf        []adapter.Outbound
	// detourScope holds the lifetime of the outbounds returned by DupOverrideDetour.
	detourScope   *adapter.Scope
	globalHistory *urltest.HistoryStorage

	loopCtx        context.Context
	loopStarted    bool
	cleanupStarted bool
	loopMu         sync.Mutex

	// idleMu guards the per namespace activity used to suspend the loops when
	// every namespace is idle. suspended is guarded by loopMu.
	idleMu     sync.Mutex
	lastActive map[string]time.Time
	lastCheck  map[string]time.Time
	checking   map[string]struct{}
	suspended  bool

	listenersMu        sync.Mutex
	postCheckListeners []func()
}

// NewHealthCheck creates a new HealthPing with settings.
//
// The globalHistory is optional and is only used to sync latency history
// between different health checkers. Each HealthCheck will maintain its own
// history storage since different ones can have different check destinations,
// sampling numbers, etc.
func NewHealthCheck(
	ctx context.Context,
	router adapter.Router,
	outbound adapter.OutboundManager,
	options *option.HealthCheckOptions, logger log.ContextLogger,
) *HealthCheck {
	if options == nil {
		options = &option.HealthCheckOptions{}
	}
	if options.Destination == "" {
		options.Destination = "https://www.gstatic.com/generate_204"
	}
	if options.Interval <= 0 {
		options.Interval = badoption.Duration(5 * time.Minute)
	}
	if options.Interval < badoption.Duration(10*time.Second) {
		options.Interval = badoption.Duration(10 * time.Second)
	}
	if options.Sampling <= 0 {
		options.Sampling = 10
	}
	if options.IdleTimeout > 0 && options.IdleTimeout < options.Interval {
		options.IdleTimeout = options.Interval
	}
	return &HealthCheck{
		lastActive:      make(map[string]time.Time),
		lastCheck:       make(map[string]time.Time),
		checking:        make(map[string]struct{}),
		ctx:             ctx,
		om:              outbound,
		logger:          logger,
		mergedProviders: newMergedProvider(),
		options:         options,
		globalHistory:   service.PtrFromContext[urltest.HistoryStorage](ctx),
		Storage: NewStorages(
			options.Sampling,
			time.Duration(options.Sampling+1)*time.Duration(options.Interval),
		),
		pauseManager: service.FromContext[pause.Manager](ctx),
	}
}

// SetProviders replaces the full provider list for the given namespace.
func (h *HealthCheck) SetProviders(namespace string, providers []adapter.Provider) error {
	h.mergedProviders.Set(namespace, providers)
	if len(providers) > 0 {
		h.markActive(namespace)
		h.tryStartLoops()
	}
	go func() {
		// wait for all providers to be ready
		h.mergedProviders.NamespacedWait(namespace)
		h.CheckAll(context.Background(), namespace)
	}()
	return nil
}

// RemoveProviders removes the provider list for the given namespace.
func (h *HealthCheck) RemoveProviders(namespace string) error {
	if h.mergedProviders != nil {
		h.mergedProviders.Set(namespace, nil)
	}
	h.idleMu.Lock()
	delete(h.lastActive, namespace)
	delete(h.lastCheck, namespace)
	h.idleMu.Unlock()
	return nil
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
		// All detour duplicates share a single scope and are torn down together in Close.
		h.detourScope = adapter.NewScope(h.ctx, h.logger)
		for i := len(h.options.DetourOf) - 1; i >= 0; i-- {
			tag := h.options.DetourOf[i]
			outbound, err := h.om.DupOverrideDetour(h.ctx, h.detourScope, h.router, tag, h.logger, detour)
			if err != nil {
				return E.Cause(err, "detour_of")
			}
			h.detourOf[i] = outbound
			detour = outbound
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.loopCtx = ctx
	// loops are started lazily when providers are registered via SetProviders
	return nil
}

// tryStartLoops starts the background loops if they haven't been started yet.
// Must be called after Start() has initialized h.loopCtx.
func (h *HealthCheck) tryStartLoops() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	if h.loopCtx == nil {
		return
	}
	// The cleanup loop lives until Close, only the check loop is suspended.
	if !h.cleanupStarted {
		h.cleanupStarted = true
		go h.cleanupLoop(h.loopCtx, 8*time.Hour)
	}
	if !h.loopStarted {
		h.loopStarted = true
		go h.checkLoop(h.loopCtx)
	}
}

// Close stops the health check service, implements adapter.Service
func (h *HealthCheck) Close() error {
	h.loopMu.Lock()
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	h.loopCtx = nil
	h.loopStarted = false
	h.cleanupStarted = false
	h.suspended = false
	h.loopMu.Unlock()
	if h.detourScope == nil {
		return nil
	}
	err := h.detourScope.Close()
	h.detourScope = nil
	return err
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (h *HealthCheck) InterfaceUpdated(ctx context.Context) {
	if h == nil {
		return
	}
	// h.logger.Info("[InterfaceUpdated]: CheckAll()")
	h.markChecked(nil)
	go h.checkAll(ctx, true, nil)
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

// Touch records that the namespace is in use, so its health checks keep running.
// When the loop was suspended because every namespace was idle, it is restarted
// and a catch-up check is performed for this namespace, since the statistics of
// its nodes are stale.
func (h *HealthCheck) Touch(namespace string) {
	if h == nil {
		return
	}
	now := time.Now()
	h.loopMu.Lock()
	suspended := h.suspended
	h.loopMu.Unlock()
	h.idleMu.Lock()
	h.lastActive[namespace] = now
	catchUp := false
	// Without an idle timeout the loop checks every namespace anyway, a catch-up
	// would only duplicate it.
	if !suspended && h.options.IdleTimeout > 0 && now.Sub(h.lastCheck[namespace]) >= time.Duration(h.options.Interval) {
		if _, inFlight := h.checking[namespace]; !inFlight {
			h.checking[namespace] = struct{}{}
			h.lastCheck[namespace] = now
			catchUp = true
		}
	}
	h.idleMu.Unlock()
	if suspended {
		// The restarted loop checks the namespace right away.
		h.resume()
		return
	}
	if catchUp {
		go h.checkNamespace(namespace)
	}
}

// markChecked records that the namespaces were just checked, so a Touch doesn't
// schedule a redundant catch-up check. A nil list means all namespaces.
func (h *HealthCheck) markChecked(namespaces []string) {
	now := time.Now()
	h.idleMu.Lock()
	if namespaces == nil {
		for namespace := range h.lastActive {
			h.lastCheck[namespace] = now
		}
	} else {
		for _, namespace := range namespaces {
			h.lastCheck[namespace] = now
		}
	}
	h.idleMu.Unlock()
}

func (h *HealthCheck) markActive(namespace string) {
	now := time.Now()
	h.idleMu.Lock()
	h.lastActive[namespace] = now
	h.lastCheck[namespace] = now
	h.idleMu.Unlock()
}

// checkNamespace performs a scheduled check for a single namespace, it is used
// to catch up after the loop was suspended or the namespace was idle.
func (h *HealthCheck) checkNamespace(namespace string) {
	defer func() {
		h.idleMu.Lock()
		delete(h.checking, namespace)
		h.idleMu.Unlock()
	}()
	_, err := h.checkAll(h.ctx, true, []string{namespace})
	if err != nil && h.ctx.Err() == nil {
		h.logger.Debug("health check for [", namespace, "]: ", err)
	}
}

// resume restarts the loops after they were suspended because every namespace
// was idle.
func (h *HealthCheck) resume() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	if !h.suspended || h.loopCtx == nil {
		return
	}
	h.suspended = false
	h.loopStarted = true
	h.logger.Info("health check resumed")
	go h.checkLoop(h.loopCtx)
}

// activeNamespaces returns the namespaces that are still in use. A nil result
// means all namespaces. The loop must be suspended when run is false, because
// every namespace has been idle for longer than the configured idle timeout.
func (h *HealthCheck) activeNamespaces() (namespaces []string, run bool) {
	idleTimeout := time.Duration(h.options.IdleTimeout)
	if idleTimeout <= 0 {
		return nil, true
	}
	now := time.Now()
	h.idleMu.Lock()
	defer h.idleMu.Unlock()
	for namespace, lastActive := range h.lastActive {
		if now.Sub(lastActive) < idleTimeout {
			namespaces = append(namespaces, namespace)
		}
	}
	if len(namespaces) > 0 {
		return namespaces, true
	}
	return nil, false
}

// scheduledCheck runs a scheduled check for the namespaces in use and reports
// whether the loop should keep running.
func (h *HealthCheck) scheduledCheck(ctx context.Context) bool {
	namespaces, run := h.activeNamespaces()
	if !run {
		return false
	}
	h.markChecked(namespaces)
	go h.checkAll(ctx, true, namespaces)
	return true
}

func (h *HealthCheck) checkLoop(ctx context.Context) {
	if !h.scheduledCheck(ctx) {
		h.suspend()
		return
	}
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		h.pauseManager.WaitActive()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !h.scheduledCheck(ctx) {
				h.suspend()
				return
			}
		}
	}
}

// suspend stops the loops until Touch is called for any namespace.
func (h *HealthCheck) suspend() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	if h.loopCtx == nil {
		return
	}
	h.suspended = true
	h.loopStarted = false
	h.logger.Info("health check paused, no namespace active for ", time.Duration(h.options.IdleTimeout))
}

// RegisterPostCheckListener registers a listener function that will be called after each CheckAll() call.
func (h *HealthCheck) RegisterPostCheckListener(listener func()) {
	h.listenersMu.Lock()
	defer h.listenersMu.Unlock()
	h.postCheckListeners = append(h.postCheckListeners, listener)
}

// UnregisterPostCheckListener unregisters a listener function that was previously registered with RegisterPostCheckListener.
func (h *HealthCheck) UnregisterPostCheckListener(listener func()) {
	h.listenersMu.Lock()
	defer h.listenersMu.Unlock()
	for i, l := range h.postCheckListeners {
		if &l == &listener {
			h.postCheckListeners = append(h.postCheckListeners[:i], h.postCheckListeners[i+1:]...)
			return
		}
	}
}

func (h *HealthCheck) notifyPostCheckListeners() {
	h.listenersMu.Lock()
	defer h.listenersMu.Unlock()
	for _, listener := range h.postCheckListeners {
		listener()
	}
}

// CheckAll performs checks for nodes of the given namespace
func (h *HealthCheck) CheckAll(ctx context.Context, namespace string) (map[string]uint16, error) {
	return h.checkAll(ctx, false, []string{namespace})
}

// checkAll performs checks for the nodes of the given namespaces, a nil list
// means all namespaces.
func (h *HealthCheck) checkAll(ctx context.Context, scheduled bool, namespaces []string) (map[string]uint16, error) {
	batch, _ := batch.New(ctx, batch.WithConcurrencyNum[uint16](10))
	// share ctx information between checks
	meta := NewMetaData()
	meta.scheduled = scheduled
	outbounds := h.mergedProviders.NamespacesOutbounds(namespaces)
	for _, outbound := range outbounds {
		err := h.checkOutboundBatch(ctx, meta, batch, outbound)
		if err != nil {
			return nil, err
		}
	}
	r, err := h.waitProcessResult(batch, meta)
	go h.notifyPostCheckListeners()
	return r, err
}

// // CheckOutbound performs check for the specified node
// func (h *HealthCheck) CheckOutbound(ctx context.Context, namespace, tag string) (uint16, error) {
// 	outbound, ok := h.mergedProviders.NamespacedOutbound(namespace, tag)
// 	if !ok {
// 		return 0, E.New("outbound [", tag, "] not found")
// 	}
// 	outbound, err := adapter.RealOutbound(outbound)
// 	if err != nil {
// 		return 0, err
// 	}
// 	t, err := h.checkOutbound(ctx, outbound)
// 	if h.globalHistory != nil {
// 		h.globalHistory.StoreURLTestHistory(tag, &adapter.URLTestHistory{
// 			Time:  time.Now(),
// 			Delay: t,
// 		})
// 	}
// 	h.Storage.Update(tag, RTT(t))
// 	return t, err
// }

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

func (h *HealthCheck) checkOutbound(ctx context.Context, outbound adapter.Outbound) (uint16, error) {
	tag := outbound.Tag()
	testCtx, cancel := context.WithTimeout(ctx, C.TCPTimeout)
	defer cancel()
	testCtx = log.ContextWithOverrideLevel(testCtx, log.LevelDebug)
	if len(h.detourOf) > 0 {
		testCtx = contextWithDetourVar(testCtx, outbound)
		outbound = h.detourOf[0]
	}
	// Run the test in a goroutine and race it against the context timeout,
	// so a single check can never hang longer than C.TCPTimeout even if the
	// outbound ignores context cancellation (e.g. a stuck SSH handshake).
	type checkResult struct {
		delay uint16
		err   error
	}
	testChan := make(chan checkResult, 1)
	go func() {
		delay, err := urltest.URLTest(testCtx, h.options.Destination, outbound)
		testChan <- checkResult{delay, err}
	}()
	var result checkResult
	select {
	case result = <-testChan:
	case <-testCtx.Done():
		result.err = testCtx.Err()
	}
	if result.err != nil {
		h.logger.Debug("outbound ", tag, " unavailable: ", result.err)
		return 0, result.err
	}
	rtt := RTT(result.delay)
	h.logger.Debug("outbound ", tag, " available: ", rtt)
	return result.delay, nil
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
		if _, ok := h.mergedProviders.Outbound(tag); !ok {
			h.Storage.Delete(tag)
		}
	}
}
