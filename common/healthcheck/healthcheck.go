package healthcheck

import (
	"context"
	"strings"
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
	N "github.com/sagernet/sing/common/network"
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
	// relayExit is the outbound the check is dialed by, the last one of relay_path. The nodes
	// before it are reachable through its dialer chain, which reaches the one it dials through
	// down to the checked node, so there is no need to keep them here.
	relayExit adapter.Outbound
	// detourScope holds the lifetime of the outbounds returned by CreateDetour.
	detourScope   *adapter.Scope
	globalHistory *urltest.HistoryStorage

	loopCtx     context.Context
	loopStarted bool
	loopMu      sync.Mutex

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
	if options.URL == "" {
		options.URL = "https://www.gstatic.com/generate_204"
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
	// Nothing is checked until the group is used, see Touch.
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
	if len(h.options.RelayPath) > 0 {
		if h.om == nil {
			return E.New("missing outbound manager")
		}
		// All detour duplicates share a single scope, which closes them in Close.
		h.detourScope = adapter.NewScope(h.ctx, h.logger)
		// The checked outbound is the entry, which the check supplies through the context, and
		// each duplicate dials through the one before it: the list is the order the check passes
		// through it, the same as the outbounds of a relay. Only the last one, the exit, has to be
		// kept, see checkOutbound.
		var detour N.Dialer = sharedDetourVar
		for _, tag := range h.options.RelayPath {
			detoured, err := h.om.CreateDetour(h.detourScope.Context(), h.router, h.logger, tag, detour, h.detourScope)
			if err != nil {
				return E.Cause(err, "relay_path")
			}
			h.relayExit = detoured
			detour = detoured
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.loopCtx = ctx
	go h.cleanupLoop(ctx, 8*time.Hour)
	// The check loop is only started when a group is used, see Touch.
	return nil
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
	namespaces, run := h.activeNamespaces()
	if !run {
		return
	}
	h.markChecked(namespaces)
	go h.checkAll(ctx, true, namespaces)
}

// ReportConnection records the outcome of a connection the group carried with the node.
//
// It is recorded next to the checks rather than as one of them: the checks are probes on
// a schedule, and the connections say something they cannot, such as a node that passes
// every probe and then drops what it is asked to carry. Both outcomes are recorded, so
// the share that failed is a rate, not a count that grows with the traffic of the node.
func (h *HealthCheck) ReportConnection(outbound adapter.Outbound, established bool) {
	if _, ok := outbound.(adapter.OutboundGroup); ok {
		// cannot determine which node in the group is failed, so just ignore the report
		return
	}
	h.Storage.ReportConnection(outbound.Tag(), established)
}

// IsActive reports whether the namespace was in use within the idle timeout, so its
// health checks are running and its statistics are current. A namespace that was
// never used, or has been idle for longer, is not checked, so its statistics would
// only describe the past.
func (h *HealthCheck) IsActive(namespace string) bool {
	if h == nil {
		return false
	}
	h.idleMu.Lock()
	lastActive := h.lastActive[namespace]
	h.idleMu.Unlock()
	if lastActive.IsZero() {
		return false
	}
	idleTimeout := time.Duration(h.options.IdleTimeout)
	if idleTimeout <= 0 {
		// without an idle timeout the loop checks every namespace each round
		return true
	}
	return time.Since(lastActive) < idleTimeout
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
	h.idleMu.Lock()
	h.lastActive[namespace] = now
	h.idleMu.Unlock()
	if h.startLoop() {
		// The first round of the started loop covers this namespace.
		return
	}
	if h.options.IdleTimeout <= 0 {
		// Without an idle timeout the loop checks every namespace each round,
		// a catch-up would only duplicate it.
		return
	}
	h.idleMu.Lock()
	catchUp := false
	if now.Sub(h.lastCheck[namespace]) >= time.Duration(h.options.Interval) {
		if _, inFlight := h.checking[namespace]; !inFlight {
			h.checking[namespace] = struct{}{}
			h.lastCheck[namespace] = now
			catchUp = true
		}
	}
	h.idleMu.Unlock()
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

// startLoop starts the check loop when it is not running, either because it was
// never needed or because it was suspended after being idle. It returns true
// when the loop was started, in which case its first round covers the namespaces
// in use right away.
func (h *HealthCheck) startLoop() bool {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	if h.loopCtx == nil {
		return false
	}
	if h.suspended {
		h.suspended = false
		h.loopStarted = true
		h.logger.Info("health check resumed")
		go h.checkLoop(h.loopCtx)
		return true
	}
	if h.loopStarted {
		return false
	}
	h.loopStarted = true
	h.logger.Debug("health check started")
	go h.checkLoop(h.loopCtx)
	return true
}

// activeNamespaces returns the namespaces that are still in use. A nil result
// means all namespaces. The loop must be suspended when run is false, because
// every namespace has been idle for longer than the configured idle timeout.
func (h *HealthCheck) activeNamespaces() (namespaces []string, run bool) {
	now := time.Now()
	h.idleMu.Lock()
	defer h.idleMu.Unlock()
	if len(h.lastActive) == 0 {
		// Nothing has ever been used, keep the checks lazy.
		return nil, false
	}
	idleTimeout := time.Duration(h.options.IdleTimeout)
	if idleTimeout <= 0 {
		return nil, true
	}
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
	if len(namespaces) > 0 {
		// format.ToString doesn't handle slices, join them first.
		h.logger.Info("scheduled health check for: ", strings.Join(namespaces, ", "))
	}
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
				// Ignore the error so the failure can be returned by the batch.
				// The result is committed after the round, see waitProcessResult.
				meta.ReportFailure(tag)
				return 0, nil
			}
			meta.ReportSuccess()
			// Commit the result as soon as it is available, instead of waiting for
			// the whole round, so a single slow node doesn't delay the statistics.
			h.storeResult(meta.scheduled, tag, RTT(t))
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
	if h.relayExit != nil {
		testCtx = contextWithDetourVar(testCtx, outbound)
		outbound = h.relayExit
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
		delay, err := urltest.URLTest(testCtx, h.options.URL, outbound)
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

// storeResult commits a check result for a node: the scheduled check appends it
// to the sampling window, the non-scheduled one updates the latest result.
func (h *HealthCheck) storeResult(scheduled bool, tag string, delay RTT) {
	// always update global history for display usage,
	// so that user can see the latest failure status
	if h.globalHistory != nil {
		h.globalHistory.StoreURLTestHistory(tag, &adapter.URLTestHistory{
			Time:  time.Now(),
			Delay: uint16(delay),
		})
	}
	if scheduled {
		h.Storage.Put(tag, delay)
	} else {
		h.Storage.Update(tag, delay)
	}
}

func (h *HealthCheck) waitProcessResult(batch *batch.Batch[uint16], meta *MetaData) (map[string]uint16, error) {
	m, err := batch.WaitAndGetResult()
	if err != nil {
		return nil, err
	}
	r := make(map[string]uint16, len(m))
	for tag, v := range m {
		r[tag] = v.Value
	}
	// Successful results were already committed when they arrived. Failed ones are
	// committed now, since an all-failed round means the network is unavailable or
	// every node is dead, which doesn't contribute to the objective.
	pendingFailures := meta.TakePendingFailures()
	anySuccess := meta.AnySuccess()
	for _, tag := range pendingFailures {
		if anySuccess {
			h.storeResult(meta.scheduled, tag, Failed)
		} else if h.globalHistory != nil {
			h.globalHistory.StoreURLTestHistory(tag, &adapter.URLTestHistory{
				Time:  time.Now(),
				Delay: uint16(Failed),
			})
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
