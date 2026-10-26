package healthcheck

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/provider"
	"github.com/sagernet/sing/common/batch"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

func newIdleTestChecker(idleTimeout, interval time.Duration) *HealthCheck {
	ctx := pause.WithDefaultManager(context.Background())
	return &HealthCheck{
		ctx:             ctx,
		logger:          log.NewNOPFactory().NewLogger("healthcheck"),
		mergedProviders: newMergedProvider(),
		options: &option.HealthCheckOptions{
			Interval:    badoption.Duration(interval),
			IdleTimeout: badoption.Duration(idleTimeout),
		},
		pauseManager: service.FromContext[pause.Manager](ctx),
		Storage:      NewStorages(10, time.Hour),
		lastActive:   make(map[string]time.Time),
		lastCheck:    make(map[string]time.Time),
		checking:     make(map[string]struct{}),
	}
}

func TestActiveNamespaces(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		idle       time.Duration
		lastActive map[string]time.Time
		wantRun    bool
		wantAll    bool
		wantCount  int
	}{
		{
			name:       "disabled",
			idle:       0,
			lastActive: map[string]time.Time{"a": now, "b": now.Add(-24 * time.Hour)},
			wantRun:    true,
			wantAll:    true,
		},
		{
			name:       "never used",
			idle:       0,
			lastActive: map[string]time.Time{},
			wantRun:    false,
			wantCount:  0,
		},
		{
			name:       "all active",
			idle:       time.Hour,
			lastActive: map[string]time.Time{"a": now, "b": now},
			wantRun:    true,
			wantCount:  2,
		},
		{
			name:       "partly idle",
			idle:       time.Hour,
			lastActive: map[string]time.Time{"a": now, "b": now.Add(-2 * time.Hour)},
			wantRun:    true,
			wantCount:  1,
		},
		{
			name:       "all idle",
			idle:       time.Hour,
			lastActive: map[string]time.Time{"a": now.Add(-2 * time.Hour), "b": now.Add(-2 * time.Hour)},
			wantRun:    false,
			wantCount:  0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checker := newIdleTestChecker(test.idle, time.Minute)
			checker.lastActive = test.lastActive
			namespaces, run := checker.activeNamespaces()
			if run != test.wantRun {
				t.Fatalf("run = %v, want %v", run, test.wantRun)
			}
			if test.wantAll {
				if namespaces != nil {
					t.Fatalf("namespaces = %v, want nil for all", namespaces)
				}
				return
			}
			if len(namespaces) != test.wantCount {
				t.Fatalf("namespaces = %v, want %d entries", namespaces, test.wantCount)
			}
		})
	}
}

func markUsed(checker *HealthCheck, namespace string, lastCheck time.Time) {
	checker.idleMu.Lock()
	checker.lastActive[namespace] = time.Now()
	checker.lastCheck[namespace] = lastCheck
	checker.idleMu.Unlock()
}

func TestTouchCatchesUpOnlyWhenStale(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, time.Minute)
	markUsed(checker, "group", time.Now())
	checker.Touch("group")
	if _, inFlight := checker.checking["group"]; inFlight {
		t.Fatal("a recently checked namespace must not catch up")
	}

	markUsed(checker, "group", time.Now().Add(-time.Hour))
	checker.Touch("group")
	checker.idleMu.Lock()
	_, inFlight := checker.checking["group"]
	lastCheck := checker.lastCheck["group"]
	checker.idleMu.Unlock()
	if !inFlight {
		t.Fatal("a stale namespace must catch up")
	}
	if time.Since(lastCheck) > time.Minute {
		t.Fatal("a catch-up check must refresh the last check time")
	}
	waitIdle(t, checker, "group")
}

func TestTouchWithoutIdleTimeoutDoesNotCatchUp(t *testing.T) {
	checker := newIdleTestChecker(0, time.Minute)
	markUsed(checker, "group", time.Now().Add(-2*time.Hour))
	checker.markChecked(nil)
	checker.Touch("group")
	if _, inFlight := checker.checking["group"]; inFlight {
		t.Fatal("a checker without idle timeout must not catch up on use")
	}
}

func TestSetProvidersDefersChecks(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, time.Minute)
	err := checker.SetProviders("group", []adapter.Provider{provider.NewMemory(nil)})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	checker.loopMu.Lock()
	started := checker.loopStarted
	checker.loopMu.Unlock()
	if started {
		t.Fatal("registering providers must not start the periodic loop")
	}
	checker.idleMu.Lock()
	lastCheck := checker.lastCheck["group"]
	_, active := checker.lastActive["group"]
	checker.idleMu.Unlock()
	if !lastCheck.IsZero() {
		t.Fatal("nothing must be checked before the group is used")
	}
	if active {
		t.Fatal("registering providers must not mark the namespace as used")
	}
}

func TestCheckLoopStartsLazily(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, 10*time.Millisecond)
	err := checker.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	checker.loopMu.Lock()
	started := checker.loopStarted
	checker.loopMu.Unlock()
	if started {
		t.Fatal("the check loop must not run before the group is used")
	}
	checker.Touch("group")
	checker.loopMu.Lock()
	started = checker.loopStarted
	checker.loopMu.Unlock()
	if !started {
		t.Fatal("the check loop must start once the group is used")
	}
}

func TestIdleTimeoutSuspendsAndResumes(t *testing.T) {
	checker := newIdleTestChecker(50*time.Millisecond, 10*time.Millisecond)
	err := checker.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	checker.Touch("group")

	waitFor(t, time.Second, func() bool {
		checker.loopMu.Lock()
		defer checker.loopMu.Unlock()
		return checker.suspended
	}, "loop not suspended after being idle")

	checker.Touch("group")
	waitFor(t, time.Second, func() bool {
		checker.loopMu.Lock()
		defer checker.loopMu.Unlock()
		return !checker.suspended
	}, "loop not resumed after being used")
}

func waitIdle(t *testing.T, checker *HealthCheck, namespace string) {
	t.Helper()
	waitFor(t, time.Second, func() bool {
		checker.idleMu.Lock()
		defer checker.idleMu.Unlock()
		_, inFlight := checker.checking[namespace]
		return !inFlight
	}, "catch-up check did not finish")
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(message)
}

func newResultTestBatch(t *testing.T) *batch.Batch[uint16] {
	t.Helper()
	b, _ := batch.New(context.Background(), batch.WithConcurrencyNum[uint16](2))
	return b
}

func TestSuccessResultIsCommittedImmediately(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, time.Minute)
	checker.globalHistory = urltest.NewHistoryStorage()
	defer checker.globalHistory.Close()
	meta := NewMetaData()
	meta.scheduled = true
	b := newResultTestBatch(t)
	b.Go("fast", func() (uint16, error) {
		checker.storeResult(meta.scheduled, "fast", 10)
		meta.ReportSuccess()
		return 10, nil
	})
	// The round is still open (the slow node isn't even submitted yet), but the
	// result of the fast node becomes visible without waiting for the others.
	waitFor(t, time.Second, func() bool {
		return checker.Storage.Stats("fast").All == 1
	}, "success result is not committed immediately")
	if stats := checker.Storage.Stats("fast"); stats.Latest != 10 {
		t.Fatalf("unexpected committed result: %+v", stats)
	}
	b.Go("slow", func() (uint16, error) {
		meta.ReportFailure("slow")
		return 0, nil
	})
	if _, err := checker.waitProcessResult(b, meta); err != nil {
		t.Fatal(err)
	}
	if stats := checker.Storage.Stats("slow"); stats.All != 1 || stats.Latest != Failed {
		t.Fatalf("failed result is not committed after the round: %+v", stats)
	}
	if checker.globalHistory.LoadURLTestHistory("slow") == nil {
		t.Fatal("failed result is not visible in the global history")
	}
}

func TestFailedResultsAreDroppedWhenAllFailed(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, time.Minute)
	checker.globalHistory = urltest.NewHistoryStorage()
	defer checker.globalHistory.Close()
	meta := NewMetaData()
	meta.scheduled = true
	b := newResultTestBatch(t)
	b.Go("dead", func() (uint16, error) {
		meta.ReportFailure("dead")
		return 0, nil
	})
	if _, err := checker.waitProcessResult(b, meta); err != nil {
		t.Fatal(err)
	}
	if stats := checker.Storage.Stats("dead"); stats.All != 0 {
		t.Fatalf("an all-failed round must not be committed: %+v", stats)
	}
	if checker.globalHistory.LoadURLTestHistory("dead") == nil {
		t.Fatal("failed result is not visible in the global history")
	}
}
