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
	s := healthcheck.NewStorage(4, time.Hour)
	for _, rtt := range rtts {
		s.Put(rtt)
	}
	want := healthcheck.Stats{
		All:       4,
		Fail:      0,
		Deviation: 40,
		Average:   100,
		Median:    100,
		Max:       140,
		Min:       60,
		Latest:    140,
	}
	assertStats(t, "Stats() - All Success", want, s.Stats())

	// The delay statistics describe the checks that succeeded: a failure says nothing
	// about the delay the node has when it works, it is reported by Fail and Latest.
	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want = healthcheck.Stats{
		All:       4,
		Fail:      2,
		Deviation: 40,
		Average:   100,
		Median:    100,
		Max:       140,
		Min:       60,
		Latest:    healthcheck.Failed,
	}
	assertStats(t, "Stats() - Half Fail", want, s.Stats())

	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want = healthcheck.Stats{
		All:    4,
		Fail:   4,
		Latest: healthcheck.Failed,
	}
	assertStats(t, "Stats() - All Fail", want, s.Stats())
}

func TestStorageStatsFirstCheckFailed(t *testing.T) {
	t.Parallel()
	// A node that only failed has no delay to report, and the zero of a missing value
	// would read like the best possible result.
	s := healthcheck.NewStorage(10, time.Hour)
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
	s := healthcheck.NewStorage(10, time.Hour)
	for i := 0; i < 8; i++ {
		s.Put(healthcheck.Failed)
	}
	s.Put(100)
	s.Put(100)
	want := healthcheck.Stats{
		All:       10,
		Fail:      8,
		Deviation: 0,
		Average:   100,
		Median:    100,
		Max:       100,
		Min:       100,
		Latest:    100,
	}
	assertStats(t, "Stats() - Mostly Fail", want, s.Stats())
	if got := s.Stats().Succeeded(); got != 2 {
		t.Fatalf("want 2 succeeded checks, got %v", got)
	}
}

func TestStorageStatsIgnoreOutdated(t *testing.T) {
	t.Parallel()
	rtts := []healthcheck.RTT{60, 140, 60, 140}
	s := healthcheck.NewStorage(4, time.Duration(10)*time.Millisecond)
	for i, rtt := range rtts {
		if i == 2 {
			// wait for previous 2 outdated
			time.Sleep(time.Duration(100) * time.Millisecond)
		}
		s.Put(rtt)
	}
	want := healthcheck.Stats{
		All:       2,
		Fail:      0,
		Deviation: 40,
		Average:   100,
		Median:    100,
		Max:       140,
		Min:       60,
		Latest:    140,
	}

	assertStats(t, "Stats() - Half Outdated", want, s.Stats())

	// wait for all outdated
	time.Sleep(time.Duration(100) * time.Millisecond)
	want = healthcheck.Stats{
		All:       0,
		Fail:      0,
		Deviation: 0,
		Average:   0,
		Median:    0,
		Max:       0,
		Min:       0,
		Latest:    0,
	}
	assertStats(t, "Stats() - All Outdated", want, s.Stats())

	s.Put(60)
	want = healthcheck.Stats{
		All:  1,
		Fail: 0,
		// 1 sample, std=0.5rtt
		Deviation: 30,
		Average:   60,
		Median:    60,
		Max:       60,
		Min:       60,
		Latest:    60,
	}
	assertStats(t, "Stats() - Put After Outdated", want, s.Stats())
}

func TestRTTStorageGetFromNil(t *testing.T) {
	t.Parallel()
	s := (*healthcheck.Storage)(nil)
	want := healthcheck.Stats{}
	assertStats(t, "nil.Stats()", want, s.Stats())
	if got := s.Stats().Cost(100, 0, 0); got != healthcheck.Failed {
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
		jitter healthcheck.RTT
		weight float32
		want   healthcheck.RTT
	}{
		{name: "no failure", all: 10, base: 100, want: 100},
		// the jitter is added, a millisecond of it costs a millisecond by default
		{name: "jitter", all: 10, base: 100, jitter: 20, weight: 1, want: 120},
		{name: "half weight", all: 10, base: 100, jitter: 20, weight: 0.5, want: 110},
		// the weight is rounded, not truncated
		{name: "fractional weight", all: 10, base: 100, jitter: 7, weight: 0.5, want: 104},
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
			got := stats.Cost(testCase.base, testCase.jitter, testCase.weight)
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
			got := stats.Cost(testCase.base, 0, 0)
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestStorageConnectionOutcomes(t *testing.T) {
	t.Parallel()
	s := healthcheck.NewStorage(10, time.Hour)
	now := time.Now()
	s.ReportConnection(now, true)
	s.ReportConnection(now, true)
	s.ReportConnection(now, false)
	attempts, failures := s.ConnectionOutcomes(now)
	if attempts != 3 || failures != 1 {
		t.Fatalf("want 3 attempts and 1 failure, got %v and %v", attempts, failures)
	}
	// the checks are untouched: a connection is not a sample
	stats := s.Stats()
	if stats.All != 0 {
		t.Fatalf("want no check sample, got %v", stats.All)
	}
	if stats.Connections != 3 || stats.ConnectionFailures != 1 {
		t.Fatalf("want 3 attempts and 1 failure in the statistics, got %v and %v",
			stats.Connections, stats.ConnectionFailures)
	}
	// a connection leaves the window with the checks it is counted next to
	attempts, failures = s.ConnectionOutcomes(now.Add(2 * time.Hour))
	if attempts != 0 || failures != 0 {
		t.Fatalf("want the connections expired, got %v and %v", attempts, failures)
	}
}

func TestStorageConnectionOutcomesLimit(t *testing.T) {
	t.Parallel()
	// The connections are bounded like the checks, so the two describe the same length of
	// the same window instead of one outweighing the other.
	s := healthcheck.NewStorage(10, time.Hour)
	at := time.Now()
	for i := 0; i < 100; i++ {
		s.ReportConnection(at, true)
	}
	attempts, failures := s.ConnectionOutcomes(at)
	if attempts != 10 {
		t.Fatalf("want the connections bounded like the checks, got %v", attempts)
	}
	if failures != 0 {
		t.Fatalf("want no failure, got %v", failures)
	}
}

func TestStorageConnectionOutcomesWindow(t *testing.T) {
	t.Parallel()
	// Only the connections inside the validity window are counted, and the walk stops at
	// the first expired one: the ring is in order.
	s := healthcheck.NewStorage(10, time.Hour)
	at := time.Now()
	s.ReportConnection(at.Add(-2*time.Hour), false)
	s.ReportConnection(at, true)
	s.ReportConnection(at, false)
	attempts, failures := s.ConnectionOutcomes(at)
	if attempts != 2 || failures != 1 {
		t.Fatalf("want 2 attempts and 1 failure, got %v and %v", attempts, failures)
	}
}

func TestStorageConnectionOutcomesAreLazy(t *testing.T) {
	t.Parallel()
	// A node whose traffic never reaches the group doesn't pay for the ring.
	s := healthcheck.NewStorage(10, time.Hour)
	attempts, failures := s.ConnectionOutcomes(time.Now())
	if attempts != 0 || failures != 0 {
		t.Fatalf("want nothing, got %v and %v", attempts, failures)
	}
	// both the checks and the statistics still work
	s.Put(100)
	if got := s.Stats().Median; got != 100 {
		t.Fatalf("want the median of the check, got %v", got)
	}
}

func TestStatsCostWithConnections(t *testing.T) {
	t.Parallel()
	// A connection is an attempt too, so it is mixed in next to the checks. Only the
	// share that failed counts, not how many were carried: (10*100 + 2*300) / 12
	stats := healthcheck.Stats{All: 10, Connections: 2, ConnectionFailures: 2}
	if got := stats.Cost(100, 0, 0); got != 133 {
		t.Fatalf("want 133, got %v", got)
	}
	// the same share over more traffic scores worse only because the sample is larger,
	// a busy node is not punished for the traffic itself: (10*100 + 20*300) / 30
	stats = healthcheck.Stats{All: 10, Connections: 20, ConnectionFailures: 20}
	if got := stats.Cost(100, 0, 0); got != 233 {
		t.Fatalf("want 233, got %v", got)
	}
	// without a check but with connections there is no delay, the timeout is all that is
	// known about the node
	stats = healthcheck.Stats{Connections: 3, ConnectionFailures: 3}
	if got := stats.Cost(0, 0, 0); got != 15000 {
		t.Fatalf("want the timeout, got %v", got)
	}
}

func TestStatsCostIsIndependentOfTraffic(t *testing.T) {
	t.Parallel()
	// The same failure rate must score the same however much traffic the node carries:
	// a node that drops one connection in ten is as good with 10 connections as with
	// 100, and neither is worse than a node that is simply not used.
	for _, connections := range []int{10, 100} {
		stats := healthcheck.Stats{All: 10, Connections: connections, ConnectionFailures: connections / 10}
		got := stats.Cost(100, 0, 0)
		// (9 attempts in 19, 99 in 109 ... see the assertion below for the exact share)
		if got < 100 || got > 300 {
			t.Fatalf("%v connections: want a cost between the delay and the failure cost, got %v", connections, got)
		}
	}
	// ten connections, one failed: (19*100 + 1*300) / 20 = 110
	stats := healthcheck.Stats{All: 10, Connections: 10, ConnectionFailures: 1}
	if got := stats.Cost(100, 0, 0); got != 110 {
		t.Fatalf("want 110, got %v", got)
	}
	// a hundred connections, ten failed: the same tenth, (100*100 + 10*300) / 110 = 118
	stats = healthcheck.Stats{All: 10, Connections: 100, ConnectionFailures: 10}
	if got := stats.Cost(100, 0, 0); got != 118 {
		t.Fatalf("want 118, got %v", got)
	}
	// a node that is not used at all keeps the cost of its checks: no penalty, and no
	// credit either
	stats = healthcheck.Stats{All: 10}
	if got := stats.Cost(100, 0, 0); got != 100 {
		t.Fatalf("want 100, got %v", got)
	}
}

func assertStats(t *testing.T, name string, want, got healthcheck.Stats) {
	// do not comapre times
	want.Expires = time.Time{}
	got.Expires = time.Time{}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("[%s] want: %v, got: %v", name, want, got)
	}
}
