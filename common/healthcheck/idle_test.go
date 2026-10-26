package healthcheck

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
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

func TestTouchCatchesUpOnlyWhenStale(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, time.Minute)
	checker.markActive("group")
	checker.Touch("group")
	if _, inFlight := checker.checking["group"]; inFlight {
		t.Fatal("a recently checked namespace must not catch up")
	}

	checker.idleMu.Lock()
	checker.lastCheck["group"] = time.Now().Add(-time.Hour)
	checker.idleMu.Unlock()
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
	checker.markActive("group")
	checker.idleMu.Lock()
	checker.lastCheck["group"] = time.Now().Add(-2 * time.Hour)
	checker.idleMu.Unlock()
	checker.markChecked(nil)
	checker.Touch("group")
	if _, inFlight := checker.checking["group"]; inFlight {
		t.Fatal("a checker without idle timeout must not catch up on use")
	}
}

func TestIdleTimeoutSuspendsAndResumes(t *testing.T) {
	checker := newIdleTestChecker(50*time.Millisecond, 10*time.Millisecond)
	err := checker.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	checker.markActive("group")
	checker.tryStartLoops()

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
