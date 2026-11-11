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
		pauseManager:        service.FromContext[pause.Manager](ctx),
		Storage:             NewStorages(StorageOptions{Sampling: 10, Validity: time.Hour}),
		lastActive:          make(map[string]time.Time),
		lastCheck:           make(map[string]time.Time),
		idleProbeNamespaces: make(map[string]struct{}),
		checking:            make(map[string]struct{}),
		loopWake:            make(chan struct{}, 1),
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
			// without an idle timeout the checks never pause, so there is nothing to wait for
			name:       "no idle timeout, never used",
			idle:       0,
			lastActive: map[string]time.Time{},
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

func TestIdleNamespacesAreCheckedOncePerProbeWindow(t *testing.T) {
	checker := newIdleTestChecker(time.Minute, time.Second)
	now := idleProbeWindowStart(time.Now()).Add(idleProbePeriod / 4)
	checker.SetIdleProbe("idle", true)
	checker.lastActive["idle"] = now.Add(-time.Hour)
	checker.lastCheck["idle"] = now.Add(-idleProbePeriod)

	namespaces := checker.idleProbeCandidates(now)
	if len(namespaces) != 1 || namespaces[0] != "idle" {
		t.Fatalf("namespaces = %v, want [idle]", namespaces)
	}
	if namespaces = checker.idleProbeCandidates(now); len(namespaces) != 0 {
		t.Fatalf("namespaces = %v, want no second check in the same window", namespaces)
	}
	// A check inside the window, which a probe or a round of a group in use can make, is
	// enough for it: the next probe waits for the next window.
	if namespaces = checker.idleProbeCandidates(now.Add(idleProbePeriod / 2)); len(namespaces) != 0 {
		t.Fatalf("namespaces = %v, want no second check in the same window", namespaces)
	}

	if namespaces = checker.idleProbeCandidates(now.Add(idleProbePeriod)); len(namespaces) != 1 || namespaces[0] != "idle" {
		t.Fatalf("namespaces = %v, want [idle] in the next window", namespaces)
	}
}

func TestIdleNamespacesAreProbedWithoutBeingUsed(t *testing.T) {
	// The idle objective ranks on what the probes measure, and a group is most in need of it
	// while it is not in use: a namespace that was never used is a probe candidate.
	checker := newIdleTestChecker(time.Minute, time.Minute)
	checker.SetIdleProbe("group", true)

	if namespaces := checker.idleProbeCandidates(time.Now()); len(namespaces) != 1 || namespaces[0] != "group" {
		t.Fatalf("namespaces = %v, want [group]", namespaces)
	}
	if namespaces := checker.idleProbeCandidates(time.Now()); len(namespaces) != 0 {
		t.Fatalf("namespaces = %v, want no second probe in the same window", namespaces)
	}
}

func TestIdleNamespacesSkipActiveAndAlreadyChecked(t *testing.T) {
	checker := newIdleTestChecker(time.Minute, time.Second)
	now := idleProbeWindowStart(time.Now()).Add(idleProbePeriod / 4)
	checker.SetIdleProbe("active", true)
	checker.SetIdleProbe("checked", true)
	checker.SetIdleProbe("quick", false)
	checker.lastActive["active"] = now
	checker.lastActive["checked"] = now.Add(-time.Hour)
	checker.lastActive["quick"] = now.Add(-time.Hour)
	checker.lastCheck["checked"] = now

	if namespaces := checker.idleProbeCandidates(now); len(namespaces) != 0 {
		t.Fatalf("namespaces = %v, want no checks for active or already checked groups", namespaces)
	}
}

func TestNextIdleProbeTimeIsRandomizedWithinAFutureWindow(t *testing.T) {
	now := idleProbeWindowStart(time.Now()).Add(idleProbePeriod / 4)
	probeAt, windowStart := nextIdleProbeTime(now, time.Time{})
	if !probeAt.After(now) {
		t.Fatalf("probeAt = %v, want a future time", probeAt)
	}
	if !idleProbeWindowStart(probeAt).Equal(windowStart) {
		t.Fatalf("probeAt = %v, want it in the window starting at %v", probeAt, windowStart)
	}
	if !windowStart.Equal(idleProbeWindowStart(now)) {
		t.Fatalf("windowStart = %v, want the current window %v", windowStart, idleProbeWindowStart(now))
	}
	if probeAt.Before(windowStart) || !probeAt.Before(windowStart.Add(idleProbePeriod)) {
		t.Fatalf("probeAt = %v, want it inside the probe window", probeAt)
	}
	if !probeAt.After(now) {
		t.Fatalf("probeAt = %v, want a future point after %v", probeAt, now)
	}

	nextAt, nextStart := nextIdleProbeTime(now, windowStart.Add(idleProbePeriod))
	if !nextStart.Equal(windowStart.Add(idleProbePeriod)) || !idleProbeWindowStart(nextAt).Equal(nextStart) {
		t.Fatalf("next probe = %v in window %v, want the window after %v", nextAt, nextStart, windowStart)
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

func TestTouchDoesNotCatchUpAFreshNamespace(t *testing.T) {
	checker := newIdleTestChecker(0, time.Minute)
	markUsed(checker, "group", time.Now().Add(-2*time.Hour))
	checker.markChecked(nil)
	checker.Touch("group")
	if _, inFlight := checker.checking["group"]; inFlight {
		t.Fatal("a namespace checked within the interval must not catch up on use")
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
	running := checker.loopCtx != nil
	checker.loopMu.Unlock()
	if running {
		t.Fatal("registering providers must not start the checker")
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

func TestSchedulerRunsWithoutTheGroupBeingUsed(t *testing.T) {
	checker := newIdleTestChecker(time.Hour, 10*time.Millisecond)
	err := checker.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	checker.loopMu.Lock()
	running := checker.loopCtx != nil
	checker.loopMu.Unlock()
	if !running {
		t.Fatal("the scheduler must run from the start")
	}
	time.Sleep(50 * time.Millisecond)
	checker.idleMu.Lock()
	lastCheck := checker.lastCheck["group"]
	_, used := checker.lastActive["group"]
	checker.idleMu.Unlock()
	if !lastCheck.IsZero() {
		t.Fatal("a group that was never used must not have a regular round")
	}
	if used {
		t.Fatal("the scheduler must not mark a namespace as used")
	}
}

func TestSchedulerChecksWithoutTheGroupBeingUsedWithoutIdleTimeout(t *testing.T) {
	// idle_timeout = 0 means the checks never pause: there is nothing to wait for, so a round
	// runs without a group being used.
	checker := newIdleTestChecker(0, 10*time.Millisecond)
	checked := make(chan struct{}, 1)
	checker.RegisterPostCheckListener(func() {
		select {
		case checked <- struct{}{}:
		default:
		}
	})
	err := checker.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	select {
	case <-checked:
	case <-time.After(3 * time.Second):
		t.Fatal("a checker without an idle timeout must run rounds without the group being used")
	}
}

func TestSchedulerWaitsWhenIdleAndResumesOnTouch(t *testing.T) {
	checker := newIdleTestChecker(50*time.Millisecond, 10*time.Millisecond)
	err := checker.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	checker.Touch("group")

	waitFor(t, time.Second, func() bool {
		checker.idleMu.Lock()
		defer checker.idleMu.Unlock()
		return !checker.lastCheck["group"].IsZero() &&
			time.Since(checker.lastActive["group"]) > time.Duration(checker.options.IdleTimeout)+time.Duration(checker.options.Interval)*2
	}, "scheduler did not reach the idle state")
	checker.idleMu.Lock()
	lastCheck := checker.lastCheck["group"]
	checker.idleMu.Unlock()
	time.Sleep(30 * time.Millisecond)
	checker.idleMu.Lock()
	if got := checker.lastCheck["group"]; !got.Equal(lastCheck) {
		checker.idleMu.Unlock()
		t.Fatalf("last check advanced while idle: got %v, want %v", got, lastCheck)
	}
	checker.idleMu.Unlock()

	checker.Touch("group")
	waitFor(t, time.Second, func() bool {
		checker.idleMu.Lock()
		defer checker.idleMu.Unlock()
		return checker.lastCheck["group"].After(lastCheck)
	}, "touch did not trigger a catch-up check after idle")
	checker.loopMu.Lock()
	running := checker.loopCtx != nil
	checker.loopMu.Unlock()
	if !running {
		t.Fatal("scheduler stopped after the namespace became idle")
	}
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

func TestIsActive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		idle       time.Duration
		lastActive time.Time
		want       bool
	}{
		// never used, and not used since longer than the idle timeout
		{name: "never used", idle: 30 * time.Minute, want: false},
		{name: "idle", idle: 30 * time.Minute, lastActive: now.Add(-time.Hour), want: false},
		{name: "active", idle: 30 * time.Minute, lastActive: now, want: true},
		// without an idle timeout the loop checks every namespace each round, used or not
		{name: "no idle timeout, never used", idle: 0, want: true},
		{name: "no idle timeout, used", idle: 0, lastActive: now.Add(-24 * time.Hour), want: true},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			checker := newIdleTestChecker(testCase.idle, time.Minute)
			if !testCase.lastActive.IsZero() {
				checker.lastActive["a"] = testCase.lastActive
			}
			if got := checker.IsActive("a"); got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestIsActiveWithoutChecker(t *testing.T) {
	var checker *HealthCheck
	if checker.IsActive("a") {
		t.Fatal("a missing checker has no active namespace")
	}
}
