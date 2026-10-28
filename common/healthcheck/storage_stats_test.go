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
	if got := s.Stats().Mixed(100, healthcheck.Second); got != healthcheck.Failed {
		t.Fatalf("want no mixed value without samples, got %v", got)
	}
}

func TestStatsMixed(t *testing.T) {
	t.Parallel()
	// An attempt either succeeds with the value, or fails after waiting for the
	// failure delay, so the two are mixed by the share of the checks that failed.
	const failureDelay = 1000
	testCases := []struct {
		name  string
		all   int
		fail  int
		value healthcheck.RTT
		want  healthcheck.RTT
	}{
		{name: "no failure", all: 10, fail: 0, value: 100, want: 100},
		// (9*100 + 1*1000) / 10
		{name: "one failure", all: 10, fail: 1, value: 100, want: 190},
		// (5*100 + 5*1000) / 10
		{name: "half", all: 10, fail: 5, value: 100, want: 550},
		// every attempt waits for the timeout
		{name: "all failed", all: 10, fail: 10, value: 0, want: failureDelay},
		// a value measured with a longer timeout is still capped by the attempt
		{name: "value above the timeout", all: 10, fail: 0, value: 3000, want: failureDelay},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			stats := healthcheck.Stats{All: testCase.all, Fail: testCase.fail}
			if got := stats.Mixed(testCase.value, failureDelay); got != testCase.want {
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
