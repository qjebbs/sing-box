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
	s.FailureDelay = healthcheck.Second
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

	// A failed check counts with the delay it waited for, so the half failed node is
	// described by 60, 140, 1000 and 1000, not by 60 and 140 only.
	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want = healthcheck.Stats{
		All:       4,
		Fail:      2,
		Deviation: 450,
		Average:   550,
		Median:    570,
		Max:       1000,
		Min:       60,
		Latest:    healthcheck.Failed,
	}
	assertStats(t, "Stats() - Half Fail", want, s.Stats())

	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	// Every sample is a timeout, so every sample has the same delay: no deviation,
	// but the delay itself is the one to report.
	want = healthcheck.Stats{
		All:       4,
		Fail:      4,
		Deviation: 0,
		Average:   1000,
		Median:    1000,
		Max:       1000,
		Min:       1000,
		Latest:    healthcheck.Failed,
	}
	assertStats(t, "Stats() - All Fail", want, s.Stats())
}

func TestStorageStatsFirstCheckFailed(t *testing.T) {
	t.Parallel()
	// A node that failed its only check reports the timeout it waited for, instead
	// of the zero statistics that read like the best possible result.
	s := healthcheck.NewStorage(10, time.Hour)
	s.FailureDelay = healthcheck.Second
	s.Put(healthcheck.Failed)
	want := healthcheck.Stats{
		All:  1,
		Fail: 1,
		// a single sample, deviation is assumed to be half of the average
		Deviation: 500,
		Average:   1000,
		Median:    1000,
		Max:       1000,
		Min:       1000,
		Latest:    healthcheck.Failed,
	}
	assertStats(t, "Stats() - First Check Failed", want, s.Stats())
}

func TestStorageStatsFailuresAreNotCensored(t *testing.T) {
	t.Parallel()
	// A node failing most of its checks used to be described by the few successful
	// ones, which made it look like the most stable node of all.
	s := healthcheck.NewStorage(10, time.Hour)
	s.FailureDelay = healthcheck.Second
	for i := 0; i < 8; i++ {
		s.Put(healthcheck.Failed)
	}
	s.Put(100)
	s.Put(100)
	want := healthcheck.Stats{
		All:       10,
		Fail:      8,
		Deviation: 360,
		Average:   820,
		Median:    1000,
		Max:       1000,
		Min:       100,
		Latest:    100,
	}
	assertStats(t, "Stats() - Mostly Fail", want, s.Stats())
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
}

func assertStats(t *testing.T, name string, want, got healthcheck.Stats) {
	// do not comapre times
	want.Expires = time.Time{}
	got.Expires = time.Time{}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("[%s] want: %v, got: %v", name, want, got)
	}
}
