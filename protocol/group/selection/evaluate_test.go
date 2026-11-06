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
