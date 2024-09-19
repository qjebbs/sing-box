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
		Max:       140,
		Min:       60,
		Latest:    140,
	}
	assertStats(t, "Stats() - All Success", want, s.Stats())

	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want.Fail = 2
	want.Latest = healthcheck.Failed
	assertStats(t, "Stats() - Half Fail", want, s.Stats())

	s.Put(healthcheck.Failed)
	s.Put(healthcheck.Failed)
	want = healthcheck.Stats{
		All:       4,
		Fail:      4,
		Deviation: healthcheck.Failed,
		Average:   healthcheck.Failed,
		Max:       healthcheck.Failed,
		Min:       healthcheck.Failed,
		Latest:    healthcheck.Failed,
	}
	assertStats(t, "Stats() - All Fail", want, s.Stats())
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
