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
	// Jitter: the median absolute deviation of the delays that are kept, {60, 140}, is
	// 40, scaled to an estimate of the standard deviation: 1.4826 × 40 ≈ 59.
	// MeanDeviation: every distance is 40, so their mean is 40 as well.
	want := healthcheck.Stats{
		All:           4,
		Fail:          0,
		Jitter:        59,
		MeanDeviation: 40,
		Average:       100,
		Median:        100,
		Max:           140,
		Min:           60,
		Latest:        140,
	}
	assertStats(t, "Stats() - All Success", want, s.Stats())

	// The delay statistics describe the checks that succeeded: a failure says nothing
	// about the delay the node has when it works, it is reported by Fail and Latest.
	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want = healthcheck.Stats{
		All:           4,
		Fail:          2,
		Jitter:        59,
		MeanDeviation: 40,
		Average:       100,
		Median:        100,
		Max:           140,
		Min:           60,
		Latest:        healthcheck.Failed,
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
		All:     10,
		Fail:    8,
		Jitter:  0,
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
	s := healthcheck.NewStorage(4, time.Duration(10)*time.Millisecond)
	for i, rtt := range rtts {
		if i == 2 {
			// wait for previous 2 outdated
			time.Sleep(time.Duration(100) * time.Millisecond)
		}
		s.Put(rtt)
	}
	want := healthcheck.Stats{
		All:           2,
		Fail:          0,
		Jitter:        59,
		MeanDeviation: 40,
		Average:       100,
		Median:        100,
		Max:           140,
		Min:           60,
		Latest:        140,
	}

	assertStats(t, "Stats() - Half Outdated", want, s.Stats())

	// wait for all outdated
	time.Sleep(time.Duration(100) * time.Millisecond)
	want = healthcheck.Stats{
		All:     0,
		Fail:    0,
		Jitter:  0,
		Average: 0,
		Median:  0,
		Max:     0,
		Min:     0,
		Latest:  0,
	}
	assertStats(t, "Stats() - All Outdated", want, s.Stats())

	s.Put(60)
	// one sample says nothing about how much the node varies, so both spreads are
	// assumed to be half of its delay
	want = healthcheck.Stats{
		All:           1,
		Fail:          0,
		Jitter:        30,
		MeanDeviation: 30,
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

func assertStats(t *testing.T, name string, want, got healthcheck.Stats) {
	// do not comapre times
	want.Expires = time.Time{}
	got.Expires = time.Time{}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("[%s] want: %v, got: %v", name, want, got)
	}
}

func TestStatsCostZeroIsNotFailed(t *testing.T) {
	t.Parallel()
	// steady costs a node by its jitter, and a steady node has none. Zero is
	// reserved for a missing value, so the cost must stay measurable.
	stats := healthcheck.Stats{All: 10, Median: 100, Jitter: 0, Latest: 100}
	if cost := stats.Cost(0, 0); cost != healthcheck.Millisecond {
		t.Fatalf("want the smallest delay, got %v", cost)
	}
	// a node that never worked has no measurable cost, the zero is its absence
	if cost := (healthcheck.Stats{}).Cost(0, 0); cost != healthcheck.Failed {
		t.Fatalf("want no cost, got %v", cost)
	}
}
