package healthcheck

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/cachefile"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
)

// fakeStateStore is a cache file that keeps the states in memory. Only the part the health
// check uses is implemented.
type fakeStateStore struct {
	states      map[string]adapter.HealthState
	fingerprint []byte
	expiresAt   time.Time
	saved       int
}

func (s *fakeStateStore) LoadHealthStates(fingerprint []byte) map[string]adapter.HealthState {
	if string(fingerprint) != string(s.fingerprint) {
		return nil
	}
	states := make(map[string]adapter.HealthState, len(s.states))
	for tag, state := range s.states {
		states[tag] = state
	}
	return states
}

func (s *fakeStateStore) SaveHealthStates(fingerprint []byte, states map[string]adapter.HealthState, expiresAt time.Time) error {
	s.fingerprint = append([]byte(nil), fingerprint...)
	s.expiresAt = expiresAt
	if s.states == nil {
		s.states = make(map[string]adapter.HealthState)
	}
	for tag, state := range states {
		s.states[tag] = state
	}
	s.saved++
	return nil
}

func newStoredCheck(store adapter.HealthStateStore) *HealthCheck {
	check := newConfigurable(&option.HealthCheckOptions{
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	})
	check.mergedProviders = newMergedProvider()
	check.logger = log.NewNOPFactory().NewLogger("healthcheck")
	check.stateStore = store
	check.stateKey = healthStateFingerprint(*check.options)
	return check
}

// attachNodes makes the checker see the given nodes, which is what the restore filters on.
func attachNodes(check *HealthCheck, tags ...string) {
	check.mergedProviders.Set("group", []adapter.Provider{&fakeProvider{tags: tags}})
}

// floorValues returns the slots of a state that holds one floor, the one measured at that
// moment.
func floorValues(at time.Time, floor RTT) []uint16 {
	values := make([]uint16, baselineSlots)
	values[baselineSlotOf(at)] = uint16(floor)
	return values
}

// floorOf returns the floor a stored state holds, which is the fastest of its slots.
func floorOf(state adapter.HealthState) RTT {
	var floor RTT
	for _, value := range state.Floors {
		if value != 0 && (floor == 0 || RTT(value) < floor) {
			floor = RTT(value)
		}
	}
	return floor
}

// seededFloor returns the floor the storage of the tag holds, which is what a restore puts
// there. It reads the baseline rather than the statistics: a node with no check of this run
// has none of those yet, see refreshStats.
func seededFloor(check *HealthCheck, tag string, at time.Time) (RTT, bool) {
	check.Storage.access.Lock()
	store, loaded := check.Storage.storages[tag]
	check.Storage.access.Unlock()
	if !loaded {
		return 0, false
	}
	return store.baseline.RTT(at)
}

// restoreStates seeds the storages the way a cache file does, one tag at a time.
func restoreStates(check *HealthCheck, states map[string]adapter.HealthState) {
	current := now()
	for tag, state := range states {
		check.Storage.Restore(tag, state, current)
	}
}

func TestRestoredFloorIsUsedBeforeTheWindowFills(t *testing.T) {
	t.Parallel()
	// The floor is the whole point of storing: a restarted process has to rank on the day it
	// has already seen, not on the ten checks it has collected since it started.
	now := time.Now()
	store := &fakeStateStore{}
	store.SaveHealthStates(healthStateFingerprint(option.HealthCheckOptions{
		URL:      "https://example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}), map[string]adapter.HealthState{
		"node": {Floors: floorValues(now.Add(-2*time.Hour), 150), FloorsAt: now.Add(-2 * time.Hour)},
	}, time.Now().Add(time.Hour))

	check := newConfigurable(&option.HealthCheckOptions{
		URL:      "https://example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	})
	check.stateKey = healthStateFingerprint(*check.options)
	restoreStates(check, store.LoadHealthStates(check.stateKey))

	// A window nowhere near the floor of the previous day.
	for i := 0; i < 10; i++ {
		check.Storage.Put("node", 600)
	}
	if got := check.Storage.Stats("node").Floor; got != 150 {
		t.Fatalf("want the restored floor of 150, got %d", got)
	}
}

func TestRestoredFloorIsReadBeforeTheWindowHasTwoChecks(t *testing.T) {
	t.Parallel()
	// A round of one check sets no baseline of its own, but a floor a restart restored is
	// already there: the report prices the node from its first check instead of calling it
	// unknown, which is the wait a restart must not have.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	restoreStates(check, map[string]adapter.HealthState{
		"node": {Floors: floorValues(now.Add(-time.Hour), 150), FloorsAt: now.Add(-time.Hour)},
	})
	check.Storage.Put("node", RTT(600))
	if got := check.Storage.Stats("node").Floor; got != 150 {
		t.Fatalf("want the restored floor from the first check, got %d", got)
	}
}

func TestRestoredFloorExpiresLikeTheSlotItCameFrom(t *testing.T) {
	t.Parallel()
	// Restoring a floor into the slot it came from is what keeps its life: it has to expire at
	// the moment it would have without the restart, not a day later.
	store := &fakeStateStore{}
	stale := time.Now().Add(-baselineSlot*baselineSlots - time.Minute)
	store.SaveHealthStates(nil, nil, time.Time{})
	check := newStoredCheck(store)
	restoreStates(check, map[string]adapter.HealthState{
		"node": {Floors: floorValues(stale, 150), FloorsAt: stale},
	})
	for i := 0; i < 10; i++ {
		check.Storage.Put("node", 600)
	}
	// The floor is gone, so the ten checks set one of their own: their fastest.
	if got := check.Storage.Stats("node").Floor; got != 600 {
		t.Fatalf("want the expired floor to be dropped and 600 measured, got %d", got)
	}
}

func TestStateRoundTripsThroughTheStorages(t *testing.T) {
	t.Parallel()
	// What is written has to be what is read: a floor in its slot, and an estimate that keeps
	// decaying from where it stopped.
	check := newStoredCheck(&fakeStateStore{})
	for i := 0; i < 10; i++ {
		check.Storage.Put("node", RTT(100))
	}
	// A round is ranked, which is what writes the floor of the window into its slot.
	check.Storage.Stats("node")
	check.Storage.Put("node", Failed)
	for i := 0; i < 10; i++ {
		check.Storage.Put("node", RTT(300))
	}
	now := time.Now()
	states := check.Storage.States(now)
	state, loaded := states["node"]
	if !loaded {
		t.Fatal("want a state for the measured node")
	}
	if got := floorOf(state); got != 100 {
		t.Fatalf("want the floor of 100, got %d", got)
	}
	if state.FailureWeight == 0 || state.FailureSum == 0 {
		t.Fatalf("want the failure estimate kept, got %v / %v", state.FailureSum, state.FailureWeight)
	}
	wantRate := float64(state.FailureSum) / state.FailureWeight

	restarted := newStoredCheck(&fakeStateStore{})
	restoreStates(restarted, states)
	// Two successes are what a window needs to read a floor at all, restored or not, see
	// refreshStats.
	restarted.Storage.Put("node", RTT(300))
	restarted.Storage.Put("node", RTT(300))
	stats := restarted.Storage.Stats("node")
	if stats.Floor != 100 {
		t.Fatalf("want the restored floor of 100, got %d", stats.Floor)
	}
	if diff := stats.FailureRate - float32(wantRate); diff > 0.05 || diff < -0.05 {
		t.Fatalf("want the failure rate near %v, got %v", wantRate, stats.FailureRate)
	}
}

func TestStateCarriesTheWholeDayOfSlots(t *testing.T) {
	t.Parallel()
	// The record carries the day of slots, not only the one holding the floor: every slot comes
	// back into the window it was measured in, so a restart does not have to measure the rest
	// of the day again. Only the moment the values were read travels with them.
	now := time.Now()
	options := StorageOptions{Sampling: 10, Validity: 20 * time.Minute, Interval: 2 * time.Minute}
	storages := NewStorages(options)
	storages.access.Lock()
	storage := storages.storageOf("node")
	for index := 0; index < baselineSlots; index++ {
		storage.baseline.Put(lastWindowOf(now, index), RTT(200+index))
	}
	storages.access.Unlock()

	state, loaded := storages.States(now)["node"]
	if !loaded {
		t.Fatal("want the day to be kept")
	}
	if len(state.Floors) != baselineSlots {
		t.Fatalf("want %d slots, got %d", baselineSlots, len(state.Floors))
	}
	// the one moment that travels with them is the window they were read in, at its start
	if !state.FloorsAt.Equal(baselineWindowStart(now)) {
		t.Fatalf("want the window read in, %v, got %v", baselineWindowStart(now), state.FloorsAt)
	}

	restarted := NewStorages(options)
	restarted.Restore("node", state, now)
	restarted.access.Lock()
	restored := restarted.storages["node"]
	restarted.access.Unlock()
	for index := 0; index < baselineSlots; index++ {
		if got := restored.baseline.values[index]; got != RTT(200+index) {
			t.Fatalf("slot %d: want %d, got %d", index, 200+index, got)
		}
	}
}

func TestRestoreSeedsTheNodeBeforeItIsMeasured(t *testing.T) {
	t.Parallel()
	// The state is in place before the first check arrives, which is what makes the first
	// window of the run read the floor of the previous one instead of starting over.
	now := time.Now()
	storages := NewStorages(StorageOptions{Sampling: 10, Validity: 20 * time.Minute, Interval: 2 * time.Minute})
	storages.Restore("node", adapter.HealthState{Floors: floorValues(now.Add(-time.Hour), 120), FloorsAt: now.Add(-time.Hour)}, now)
	for i := 0; i < 10; i++ {
		storages.Put("node", RTT(500))
	}
	if got := storages.Stats("node").Floor; got != 120 {
		t.Fatalf("want the restored floor of 120, got %d", got)
	}
}

func TestExpiredStatesAreNotWritten(t *testing.T) {
	t.Parallel()
	// A record that has expired is worth nothing to the next run, so it is not carried into
	// the next write either.
	now := time.Now()
	stale := now.Add(-failureStateTimeout - time.Minute)
	expired := now.Add(-baselineSlot*baselineSlots - time.Minute)
	check := newStoredCheck(&fakeStateStore{})
	restoreStates(check, map[string]adapter.HealthState{
		"gone":  {FailureSum: 1, FailureWeight: 5, FailureAt: stale},
		"floor": {Floors: floorValues(expired, 150), FloorsAt: expired},
	})
	written := check.Storage.States(now)
	if len(written) != 0 {
		t.Fatalf("want the expired states left out, got %v", written)
	}
}

func TestHealthStateFingerprintFollowsTheMeasurement(t *testing.T) {
	t.Parallel()
	// A floor measured through another target or another relay says nothing about this one,
	// and the record is dropped for it.
	base := option.HealthCheckOptions{
		URL:      "https://example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}
	fingerprint := healthStateFingerprint(base)
	variants := []option.HealthCheckOptions{
		{URL: "https://example.org", Sampling: 10, Interval: badoption.Duration(2 * time.Minute)},
		{URL: "https://example.com", Sampling: 10, Interval: badoption.Duration(2 * time.Minute), RelayPath: []string{"relay"}},
		{URL: "https://example.com", Sampling: 10, Interval: badoption.Duration(2 * time.Minute), RelayPath: []string{"relay", "exit"}},
	}
	for _, variant := range variants {
		if string(healthStateFingerprint(variant)) == string(fingerprint) {
			t.Fatalf("want a different fingerprint for %+v", variant)
		}
	}
	if string(healthStateFingerprint(base)) != string(fingerprint) {
		t.Fatal("want the fingerprint to be stable")
	}
}

func TestHealthStateFingerprintIgnoresTheWindow(t *testing.T) {
	t.Parallel()
	// A floor is the smallest check of a day, so the size of the window it was taken from and
	// how often it was taken do not change what the node can do: tuning either must not throw
	// away a day of evidence.
	base := option.HealthCheckOptions{
		URL:      "https://example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}
	fingerprint := healthStateFingerprint(base)
	for _, variant := range []option.HealthCheckOptions{
		{URL: "https://example.com", Sampling: 1, Interval: badoption.Duration(2 * time.Minute)},
		{URL: "https://example.com", Sampling: 50, Interval: badoption.Duration(2 * time.Minute)},
		{URL: "https://example.com", Sampling: 10, Interval: badoption.Duration(10 * time.Second)},
		{URL: "https://example.com", Sampling: 10, Interval: badoption.Duration(5 * time.Minute)},
	} {
		if string(healthStateFingerprint(variant)) != string(fingerprint) {
			t.Fatalf("want the same fingerprint for %+v", variant)
		}
	}
}

func TestSaveWritesTheStatesAndCloseWritesThemOnce(t *testing.T) {
	t.Parallel()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	check.logger = log.NewNOPFactory().NewLogger("healthcheck")
	for i := 0; i < 10; i++ {
		check.Storage.Put("node", RTT(100))
	}
	check.saveHealthState(false)
	if store.saved != 1 {
		t.Fatalf("want one write, got %d", store.saved)
	}
	if floorOf(store.states["node"]) != 100 {
		t.Fatalf("want the floor written, got %v", store.states)
	}
	// Nothing was measured since, so the next period writes nothing.
	check.saveHealthState(false)
	if store.saved != 1 {
		t.Fatalf("want no write while nothing moves, got %d", store.saved)
	}
	// Close writes regardless, and only once however many times it is called.
	check.closeHealthState(nil)
	check.closeHealthState(nil)
	if store.saved != 2 {
		t.Fatalf("want the close to write once, got %d writes", store.saved)
	}
}

func TestSaveLeavesTheNodesOfAnotherCheckerAlone(t *testing.T) {
	t.Parallel()
	// Two checks with the same options but different nodes share a fingerprint. Each writes
	// only the nodes it measures, so neither can carry a stale copy of the other's back.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	check.Storage.Put("ours", RTT(100))
	check.saveHealthState(false)
	store.states["theirs"] = adapter.HealthState{Floors: floorValues(now.Add(-time.Hour), 300), FloorsAt: now.Add(-time.Hour)}
	check.Storage.Put("ours", RTT(120))
	check.saveHealthState(false)
	if state, loaded := store.states["theirs"]; !loaded || floorOf(state) != 300 {
		t.Fatalf("want the other checker's node untouched, got %v", state)
	}
	if floorOf(store.states["ours"]) != 100 {
		t.Fatalf("want our floor written, got %v", store.states["ours"])
	}
}

func TestHealthCheckWritesAndReadsItsCacheFile(t *testing.T) {
	t.Parallel()
	// The end of the flow: a checker that stops writes what a restart needs, and a checker
	// built on the same file starts from the floor the last one measured.
	path := filepath.Join(t.TempDir(), "cache.db")
	options := &option.HealthCheckOptions{
		URL:      "https://example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}

	first, closeFirst := newHealthCheckWithCacheFile(t, path, options)
	for i := 0; i < 10; i++ {
		first.Storage.Put("node", RTT(90))
	}
	first.Storage.Stats("node")
	closeFirst()

	second, closeSecond := newHealthCheckWithCacheFile(t, path, options)
	defer closeSecond()
	attachNodes(second, "node")
	second.restoreHealthState()
	for i := 0; i < 10; i++ {
		second.Storage.Put("node", RTT(600))
	}
	if got := second.Storage.Stats("node").Floor; got != 90 {
		t.Fatalf("want the floor of the previous run, 90, got %d", got)
	}
}

func TestHealthCheckIgnoresACacheFileOfAnotherMeasurement(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache.db")
	first, closeFirst := newHealthCheckWithCacheFile(t, path, &option.HealthCheckOptions{
		URL:      "https://example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	})
	for i := 0; i < 10; i++ {
		first.Storage.Put("node", RTT(90))
	}
	first.Storage.Stats("node")
	closeFirst()

	// Same nodes, another target: the floor of the other path says nothing about this one.
	second, closeSecond := newHealthCheckWithCacheFile(t, path, &option.HealthCheckOptions{
		URL:      "https://example.org",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	})
	defer closeSecond()
	attachNodes(second, "node")
	second.restoreHealthState()
	for i := 0; i < 10; i++ {
		second.Storage.Put("node", RTT(600))
	}
	if got := second.Storage.Stats("node").Floor; got != 600 {
		t.Fatalf("want the floor of this run, 600, got %d", got)
	}
}

// newCacheContext opens a cache file and returns the context every checker of the test shares,
// the way one box shares one file, with the function that releases it.
func newCacheContext(t *testing.T, path string) (context.Context, func()) {
	t.Helper()
	ctx := service.ContextWith(context.Background(), log.NewNOPFactory())
	cacheFile := cachefile.New(ctx, log.NewNOPFactory().NewLogger("cache-file"), option.CacheFileOptions{Path: path})
	service.MustRegister[adapter.CacheFile](ctx, cacheFile)
	scope := adapter.NewScope(ctx, nil)
	if err := cacheFile.Start(adapter.StartStateInitialize, scope); err != nil {
		t.Fatal(err)
	}
	return ctx, func() {
		_ = scope.Close()
	}
}

// newHealthCheckWithCacheFile builds a checker that keeps its states in path, and returns the
// function that closes both: the file has to be released before the next checker opens it.
func newHealthCheckWithCacheFile(t *testing.T, path string, options *option.HealthCheckOptions) (*HealthCheck, func()) {
	t.Helper()
	ctx, closeCache := newCacheContext(t, path)
	check := NewHealthCheck(ctx, nil, nil, options, log.NewNOPFactory().NewLogger("healthcheck"))
	return check, func() {
		_ = check.Close()
		closeCache()
	}
}

// fakeOutbound is a node of a fakeProvider. Only its tag is ever read here.
type fakeOutbound struct {
	adapter.Outbound
	tag string
}

func (o *fakeOutbound) Tag() string { return o.tag }

// fakeProvider is a provider holding a fixed set of nodes. A non-nil ready channel makes it
// not loaded until that channel is closed, the way a remote provider is not ready until its
// first update finishes.
type fakeProvider struct {
	tags  []string
	ready chan struct{}
}

func (p *fakeProvider) Type() string         { return "fake" }
func (p *fakeProvider) Tag() string          { return "fake" }
func (p *fakeProvider) Update() error        { return nil }
func (p *fakeProvider) UpdatedAt() time.Time { return time.Time{} }
func (p *fakeProvider) Wait() {
	if p.ready != nil {
		<-p.ready
	}
}
func (p *fakeProvider) Outbound(tag string) (adapter.Outbound, bool) {
	for _, node := range p.tags {
		if node == tag {
			return &fakeOutbound{tag: node}, true
		}
	}
	return nil, false
}
func (p *fakeProvider) Outbounds() []adapter.Outbound {
	outbounds := make([]adapter.Outbound, 0, len(p.tags))
	for _, node := range p.tags {
		outbounds = append(outbounds, &fakeOutbound{tag: node})
	}
	return outbounds
}

func TestRestoreTakesOnlyTheNodesTheProvidersHave(t *testing.T) {
	t.Parallel()
	// A node that left the subscription, or whose provider was renamed, is never checked
	// again. Its record is simply not taken at the restore, so no name is kept for a check
	// that will never come.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	store.SaveHealthStates(check.stateKey, map[string]adapter.HealthState{
		"known":   {Floors: floorValues(now.Add(-time.Hour), 120), FloorsAt: now.Add(-time.Hour)},
		"renamed": {Floors: floorValues(now.Add(-time.Hour), 220), FloorsAt: now.Add(-time.Hour)},
	}, time.Now().Add(time.Hour))
	check.mergedProviders.Set("group", []adapter.Provider{&fakeProvider{tags: []string{"known"}}})

	check.restoreHealthState()
	if got, ok := seededFloor(check, "known", now); !ok || got != 120 {
		t.Fatalf("want the known node restored with a floor of 120, got %d (%v)", got, ok)
	}
	if _, ok := seededFloor(check, "renamed", now); ok {
		t.Fatal("want the node the providers no longer have left out of memory")
	}
}

func TestRestoreDropsTheRankingsBuiltBeforeIt(t *testing.T) {
	t.Parallel()
	// A report can be asked for before the first round measured anything, and the ranking it
	// read holds the statistics the storages had while they were empty. The restore drops
	// those rankings, so the floors it puts back are what the next report describes.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	store.SaveHealthStates(check.stateKey, map[string]adapter.HealthState{
		"node": {Floors: floorValues(now.Add(-time.Hour), 120), FloorsAt: now.Add(-time.Hour)},
	}, time.Now().Add(time.Hour))
	attachNodes(check, "node")
	notified := 0
	check.RegisterPostCheckListener(func() { notified++ })

	check.restoreHealthState()
	if notified != 1 {
		t.Fatalf("want the rankings dropped once by the restore, got %d", notified)
	}
	// reading the same record again puts nothing back, so nothing is dropped again
	check.restoreHealthState()
	if notified != 1 {
		t.Fatalf("want the restore to notify once, got %d", notified)
	}
}

func TestRestoreReadsEveryRegisteredProvider(t *testing.T) {
	t.Parallel()
	// The states are read once, when every group has registered its providers: the nodes of a
	// shared check are all there at that moment, whatever group they arrived with.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	store.SaveHealthStates(check.stateKey, map[string]adapter.HealthState{
		"first/node":  {Floors: floorValues(now.Add(-time.Hour), 120), FloorsAt: now.Add(-time.Hour)},
		"second/node": {Floors: floorValues(now.Add(-time.Hour), 220), FloorsAt: now.Add(-time.Hour)},
	}, time.Now().Add(time.Hour))
	check.mergedProviders.Set("first", []adapter.Provider{&fakeProvider{tags: []string{"first/node"}}})
	check.mergedProviders.Set("second", []adapter.Provider{&fakeProvider{tags: []string{"second/node"}}})

	check.restoreHealthState()
	if got, ok := seededFloor(check, "first/node", now); !ok || got != 120 {
		t.Fatalf("want the node of the first group restored with a floor of 120, got %d (%v)", got, ok)
	}
	if got, ok := seededFloor(check, "second/node", now); !ok || got != 220 {
		t.Fatalf("want the node of the second group restored with a floor of 220, got %d (%v)", got, ok)
	}
}

func TestManagerReadsTheStatesAtThePostStart(t *testing.T) {
	t.Parallel()
	// A group that is never used is never checked, so the states cannot wait for the first
	// round: they are read at the post start, and a report states the floor of a node that this
	// run never measured.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	store.SaveHealthStates(check.stateKey, map[string]adapter.HealthState{
		"node": {Floors: floorValues(now.Add(-time.Hour), 120), FloorsAt: now.Add(-time.Hour)},
	}, time.Now().Add(time.Hour))
	check.SetProviders("group", []adapter.Provider{&fakeProvider{tags: []string{"node"}}})
	manager := &Manager{checks: map[string]*HealthCheck{"check": check}}

	err := manager.Start(adapter.StartStatePostStart, nil)
	if err != nil {
		t.Fatalf("start post stage: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if got, ok := seededFloor(check, "node", now); ok {
			if got != 120 {
				t.Fatalf("want a floor of 120, got %d", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("want the states read at the post start, without a check")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRestoreWaitsForTheProvidersToLoad(t *testing.T) {
	t.Parallel()
	// Attached is not loaded: a remote provider reads its own cache while the groups start,
	// and a list that is still empty is not evidence that the nodes are gone.
	now := time.Now()
	store := &fakeStateStore{}
	check := newStoredCheck(store)
	store.SaveHealthStates(check.stateKey, map[string]adapter.HealthState{
		"node": {Floors: floorValues(now.Add(-time.Hour), 120), FloorsAt: now.Add(-time.Hour)},
	}, time.Now().Add(time.Hour))
	ready := make(chan struct{})
	check.mergedProviders.Set("group", []adapter.Provider{&fakeProvider{tags: []string{"node"}, ready: ready}})

	restored := make(chan struct{})
	go func() {
		check.restoreHealthState()
		close(restored)
	}()
	select {
	case <-restored:
		t.Fatal("want the restore to wait for the provider")
	case <-time.After(20 * time.Millisecond):
	}
	if _, ok := seededFloor(check, "node", now); ok {
		t.Fatal("want nothing restored while the provider is still loading")
	}

	close(ready)
	<-restored
	if got, ok := seededFloor(check, "node", now); !ok || got != 120 {
		t.Fatalf("want the floor restored once the provider is loaded, got %d (%v)", got, ok)
	}
}

func TestTwoChecksWithDifferentTargetsKeepTheirOwnNodes(t *testing.T) {
	t.Parallel()
	// One configuration can hold several health checks, each measuring its own nodes through
	// its own target. They share the file, so a write of one must not take the other's nodes
	// with it.
	path := filepath.Join(t.TempDir(), "cache.db")
	firstOptions := &option.HealthCheckOptions{
		URL:      "https://first.example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}
	secondOptions := &option.HealthCheckOptions{
		URL:      "https://second.example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}
	ctx, closeCache := newCacheContext(t, path)
	first := NewHealthCheck(ctx, nil, nil, firstOptions, log.NewNOPFactory().NewLogger("healthcheck"))
	second := NewHealthCheck(ctx, nil, nil, secondOptions, log.NewNOPFactory().NewLogger("healthcheck"))
	for i := 0; i < 10; i++ {
		first.Storage.Put("first/node", RTT(100))
		second.Storage.Put("second/node", RTT(200))
	}
	first.Storage.Stats("first/node")
	second.Storage.Stats("second/node")
	first.saveHealthState(true)
	second.saveHealthState(true)
	_ = first.Close()
	_ = second.Close()
	closeCache()

	reopenCtx, closeReopened := newCacheContext(t, path)
	defer closeReopened()
	reopenedFirst := NewHealthCheck(reopenCtx, nil, nil, firstOptions, log.NewNOPFactory().NewLogger("healthcheck"))
	attachNodes(reopenedFirst, "first/node")
	reopenedFirst.restoreHealthState()
	if got, ok := seededFloor(reopenedFirst, "first/node", time.Now()); !ok || got != 100 {
		t.Fatalf("want the first check's floor back, got %d (%v)", got, ok)
	}
	if _, ok := seededFloor(reopenedFirst, "second/node", time.Now()); ok {
		t.Fatal("want the first check to know nothing about the second's nodes")
	}

	reopenedSecond := NewHealthCheck(reopenCtx, nil, nil, secondOptions, log.NewNOPFactory().NewLogger("healthcheck"))
	attachNodes(reopenedSecond, "second/node")
	reopenedSecond.restoreHealthState()
	if got, ok := seededFloor(reopenedSecond, "second/node", time.Now()); !ok || got != 200 {
		t.Fatalf("want the second check's floor back, got %d (%v)", got, ok)
	}
}

func TestTwoChecksWithTheSamePathKeepTheNodesOfBoth(t *testing.T) {
	t.Parallel()
	// Two checks with identical options but different node lists share a fingerprint and the
	// same entries of the file. Each writes only the nodes it measures, so what one saves is
	// what the other leaves alone, and a restart reads both lists back exactly.
	path := filepath.Join(t.TempDir(), "cache.db")
	options := &option.HealthCheckOptions{
		URL:      "https://same.example.com",
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	}
	ctx, closeCache := newCacheContext(t, path)
	first := NewHealthCheck(ctx, nil, nil, options, log.NewNOPFactory().NewLogger("healthcheck"))
	second := NewHealthCheck(ctx, nil, nil, options, log.NewNOPFactory().NewLogger("healthcheck"))
	for i := 0; i < 10; i++ {
		first.Storage.Put("first/node", RTT(100))
		second.Storage.Put("second/node", RTT(200))
	}
	first.Storage.Stats("first/node")
	second.Storage.Stats("second/node")
	// Both save twice, alternating, the way two periodical writers would.
	for i := 0; i < 2; i++ {
		first.saveHealthState(true)
		second.saveHealthState(true)
	}
	_ = first.Close()
	_ = second.Close()
	closeCache()

	reopenCtx, closeReopened := newCacheContext(t, path)
	defer closeReopened()
	reopened := NewHealthCheck(reopenCtx, nil, nil, options, log.NewNOPFactory().NewLogger("healthcheck"))
	attachNodes(reopened, "first/node", "second/node")
	reopened.restoreHealthState()
	if got, ok := seededFloor(reopened, "first/node", time.Now()); !ok || got != 100 {
		t.Fatalf("want the first list back with a floor of 100, got %d (%v)", got, ok)
	}
	if got, ok := seededFloor(reopened, "second/node", time.Now()); !ok || got != 200 {
		t.Fatalf("want the second list back with a floor of 200, got %d (%v)", got, ok)
	}
}
