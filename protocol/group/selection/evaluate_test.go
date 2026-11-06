package selection_test

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
)

// evaluatedSeries builds nodes that are told apart by a fixed offset each, one check a
// minute, alternating around the node's own delay.
func evaluatedSeries(nodes int, checks int, offset healthcheck.RTT, noise healthcheck.RTT) []selection.WindowSeries {
	newest := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	series := make([]selection.WindowSeries, 0, nodes)
	for i := 0; i < nodes; i++ {
		delay := 200 + healthcheck.RTT(i)*offset
		history := make([]*healthcheck.History, 0, checks)
		for j := 0; j < checks; j++ {
			at := healthcheck.RTT(0)
			if j%2 == 0 {
				at = noise
			}
			history = append(history, &healthcheck.History{
				Time:  newest.Add(-time.Duration(j) * time.Minute),
				Delay: delay + at - noise/2,
			})
		}
		series = append(series, selection.WindowSeries{
			Node: &selection.Node{
				RTTSacale: 1,
				Status:    selection.StatusQualified,
				Stats:     healthcheck.Stats{All: checks, Floor: 100},
			},
			Checks: history,
		})
	}
	return series
}

func TestEvaluateWindowsFollowsTheRanking(t *testing.T) {
	t.Parallel()
	// Every node keeps its distance from the others for the whole history, so any window
	// that measures something has to order them the way the checks that follow it do.
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		evaluatedSeries(8, 24, 40, 10),
		selection.WindowOptions{Ahead: 4, Sizes: []selection.WindowSize{{Stride: 1, Samples: 5}}},
	)
	if evaluation.Interval != time.Minute {
		t.Fatalf("want an interval of a minute, got %v", evaluation.Interval)
	}
	if evaluation.Checks != 24 {
		t.Fatalf("want the whole history measured, got %d checks", evaluation.Checks)
	}
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want one measured window, got %d", len(evaluation.Sizes))
	}
	size := evaluation.Sizes[0]
	if size.Span != 5*time.Minute {
		t.Fatalf("want a span of five minutes, got %v", size.Span)
	}
	if size.Windows < 15 {
		t.Fatalf("want every position measured, got %d", size.Windows)
	}
	if size.Rho < 0.99 {
		t.Fatalf("the window did not recover the order of the nodes: rho %v", size.Rho)
	}
	if size.Pool > 8 {
		t.Fatalf("the pool is larger than the nodes: %v", size.Pool)
	}
}

func TestEvaluateWindowsMeasuresWhatFits(t *testing.T) {
	t.Parallel()
	// A history that holds a window and the checks after it twice is enough to measure:
	// the answer is noisier than one averaged over many positions, which is what Windows
	// and Delta report, but it is an answer.
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		evaluatedSeries(8, 8, 40, 10),
		selection.WindowOptions{Ahead: 2, Sizes: []selection.WindowSize{{Stride: 1, Samples: 5}}},
	)
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want the window measured, got %d", len(evaluation.Sizes))
	}
	size := evaluation.Sizes[0]
	if size.Windows != 2 {
		t.Fatalf("want the two positions the history holds, got %d", size.Windows)
	}
	if size.Delta != 0 {
		t.Fatalf("want no precision claim from two positions, got %v", size.Delta)
	}
	if size.Rho < 0.99 {
		t.Fatalf("the window did not recover the order of the nodes: rho %v", size.Rho)
	}
}

// seriesOfChecks builds one series out of the delays of each check, newest first.
func seriesOfChecks(nodes ...[]healthcheck.RTT) []selection.WindowSeries {
	newest := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	series := make([]selection.WindowSeries, 0, len(nodes))
	for _, delays := range nodes {
		history := make([]*healthcheck.History, 0, len(delays))
		for j, delay := range delays {
			history = append(history, &healthcheck.History{
				Time:  newest.Add(-time.Duration(j) * time.Minute),
				Delay: delay,
			})
		}
		series = append(series, selection.WindowSeries{
			Node: &selection.Node{
				RTTSacale: 1,
				Status:    selection.StatusQualified,
				Stats:     healthcheck.Stats{All: len(delays), Floor: 100},
			},
			Checks: history,
		})
	}
	return series
}

func TestEvaluateWindowsAgreesWithThePool(t *testing.T) {
	t.Parallel()
	// Every node keeps its distance from the others for the whole history, so the pool the
	// window selects and the pool the checks after it select are the same set.
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		evaluatedSeries(6, 12, 100, 0),
		selection.WindowOptions{Ahead: 3, Sizes: []selection.WindowSize{{Stride: 1, Samples: 5}}},
	)
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want one measured window, got %d", len(evaluation.Sizes))
	}
	size := evaluation.Sizes[0]
	if size.Pool != 1 {
		t.Fatalf("want the band to let in one node, got %v", size.Pool)
	}
	if size.Agreement != 1 {
		t.Fatalf("want the two pools to be the same set, got %v", size.Agreement)
	}
	if size.Exact != 1 {
		t.Fatalf("want every position to select the same pool, got %v", size.Exact)
	}
}

func TestEvaluateWindowsMeasuresThePoolApartFromTheOrder(t *testing.T) {
	t.Parallel()
	// Two nodes trade places between the window and the checks after it, each staying a
	// hundred milliseconds clear of the other: the window selects one of them and the
	// following checks select the other, so the pools share nothing even though the
	// history is long enough to measure and the ranking is not a tie.
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		seriesOfChecks(
			[]healthcheck.RTT{500, 500, 100, 100, 100},
			[]healthcheck.RTT{100, 100, 500, 500, 500},
			[]healthcheck.RTT{900, 900, 900, 900, 900},
			[]healthcheck.RTT{1200, 1200, 1200, 1200, 1200},
		),
		selection.WindowOptions{Ahead: 2, Sizes: []selection.WindowSize{{Stride: 1, Samples: 3}}},
	)
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want one measured window, got %d", len(evaluation.Sizes))
	}
	size := evaluation.Sizes[0]
	if size.Pool != 1 {
		t.Fatalf("want the band to let in one node, got %v", size.Pool)
	}
	if size.Agreement != 0 {
		t.Fatalf("want the two pools to share nothing, got %v", size.Agreement)
	}
	if size.Exact != 0 {
		t.Fatalf("want no position to select the same pool, got %v", size.Exact)
	}
}

func TestEvaluateWindowsMeasuresTheReaction(t *testing.T) {
	t.Parallel()
	// The first node was the fastest and turns three times slower in the middle of the
	// history, then recovers. The band needs two checks of the window before the median and
	// the jitter have moved enough to take it out of the pool: that delay is the price of a
	// window steady enough to agree with itself.
	slowed := make([]healthcheck.RTT, 16)
	for i := range slowed {
		slowed[i] = 150
	}
	for i := 5; i < 8; i++ {
		slowed[i] = 600
	}
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		seriesOfChecks(
			slowed,
			steadyChecks(16, 160),
			steadyChecks(16, 170),
			steadyChecks(16, 180),
		),
		selection.WindowOptions{Ahead: 4, Sizes: []selection.WindowSize{{Stride: 1, Samples: 4}}},
	)
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want one measured window, got %d", len(evaluation.Sizes))
	}
	size := evaluation.Sizes[0]
	if size.Reactions != 1 {
		t.Fatalf("want one node that turned bad, got %d", size.Reactions)
	}
	if size.Reaction != 2 {
		t.Fatalf("want the pool to drop it two checks after, got %v", size.Reaction)
	}
	// The other side of the same window: the node is back to its own speed at the fifth
	// check of the history, and the median needs two more checks before the band takes it
	// back.
	if size.Recoveries != 1 {
		t.Fatalf("want one node that turned good again, got %d", size.Recoveries)
	}
	if size.Recovery != 3 {
		t.Fatalf("want the pool to take it back three checks after, got %v", size.Recovery)
	}
}

func TestEvaluateWindowsDropsAFailureAtOnceAndTakesItBackSlowly(t *testing.T) {
	t.Parallel()
	// A failure is not a slow check: the pool already requires a window with no failure in
	// it, so the node leaves on the check that fails and there is nothing to react to.
	// Coming back is the expensive direction: the failure has to leave the window first, so
	// a single failed check costs as many checks as the window holds.
	failed := make([]healthcheck.RTT, 16)
	for i := range failed {
		failed[i] = 150
	}
	for i := 5; i < 8; i++ {
		failed[i] = healthcheck.Failed
	}
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		seriesOfChecks(
			failed,
			steadyChecks(16, 160),
			steadyChecks(16, 170),
			steadyChecks(16, 180),
		),
		selection.WindowOptions{Ahead: 4, Sizes: []selection.WindowSize{{Stride: 1, Samples: 4}}},
	)
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want one measured window, got %d", len(evaluation.Sizes))
	}
	size := evaluation.Sizes[0]
	if size.Reactions != 0 || size.Reaction != 0 {
		t.Fatalf("want no reaction to measure, got %v over %d reactions", size.Reaction, size.Reactions)
	}
	if size.Recoveries != 1 {
		t.Fatalf("want one node that turned good again, got %d", size.Recoveries)
	}
	if size.Recovery != 4 {
		t.Fatalf("want the failure to hold it out for the whole window, got %v", size.Recovery)
	}
}

func steadyChecks(count int, delay healthcheck.RTT) []healthcheck.RTT {
	delays := make([]healthcheck.RTT, count)
	for i := range delays {
		delays[i] = delay
	}
	return delays
}

func TestEvaluateWindowsHasNothingToRank(t *testing.T) {
	t.Parallel()
	// Nodes that cannot be told apart: every window sees the same score, so the rank
	// correlation is undefined and is reported as no agreement rather than as perfect
	// agreement.
	series := evaluatedSeries(8, 24, 0, 0)
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(
		series,
		selection.WindowOptions{Ahead: 4, Sizes: []selection.WindowSize{{Stride: 1, Samples: 5}}},
	)
	if len(evaluation.Sizes) != 1 {
		t.Fatalf("want one measured window, got %d", len(evaluation.Sizes))
	}
	if rho := evaluation.Sizes[0].Rho; rho != 0 {
		t.Fatalf("want no agreement between tied scores, got %v", rho)
	}
}

func TestEvaluateWindowsNeedsHistory(t *testing.T) {
	t.Parallel()
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	evaluation := objective.EvaluateWindows(evaluatedSeries(8, 4, 40, 10), selection.WindowOptions{Ahead: 4})
	if len(evaluation.Sizes) != 0 {
		t.Fatalf("want nothing measured from a history of four checks, got %d", len(evaluation.Sizes))
	}
	// A report that measures nothing still says what it had, so it reads as a window that
	// is not there yet rather than as a group that has no history.
	if evaluation.Checks != 4 || evaluation.Nodes != 8 || evaluation.Ahead != 4 {
		t.Fatalf("want the history it found, got %d nodes, %d checks, ahead %d",
			evaluation.Nodes, evaluation.Checks, evaluation.Ahead)
	}
	if evaluation.Interval != time.Minute {
		t.Fatalf("want the interval of the history, got %v", evaluation.Interval)
	}
	// An empty report has to say what it was short of: 4 checks and 4 ahead cannot hold
	// even the smallest default window, which needs 5 checks and the 4 that follow.
	if evaluation.Note == "" {
		t.Fatal("want the report to say what it was missing")
	}
	t.Logf("note: %s", evaluation.Note)
	// The default grid is the one the documentation reasons about: three densities over
	// the window sizes a group is configured with.
	evaluation = objective.EvaluateWindows(evaluatedSeries(8, 30, 40, 10), selection.WindowOptions{})
	if len(evaluation.Sizes) == 0 {
		t.Fatal("want the default grid measured")
	}
	if evaluation.Ahead != 8 {
		t.Fatalf("want the default of eight checks ahead, got %d", evaluation.Ahead)
	}
	for _, size := range evaluation.Sizes {
		if size.Stride < 1 || size.Stride > 3 {
			t.Fatalf("want a stride of one to three checks, got %d", size.Stride)
		}
		if size.Samples*size.Stride > 22 {
			t.Fatalf("the window %d×%d does not fit in the history", size.Stride, size.Samples)
		}
	}
}

func TestNodeBuilderFollowsTheWindow(t *testing.T) {
	t.Parallel()
	// max_fail is a number of checks, so it is judged as a share of the window it is read
	// against: a limit of two in ten must not stand for two in thirty, where one failure is
	// already past it. The window comes from the caller, so nothing has to be told about a
	// change to it.
	builder, err := selection.NewNodeBuilder(selection.NodeOptions{MaxFail: 2})
	if err != nil {
		t.Fatal(err)
	}
	stats := healthcheck.Stats{All: 10, Fail: 1, Median: 100, Floor: 100}
	node := func(sampling int) selection.Status {
		return builder.NewNode(tagOutbound("node"), 0, sampling, stats).Status
	}
	if status := node(10); status != selection.StatusQualified {
		t.Fatalf("one failure in ten is within two, got %v", status)
	}
	if status := node(3); status != selection.StatusQualified {
		t.Fatalf("one failure is well within two in three, got %v", status)
	}
	if status := node(30); status == selection.StatusQualified {
		t.Fatalf("one failure is past two in thirty, got %v", status)
	}
	if status := node(10); status != selection.StatusQualified {
		t.Fatalf("the window came back and so should the limit, got %v", status)
	}
}
