package healthcheck_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/healthcheck"
)

func TestStorageStats(t *testing.T) {
	t.Parallel()
	rtts := []healthcheck.RTT{60, 140, 60, 140, 60, 60, 140, 60, 140}
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 4, Validity: time.Hour})
	for _, rtt := range rtts {
		s.Put(rtt)
	}
	// Jitter: the median absolute deviation of the delays that are kept, {60, 140}, is
	// 40, scaled to an estimate of the standard deviation: 1.4826 × 40 ≈ 59.
	// MeanDeviation: every distance is 40, so their mean is 40 as well.
	// Spread: the sample standard deviation of {60, 60, 140, 140} is 46, and the distances
	// are all 40, so their own spread is 0.
	// Floor: the fastest check the node has been, 60ms. Bad: the checks above twice that,
	// 120ms, which is the two of 140ms among the window of {60, 60, 140, 140}.
	want := healthcheck.Stats{
		All:             4,
		Fail:            0,
		Jitter:          59,
		MeanDeviation:   40,
		Spread:          46,
		DeviationSpread: 0,
		Floor:           60,
		Bad:             2,
		Average:         100,
		Median:          100,
		Max:             140,
		Min:             60,
		Latest:          140,
	}
	assertStats(t, "Stats() - All Success", want, s.Stats())

	// The delay statistics describe the checks that succeeded: a failure says nothing
	// about the delay the node has when it works, it is reported by Fail and Latest.
	// The window keeps the last four checks, {Failed, Failed, 140, 60}, so the floor is
	// still 60ms and of the two delays that succeeded only the 140ms is above twice it: a
	// failure is not a delay, so it does not count as a bad one.
	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want = healthcheck.Stats{
		All:             4,
		Fail:            2,
		Jitter:          59,
		MeanDeviation:   40,
		Spread:          57,
		DeviationSpread: 0,
		Floor:           60,
		Bad:             1,
		Average:         100,
		Median:          100,
		Max:             140,
		Min:             60,
		Latest:          healthcheck.Failed,
	}
	assertStats(t, "Stats() - Half Fail", want, s.Stats())

	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	// the floor outlives the window: it is what the node can do, not what it is doing
	want = healthcheck.Stats{
		All:    4,
		Fail:   4,
		Floor:  60,
		Latest: healthcheck.Failed,
	}
	assertStats(t, "Stats() - All Fail", want, s.Stats())
}

func TestASingleCheckSetsTheFloor(t *testing.T) {
	t.Parallel()
	// An idle node is probed once a window, and the window the statistics read is shorter than
	// the gap between two probes: its round holds that one check and nothing else. The floor has
	// to come from it all the same, or a group that is never used would never get one.
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 10, Validity: 22 * time.Minute})
	s.Put(150)
	if got := s.Stats().Floor; got != 150 {
		t.Fatalf("want the floor of the single check, 150, got %d", got)
	}
}

func TestTheFloorIsWrittenByTheCheckNotByTheRead(t *testing.T) {
	t.Parallel()
	// A check fills its slot when it is measured, so the floor is there even when nothing read
	// the statistics while the check was still inside the window: an idle group is not read.
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 10, Validity: time.Nanosecond})
	s.Put(150)
	if got := s.Stats().Floor; got != 150 {
		t.Fatalf("want the floor of the check once its window expired, 150, got %d", got)
	}
}

func TestStorageStatsFirstCheckFailed(t *testing.T) {
	t.Parallel()
	// A node that only failed has no delay to report, and the zero of a missing value
	// would read like the best possible result.
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 10, Validity: time.Hour})
	s.Put(healthcheck.Failed)
	want := healthcheck.Stats{
		All:    1,
		Fail:   1,
		Latest: healthcheck.Failed,
	}
	assertStats(t, "Stats() - First Check Failed", want, s.Stats())
}

func TestStorageStatsMostlyFail(t *testing.T) {
	t.Parallel()
	// A node failing most of its checks is described by the delay of the two that
	// worked: its reliability is not a delay, and hiding it in the delays made it look
	// like the most stable node of all.
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 10, Validity: time.Hour})
	for i := 0; i < 8; i++ {
		s.Put(healthcheck.Failed)
	}
	s.Put(100)
	s.Put(100)
	// The floor is the fastest of the two that worked, 100ms, and neither of them is above
	// twice that.
	want := healthcheck.Stats{
		All:     10,
		Fail:    8,
		Jitter:  0,
		Floor:   100,
		Average: 100,
		Median:  100,
		Max:     100,
		Min:     100,
		Latest:  100,
	}
	assertStats(t, "Stats() - Mostly Fail", want, s.Stats())
	if got := s.Stats().Succeeded(); got != 2 {
		t.Fatalf("want 2 succeeded checks, got %v", got)
	}
}

func TestStorageStatsIgnoreOutdated(t *testing.T) {
	t.Parallel()
	rtts := []healthcheck.RTT{60, 140, 60, 140}
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 4, Validity: time.Duration(10) * time.Millisecond})
	for i, rtt := range rtts {
		if i == 2 {
			// wait for previous 2 outdated
			time.Sleep(time.Duration(100) * time.Millisecond)
		}
		s.Put(rtt)
	}
	want := healthcheck.Stats{
		All:             2,
		Fail:            0,
		Jitter:          59,
		MeanDeviation:   40,
		Spread:          57,
		DeviationSpread: 0,
		Floor:           60,
		Bad:             1,
		Average:         100,
		Median:          100,
		Max:             140,
		Min:             60,
		Latest:          140,
	}

	assertStats(t, "Stats() - Half Outdated", want, s.Stats())

	// wait for all outdated
	time.Sleep(time.Duration(100) * time.Millisecond)
	want = healthcheck.Stats{
		All:    0,
		Fail:   0,
		Floor:  60,
		Latest: 0,
	}
	assertStats(t, "Stats() - All Outdated, baseline retained", want, s.Stats())

	s.Put(60)
	// one sample says nothing about how much the node varies, so the spreads are assumed
	// to be half of its delay; the floor is still the one the window set before it expired
	want = healthcheck.Stats{
		All:           1,
		Fail:          0,
		Jitter:        30,
		MeanDeviation: 30,
		Floor:         60,
		Average:       60,
		Median:        60,
		Max:           60,
		Min:           60,
		Latest:        60,
	}
	assertStats(t, "Stats() - Put After Outdated", want, s.Stats())
}

func TestRTTStorageGetFromNil(t *testing.T) {
	t.Parallel()
	s := (*healthcheck.Storage)(nil)
	want := healthcheck.Stats{}
	assertStats(t, "nil.Stats()", want, s.Stats())
	if got := s.Stats().Cost(100, 0); got != healthcheck.Failed {
		t.Fatalf("want no mixed value without samples, got %v", got)
	}
}

func TestStatsCost(t *testing.T) {
	t.Parallel()
	// An attempt either succeeds with the cost, or fails after waiting for the
	// timeout, so the two are mixed by the share of the checks that failed.
	testCases := []struct {
		name   string
		all    int
		fail   int
		base   healthcheck.RTT
		spread healthcheck.RTT
		want   healthcheck.RTT
	}{
		{name: "no failure", all: 10, base: 100, want: 100},
		// the spread is added as it is, a millisecond of it costs a millisecond
		{name: "jitter", all: 10, base: 100, spread: 20, want: 120},
		// the mean deviation is a spread like any other, it is just a different one
		{name: "mean deviation", all: 10, base: 0, spread: 457, want: 457},
		// (9*100 + 1*300) / 10: a failed attempt is worth three that worked
		{name: "one failure", all: 10, fail: 1, base: 100, want: 120},
		// (5*100 + 5*300) / 10
		{name: "half failed", all: 10, fail: 5, base: 100, want: 200},
		// (9*6000 + 1*15000) / 10, the failure cost is capped by the timeout
		{name: "slow node, one failure", all: 10, fail: 1, base: 6000, want: 6900},
		// every attempt waits for the timeout
		{name: "all failed", all: 10, fail: 10, want: 15000},
		// a cost measured with a longer timeout is still capped by the attempt
		{name: "cost above the timeout", all: 10, base: 30000, want: 15000},
		// no sample at all, the zero of a missing value would read as the best result
		{name: "never checked", want: healthcheck.Failed},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			stats := healthcheck.Stats{All: testCase.all, Fail: testCase.fail}
			got := stats.Cost(testCase.base, testCase.spread)
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestStatsCostScalesWithTheNode(t *testing.T) {
	t.Parallel()
	// A failed attempt is scored in the scale of the node, so the same failure rate
	// costs a fast and a slow node the same share of their delay. Scoring it with the
	// timeout would bury the fast node behind every node that never failed, and barely
	// move the slow one.
	testCases := []struct {
		name string
		base healthcheck.RTT
		want healthcheck.RTT
	}{
		// (9*100 + 1*300) / 10
		{name: "fast", base: 100, want: 120},
		// (9*600 + 1*1800) / 10
		{name: "slow", base: 600, want: 720},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			stats := healthcheck.Stats{All: 10, Fail: 1}
			got := stats.Cost(testCase.base, 0)
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func delaysOf(checks []*healthcheck.History) []healthcheck.RTT {
	delays := make([]healthcheck.RTT, 0, len(checks))
	for _, check := range checks {
		delays = append(delays, check.Delay)
	}
	return delays
}

func assertStats(t *testing.T, name string, want, got healthcheck.Stats) {
	// do not comapre times
	want.Expires = time.Time{}
	got.Expires = time.Time{}
	// The failure estimate is wall-clock, and these histories are written in a tight loop:
	// it is a measurement of the clock rather than of the checks, so it has a test of its
	// own, see TestFailureRateFollowsTheClock.
	want.FailureRate = got.FailureRate
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("[%s] want: %v, got: %v", name, want, got)
	}
}

func TestStatsCostZeroIsNotFailed(t *testing.T) {
	t.Parallel()
	// quick costs a node by its median plus jitter, and a node with no jitter has
	// none. Zero is reserved for a missing value, so the cost must stay measurable.
	stats := healthcheck.Stats{All: 10, Median: 100, Jitter: 0, Latest: 100}
	if cost := stats.Cost(0, 0); cost != healthcheck.Millisecond {
		t.Fatalf("want the smallest delay, got %v", cost)
	}
	// a node that never worked has no measurable cost, the zero is its absence
	if cost := (healthcheck.Stats{}).Cost(0, 0); cost != healthcheck.Failed {
		t.Fatalf("want no cost, got %v", cost)
	}
}

func TestStatsOfMatchesStorage(t *testing.T) {
	t.Parallel()
	// The statistics of a window cut out of the history must be the statistics the
	// storage computes for that same window: the evaluation of a window is only worth
	// reading if it prices the checks the way the group priced them.
	rtts := []healthcheck.RTT{60, 140, 60, 0, 80, 60, 140, 60, 200, 60}
	s := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: uint(len(rtts)), Validity: time.Hour})
	for _, rtt := range rtts {
		s.Put(rtt)
	}
	stats := s.Stats()

	checks := s.All()
	floor := stats.Floor
	got := healthcheck.StatsOf(checks, floor, stats.Expires, healthcheck.ReplayFailureRate(checks))
	assertStats(t, "StatsOf() - whole history", stats, got)

	// A window of the most recent checks, measured against the same baseline: every
	// statistic has to come from that window and nothing else.
	window := checks[:4]
	got = healthcheck.StatsOf(window, floor, stats.Expires, healthcheck.ReplayFailureRate(checks))
	if got.All != 4 {
		t.Fatalf("want 4 checks in the window, got %d", got.All)
	}
	wantMedian := healthcheck.RTT(100) // {60, 200, 60, 140} sorted: 60, 60, 140, 200
	if got.Median != wantMedian {
		t.Fatalf("want median %v, got %v", wantMedian, got.Median)
	}
	if got.Floor != floor {
		t.Fatalf("want the baseline passed in, %v, got %v", floor, got.Floor)
	}

	// Without a baseline the frequency term has nothing to measure against, so it stays
	// zero rather than counting every check as bad.
	got = healthcheck.StatsOf(checks, 0, stats.Expires, healthcheck.ReplayFailureRate(checks))
	if got.Bad != 0 || got.Floor != 0 {
		t.Fatalf("want no baseline and no bad checks, got floor %v and %d bad", got.Floor, got.Bad)
	}

	// An empty window has nothing to say, not a zero measurement.
	if !reflect.DeepEqual(healthcheck.StatsOf(nil, floor, stats.Expires, 0), healthcheck.Stats{}) {
		t.Fatalf("want empty statistics, got %v", healthcheck.StatsOf(nil, floor, stats.Expires, 0))
	}
}

func TestStorageKeepsMoreThanTheWindow(t *testing.T) {
	t.Parallel()
	// The history is kept for measuring the window, never for ranking it: a node that
	// keeps twice the checks must produce exactly the statistics of one that keeps the
	// window alone.
	rtts := []healthcheck.RTT{60, 140, 60, 140, 60, 60, 140, 60, 45, 200}
	window := healthcheck.NewStorage(healthcheck.StorageOptions{
		Sampling: 4,
		Validity: time.Hour,
		Retained: 4,
	})
	kept := healthcheck.NewStorage(healthcheck.StorageOptions{
		Sampling:  4,
		Validity:  time.Hour,
		Retained:  uint(len(rtts)),
		Retention: time.Hour,
	})
	for _, rtt := range rtts {
		window.Put(rtt)
		kept.Put(rtt)
	}
	assertStats(t, "Stats() - window only", window.Stats(), kept.Stats())
	if got := len(kept.All()); got != len(rtts) {
		t.Fatalf("want the whole history kept, got %d checks", got)
	}
	if got := len(window.All()); got != 4 {
		t.Fatalf("want the window kept, got %d checks", got)
	}
	// most recent first, which is what the report hands to the measurement
	if newest := kept.All()[0].Delay; newest != rtts[len(rtts)-1] {
		t.Fatalf("want the newest check first, got %v", newest)
	}
}

func TestStoragesResizeKeepsTheHistory(t *testing.T) {
	t.Parallel()
	storages := healthcheck.NewStorages(healthcheck.StorageOptions{Sampling: 4, Validity: time.Hour})
	put := func(values ...healthcheck.RTT) {
		for _, value := range values {
			storages.Put("node", value)
		}
	}
	put(60, 140, 60, 140)
	if got := len(storages.All("node")); got != 4 {
		t.Fatalf("want the four checks of the window, got %d", got)
	}

	// Growing the history keeps what was already collected: a resize must not cost the
	// user the checks they waited for, which is the whole reason it exists.
	applied := storages.Resize(4, 6, time.Hour)
	if applied.Sampling != 4 || applied.Retained != 10 {
		t.Fatalf("want four checks read out of ten kept, got %v", applied)
	}
	if got := len(storages.All("node")); got != 4 {
		t.Fatalf("the resize lost the collected checks: %d left", got)
	}
	put(45, 200, 60, 140, 60, 60)
	if got := len(storages.All("node")); got != 10 {
		t.Fatalf("want the history to grow to ten checks, got %d", got)
	}
	if got := storages.Stats("node").All; got != 4 {
		t.Fatalf("the window moved with the history: %d checks", got)
	}

	// Shrinking keeps the newest checks, and still leaves the window alone.
	newest := delaysOf(storages.All("node")[:4])
	storages.Resize(4, 0, time.Hour)
	if kept := delaysOf(storages.All("node")); !reflect.DeepEqual(kept, newest) {
		t.Fatalf("the shrink kept %v instead of the newest %v", kept, newest)
	}
	if got := storages.Stats("node").All; got != 4 {
		t.Fatalf("the window moved with the history: %d checks", got)
	}
}
