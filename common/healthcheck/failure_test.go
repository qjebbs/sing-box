package healthcheck_test

import (
	"math"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/healthcheck"
)

// historyOf builds checks `gap` apart from the outcomes, which are oldest first, the way
// the storage holds them: newest first.
func historyOf(newest time.Time, gap time.Duration, outcomes ...bool) []*healthcheck.History {
	checks := make([]*healthcheck.History, 0, len(outcomes))
	for i := range outcomes {
		delay := healthcheck.RTT(100)
		if outcomes[len(outcomes)-1-i] {
			delay = healthcheck.Failed
		}
		checks = append(checks, &healthcheck.History{
			Time:  newest.Add(-time.Duration(i) * gap),
			Delay: delay,
		})
	}
	return checks
}

// settledOf returns a long clean history followed by the failures given, so the prior the
// estimate starts with has faded and the estimate describes the failures alone.
func settledOf(newest time.Time, gap time.Duration, outcomes ...bool) []*healthcheck.History {
	// Six hours of clean checks, so the prior is long gone whatever the spacing.
	all := append(outcomesOf(int((6*time.Hour)/gap), 0), outcomes...)
	return historyOf(newest, gap, all...)
}

// outcomesOf returns n checks with the last `failed` of them failing.
func outcomesOf(n int, failed int) []bool {
	outcomes := make([]bool, n)
	for i := n - failed; i < n; i++ {
		outcomes[i] = true
	}
	return outcomes
}

func TestReplayFailureRateStartsFromAPrior(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	// A cold start is a node nobody has seen fail: one failing check must not outweigh the
	// ten that were never seen, or a network blip at start-up would be reported as a node
	// that fails every time for an hour.
	if rate := healthcheck.ReplayFailureRate(historyOf(now, time.Minute, true)); rate > 0.1 {
		t.Fatalf("want the first failing check to stay under a tenth, got %v", rate)
	}
	// The prior is evidence like any other and fades with it: a node that has really been
	// failing reaches the same estimate as a long-standing one.
	always := make([]bool, 500)
	for i := range always {
		always[i] = true
	}
	if rate := healthcheck.ReplayFailureRate(historyOf(now, time.Minute, always...)); rate < 0.95 {
		t.Fatalf("want a node that never passed to read as failing, got %v", rate)
	}
	if rate := healthcheck.ReplayFailureRate(historyOf(now, time.Minute, false)); rate != 0 {
		t.Fatalf("want zero for a check that passed, got %v", rate)
	}
	if rate := healthcheck.ReplayFailureRate(nil); rate != 0 {
		t.Fatalf("want no estimate without checks, got %v", rate)
	}
}

func TestReplayFailureRateLooksBackInTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	// A node that has been failing for ten minutes has been failing for a fifth of the
	// memory, however many checks that minute held: the estimate is the stretch of time.
	want := 1 - math.Exp(-10.0/45.0)
	rates := make([]float64, 0, 3)
	for _, each := range []struct {
		gap   time.Duration
		fails int
	}{
		{time.Minute, 10},
		{30 * time.Second, 20},
		{15 * time.Second, 40},
	} {
		rate := healthcheck.ReplayFailureRate(settledOf(now, each.gap, outcomesOf(each.fails, each.fails)...))
		if math.Abs(rate-want) > 3e-2 {
			t.Fatalf("want about %v for ten minutes of failures in %d checks, got %v", want, each.fails, rate)
		}
		rates = append(rates, rate)
	}
	// The count of checks must not matter at all: the same ten minutes at three densities
	// is the same evidence.
	for _, rate := range rates[1:] {
		if math.Abs(rate-rates[0]) > 1e-2 {
			t.Fatalf("want the density to make no difference, got %v and %v", rates[0], rate)
		}
	}
	// Failing for one time constant out of a settled history is sixty-three percent of it,
	// which is the whole point of an exponential memory: there is no fixed window to fill.
	if rate := healthcheck.ReplayFailureRate(settledOf(now, time.Minute, outcomesOf(45, 45)...)); math.Abs(rate-(1-math.Exp(-1))) > 1e-2 {
		t.Fatalf("want %v for one time constant of failures, got %v", 1-math.Exp(-1), rate)
	}
}

func TestReplayFailureRateForgetsAnOutage(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	// Ten minutes of failures, then forty-five silent minutes: the outage is half gone,
	// and it keeps going down without anything having to be timed or swept.
	outage := append(outcomesOf(200, 0), outcomesOf(10, 10)...)
	outage = append(outage, outcomesOf(45, 0)...)
	rate := healthcheck.ReplayFailureRate(historyOf(now, time.Minute, outage...))
	if want := (1 - math.Exp(-10.0/45.0)) * math.Exp(-45.0/45.0); math.Abs(rate-want) > 1e-2 {
		t.Fatalf("want about %v after the outage, got %v", want, rate)
	}
	// And it is the passing checks that carry it down, so the same outage a day earlier is
	// nothing at all by now.
	outage = append(outcomesOf(200, 0), outcomesOf(10, 10)...)
	outage = append(outage, outcomesOf(48*60, 0)...)
	if rate := healthcheck.ReplayFailureRate(historyOf(now, time.Minute, outage...)); rate > 1e-9 {
		t.Fatalf("want nothing left a day later, got %v", rate)
	}
}

func TestFailureShareLetsTheWindowWin(t *testing.T) {
	t.Parallel()
	// The window describes what the node is doing now, the estimate what it does when the
	// window happens to be clean: the larger of the two is the cautious reading.
	cases := []struct {
		name  string
		stats healthcheck.Stats
		want  float64
	}{
		{"window has failures", healthcheck.Stats{All: 10, Fail: 3, FailureRate: 0.05}, 0.3},
		{"window is clean", healthcheck.Stats{All: 10, Fail: 0, FailureRate: 0.2}, 0.2},
		{"both agree", healthcheck.Stats{All: 10, Fail: 2, FailureRate: 0.2}, 0.2},
		{"no window", healthcheck.Stats{FailureRate: 0.4}, 0.4},
		{"nothing known", healthcheck.Stats{}, 0},
		{"capped at one", healthcheck.Stats{All: 10, Fail: 10, FailureRate: 0.9}, 1},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := testCase.stats.FailureShare(); math.Abs(got-testCase.want) > 1e-6 {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestStorageCountsTheChecksTheConnectionsAsked(t *testing.T) {
	t.Parallel()
	// A check the connections asked for is the second half of what a failed connection
	// said, so it is evidence about the node like a scheduled one, and the estimate has to
	// hear about it: Update is the only path an out of band result takes.
	storage := healthcheck.NewStorage(healthcheck.StorageOptions{Sampling: 10, Validity: time.Hour})
	for i := 0; i < 10; i++ {
		storage.Put(100)
	}
	if rate := storage.Stats().FailureRate; rate != 0 {
		t.Fatalf("want a clean estimate from clean checks, got %v", rate)
	}
	storage.Update(healthcheck.Failed)
	if rate := storage.Stats().FailureRate; rate == 0 {
		t.Fatal("want the failed out of band check to be remembered")
	}
}
