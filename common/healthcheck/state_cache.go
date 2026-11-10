package healthcheck

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

// healthStateFlushInterval is how often the states are written while the checks run. The
// write on close is what makes a restart lose nothing, so this only bounds what a crash
// costs: ten minutes of a twenty-four hour floor, and of an estimate that lives for hours.
// It is a bound on a loss rather than a preference, so it is not in the configuration.
const healthStateFlushInterval = 10 * time.Minute

// healthStateTimeout is how long a written record stays usable. It is the life of the
// longest-lived part, the floor: past it the record describes a node that has changed. The cache
// file drops the expired records when it opens, which is what removes the record of a node whose
// check is no longer configured and therefore never writes again.
const healthStateTimeout = baselineSlot * baselineSlots

// healthStateFingerprint identifies the measurement a stored state belongs to: the target and
// the path the check is dialed through. A different target answers from a different place, and
// a different relay path reaches it by a different route, so the delays of one say nothing
// about the other.
//
// What it identifies is the path, not the check: two checks with the same target and relays
// share the records of the nodes they share, because the record describes the node as that path
// reaches it. That is what keeps the record from being thrown away when only the window or the
// interval is tuned.
//
// The sampling window and the interval are not part of it. A floor is the smallest check of a
// day, and how many checks a round holds barely moves it: on a day of real history, sampling
// anywhere from two to a hundred gave every node the same floor, and stretching the interval
// from two minutes to ten moved it by a few milliseconds. Tying the record to either would
// throw away a day of evidence for a setting that does not change what is measured.
//
// It is computed after the defaults are filled in, so leaving the URL out and naming the
// default one produce the same fingerprint.
func healthStateFingerprint(options option.HealthCheckOptions) []byte {
	var builder strings.Builder
	builder.WriteString("health-state\x001\x00")
	builder.WriteString(options.URL)
	builder.WriteByte(0)
	for _, tag := range options.RelayPath {
		builder.WriteString(tag)
		builder.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return sum[:8]
}

// attachHealthState resolves the cache file the states are kept in, if there is one. Reading
// it is left to the registration of the providers, see restoreHealthState.
func (h *HealthCheck) attachHealthState() {
	cacheFile := service.FromContext[adapter.CacheFile](h.ctx)
	if cacheFile == nil {
		return
	}
	h.stateStore = cacheFile
	h.stateKey = healthStateFingerprint(*h.options)
}

// restoreHealthState reads what the previous run left and puts back the states of the nodes
// the providers have. A record of a node that is gone is simply not taken: it is not kept in
// memory waiting for a check that will never come, and nothing has to drop it later.
//
// It runs once, when the post start is past and every group has registered its providers, see
// Manager.Start.
//
// The rankings already built are dropped afterwards, see the listeners at the end of it: they
// describe the storages as they were before, and a report asks for them without waiting for a
// round.
//
// It waits for the providers first: a remote provider is still reading its own cache when the
// groups register, and its nodes are only there once the first update finishes, which is what
// closes its ready channel, cache or not.
func (h *HealthCheck) restoreHealthState() {
	if h.stateStore == nil {
		return
	}
	h.mergedProviders.Wait()
	current := now()
	restored := false
	for tag, state := range h.stateStore.LoadHealthStates(h.stateKey) {
		if _, loaded := h.mergedProviders.Outbound(tag); !loaded {
			continue
		}
		if h.Storage.Restore(tag, state, current) {
			restored = true
		}
	}
	if restored {
		// A group builds its ranking from the statistics the storages held when it was first
		// read, which was before this restore: the restored floors are not in it, and a report
		// would state none of them until the first round of checks finished. The listeners drop
		// the ranking, so the restored states are what the next report describes.
		h.notifyPostCheckListeners()
	}
}

// startRestore reads the states of the previous run in the background. It is called once, when
// every group has registered its providers, see Manager.Start.
//
// It runs in the background because it waits for the providers: a remote provider is still
// reading its own cache, and the start of the box must not wait on it. The states are only read
// for the reports and for the first round, both of which come after a group is used.
func (h *HealthCheck) startRestore() {
	if h.stateStore == nil {
		return
	}
	go h.restoreHealthState()
}

// startHealthState starts the periodic write. It is independent of the check loop: the checks
// are suspended when every group is idle, and that is exactly when there is nothing to write.
func (h *HealthCheck) startHealthState() {
	if h.stateStore == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.loopMu.Lock()
	h.stateCancel = cancel
	h.loopMu.Unlock()
	go h.healthStateLoop(ctx)
}

func (h *HealthCheck) healthStateLoop(ctx context.Context) {
	ticker := time.NewTicker(healthStateFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.saveHealthState(false)
		}
	}
}

// saveHealthState writes what a restart needs. force writes even when nothing was measured,
// which is what a shutdown does: it also drops the records that have expired since the last
// write, whoever wrote them.
//
// It never touches a context: the box cancels its context before it closes its components, so
// a write that waited on one would silently do nothing on a normal stop.
func (h *HealthCheck) saveHealthState(force bool) {
	if h.stateStore == nil {
		return
	}
	if !force && !h.Storage.TakeChanged() {
		return
	}
	current := now()
	states := h.Storage.States(current)
	if len(states) == 0 {
		// nothing measured yet: the records of the previous run are not this one's to touch
		return
	}
	err := h.stateStore.SaveHealthStates(h.stateKey, states, current.Add(healthStateTimeout))
	if err != nil {
		h.logger.Warn("save health state: ", err)
		h.Storage.MarkChanged()
		return
	}
	h.logger.Info("health state saved.")
}

// closeHealthState stops the periodic write and writes the states one last time. It runs once
// per checker: Close is called by the health check manager and again by the scope of every
// group that used it.
func (h *HealthCheck) closeHealthState(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	h.stateSaveOnce.Do(func() {
		h.saveHealthState(true)
	})
}
