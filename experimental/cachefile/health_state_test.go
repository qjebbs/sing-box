package cachefile

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sagernet/bbolt"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// slotsOf builds a day of slots holding the given values, which is what a state carries.
func slotsOf(values map[int]uint16) []uint16 {
	slots := make([]uint16, 48)
	for index, value := range values {
		slots[index] = value
	}
	return slots
}

// floorOf returns the fastest of the slots of a state, which is the floor it holds.
func floorOf(state adapter.HealthState) uint16 {
	var floor uint16
	for _, value := range state.Floors {
		if value != 0 && (floor == 0 || value < floor) {
			floor = value
		}
	}
	return floor
}

func TestHealthStateRoundTrips(t *testing.T) {
	t.Parallel()
	// What is written is read back field for field: the day of slots comes back with the moment
	// it was read, and a failure estimate is two weighted counts and the moment they stand at.
	readAt := time.Unix(1700000000, 0)
	failureAt := readAt.Add(37 * time.Minute)
	states := map[string]adapter.HealthState{
		"Provider/Node A": {
			Floors:        slotsOf(map[int]uint16{17: 143, 18: 151}),
			FloorsAt:      readAt,
			FailureSum:    1.25,
			FailureWeight: 9.5,
			FailureAt:     failureAt,
		},
		"Provider/Node B": {
			Floors:   slotsOf(map[int]uint16{0: 900}),
			FloorsAt: readAt.Add(time.Hour),
		},
	}
	for tag, want := range states {
		content, err := encodeHealthState(want, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		got, loaded := decodeHealthState(content)
		if !loaded {
			t.Fatalf("want %q back", tag)
		}
		if !slices.Equal(got.Floors, want.Floors) {
			t.Fatalf("%q: want the slots %v, got %v", tag, want.Floors, got.Floors)
		}
		if !got.FloorsAt.Equal(want.FloorsAt) {
			t.Fatalf("%q: want the day read at %v, got %v", tag, want.FloorsAt, got.FloorsAt)
		}
		if got.FailureSum != want.FailureSum || got.FailureWeight != want.FailureWeight {
			t.Fatalf("%q: want %v/%v, got %v/%v", tag, want.FailureSum, want.FailureWeight, got.FailureSum, got.FailureWeight)
		}
		if !got.FailureAt.Equal(want.FailureAt) {
			t.Fatalf("%q: want the estimate at %v, got %v", tag, want.FailureAt, got.FailureAt)
		}
	}
}

func TestHealthStateKeysFollowTheFingerprint(t *testing.T) {
	t.Parallel()
	// A check keeps its records under its own key, so a record measured through another target
	// or another relay path is never read as this one.
	states := map[string]adapter.HealthState{
		"node": {Floors: slotsOf(map[int]uint16{1: 100}), FloorsAt: time.Unix(1700000000, 0)},
	}
	content, err := encodeHealthState(states["node"], time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeHealthState(content); !ok {
		t.Fatal("want the record back")
	}
}

func TestHealthStateRejectsTruncatedContent(t *testing.T) {
	t.Parallel()
	// A record is read from a file that may have been cut short by a crash: a partial one is
	// dropped rather than decoded into a floor that was never measured.
	states := map[string]adapter.HealthState{
		"node": {Floors: slotsOf(map[int]uint16{1: 100}), FloorsAt: time.Unix(1700000000, 0)},
	}
	content, err := encodeHealthState(states["node"], time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for length := 0; length < len(content); length++ {
		if _, ok := decodeHealthState(content[:length]); ok {
			t.Fatalf("want nothing from %d of %d bytes", length, len(content))
		}
	}
}

func TestHealthStateZeroTimeIsNotTheEpoch(t *testing.T) {
	t.Parallel()
	// A state without a failure estimate has no time, which is not the same as one recorded
	// in 1970: the first is absent, the second is stale.
	states := map[string]adapter.HealthState{
		"node": {Floors: slotsOf(map[int]uint16{2: 200}), FloorsAt: time.Unix(1700000000, 0)},
	}
	content, err := encodeHealthState(states["node"], time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decodeHealthState(content)
	if !ok {
		t.Fatal("want the record back")
	}
	if !got.FailureAt.IsZero() {
		t.Fatalf("want no failure time, got %v", got.FailureAt)
	}
}

func TestHealthStatesSurviveAReopen(t *testing.T) {
	t.Parallel()
	// The states are read by the next process from the same file, and the startup cleanup
	// must leave their bucket alone: a bucket name missing from the keep list is deleted on
	// open, which would make the cache look like it never worked.
	path := filepath.Join(t.TempDir(), "cache.db")
	fingerprint := []byte{9, 8, 7, 6, 5, 4, 3, 2}
	readAt := time.Unix(1700000000, 0)
	states := map[string]adapter.HealthState{
		"Provider/Node": {Floors: slotsOf(map[int]uint16{42: 321}), FloorsAt: readAt, FailureSum: 0.5, FailureWeight: 4, FailureAt: readAt},
	}

	first := newTestCacheFile(t, path)
	if err := first.SaveHealthStates(fingerprint, states, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	closeTestCacheFile(t, first)

	second := newTestCacheFile(t, path)
	defer closeTestCacheFile(t, second)
	loaded := second.LoadHealthStates(fingerprint)
	if len(loaded) != 1 {
		t.Fatalf("want the state to survive the reopen, got %v", loaded)
	}
	if got := loaded["Provider/Node"]; floorOf(got) != 321 || got.Floors[42] != 321 {
		t.Fatalf("want the floor of 321 in slot 42, got %v", got)
	}
	if loaded := second.LoadHealthStates([]byte{1, 2, 3, 4, 5, 6, 7, 8}); loaded != nil {
		t.Fatalf("want nothing for another fingerprint, got %v", loaded)
	}
}

func newTestCacheFile(t *testing.T, path string) *CacheFile {
	t.Helper()
	cache := New(context.Background(), log.NewNOPFactory().NewLogger("cache-file"), option.CacheFileOptions{Path: path})
	scope := adapter.NewScope(context.Background(), nil)
	if err := cache.Start(adapter.StartStateInitialize, scope); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cache.Start(adapter.StartStateStart, scope)
		_ = scope.Close()
	})
	return cache
}

func closeTestCacheFile(t *testing.T, cache *CacheFile) {
	t.Helper()
	if err := cache.database().Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredHealthStatesAreDroppedOnOpen(t *testing.T) {
	t.Parallel()
	// A health check that is no longer configured never writes again, so nothing inside the
	// checker can remove its record: the file has to drop the set itself, once the lifetime
	// written with it has passed.
	path := filepath.Join(t.TempDir(), "cache.db")
	fingerprint := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	states := map[string]adapter.HealthState{
		"gone": {Floors: slotsOf(map[int]uint16{3: 300}), FloorsAt: time.Unix(1700000000, 0)},
	}

	first := newTestCacheFile(t, path)
	if err := first.SaveHealthStates(fingerprint, states, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	closeTestCacheFile(t, first)

	second := newTestCacheFile(t, path)
	defer closeTestCacheFile(t, second)
	if loaded := second.LoadHealthStates(fingerprint); loaded != nil {
		t.Fatalf("want the expired set dropped on open, got %v", loaded)
	}
	second.DB.View(func(tx *bbolt.Tx) error {
		if bucket := healthStatesOf(tx, second, fingerprint); bucket != nil {
			t.Fatal("want the expired records removed from the file")
		}
		return nil
	})
}

func TestHealthStatesWithinTheirLifetimeSurviveAnOpen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache.db")
	fingerprint := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	states := map[string]adapter.HealthState{
		"kept": {Floors: slotsOf(map[int]uint16{4: 400}), FloorsAt: time.Unix(1700000000, 0)},
	}

	first := newTestCacheFile(t, path)
	if err := first.SaveHealthStates(fingerprint, states, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	closeTestCacheFile(t, first)

	second := newTestCacheFile(t, path)
	defer closeTestCacheFile(t, second)
	loaded := second.LoadHealthStates(fingerprint)
	if len(loaded) != 1 || floorOf(loaded["kept"]) != 400 {
		t.Fatalf("want the set still there, got %v", loaded)
	}
}

func TestHealthStatesOfDifferentChecksAreKeptApart(t *testing.T) {
	t.Parallel()
	// Two health checks in one configuration write to the same file. Each works on its own
	// nodes with its own floors, so neither may overwrite the other.
	path := filepath.Join(t.TempDir(), "cache.db")
	firstKey := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	secondKey := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	expiresAt := time.Now().Add(time.Hour)

	cache := newTestCacheFile(t, path)
	if err := cache.SaveHealthStates(firstKey, map[string]adapter.HealthState{
		"only/in/the/first": {Floors: slotsOf(map[int]uint16{1: 111}), FloorsAt: time.Unix(1700000000, 0)},
	}, expiresAt); err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveHealthStates(secondKey, map[string]adapter.HealthState{
		"only/in/the/second": {Floors: slotsOf(map[int]uint16{2: 222}), FloorsAt: time.Unix(1700000000, 0)},
	}, expiresAt); err != nil {
		t.Fatal(err)
	}
	closeTestCacheFile(t, cache)

	reopened := newTestCacheFile(t, path)
	defer closeTestCacheFile(t, reopened)
	first := reopened.LoadHealthStates(firstKey)
	if len(first) != 1 || floorOf(first["only/in/the/first"]) != 111 {
		t.Fatalf("want the first check's node back, got %v", first)
	}
	second := reopened.LoadHealthStates(secondKey)
	if len(second) != 1 || floorOf(second["only/in/the/second"]) != 222 {
		t.Fatalf("want the second check's node back, got %v", second)
	}
	if loaded := reopened.LoadHealthStates([]byte{3, 3, 3, 3, 3, 3, 3, 3}); loaded != nil {
		t.Fatalf("want nothing for a check that never wrote, got %v", loaded)
	}
}
