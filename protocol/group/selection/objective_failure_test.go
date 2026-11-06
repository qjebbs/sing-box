package selection_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
)

// cleanWindow is a node whose window holds no failure: with max_fail at its default, that is
// every node a group can select, whatever the window says about the rest of its record.
func cleanWindow() healthcheck.Stats {
	return healthcheck.Stats{All: 10, Median: 150, Floor: 140, Jitter: 5, Bad: 0}
}

func scored(t *testing.T, objective selection.Objective, stats healthcheck.Stats) healthcheck.RTT {
	t.Helper()
	scorer, isScorer := objective.(selection.Scorer)
	if !isScorer {
		t.Fatalf("%s does not rank a score", objective.Name())
	}
	return scorer.Score(&selection.Node{Status: selection.StatusQualified, Stats: stats})
}

func TestLongRunFailureRateRanksACleanUnreliableNodeLower(t *testing.T) {
	t.Parallel()
	// Two nodes nobody has seen fail in the window, so the window cannot tell them apart.
	// One of them fails in bursts, which is what the long-run estimate remembers: the tie
	// is broken towards the node the group can keep using.
	steady := cleanWindow()
	bursty := cleanWindow()
	bursty.FailureRate = 0.25
	for _, objective := range []selection.Objective{
		selection.NewQuickObjective(option.LoadBalancePickOptions{}),
		selection.NewIdleObjective(option.LoadBalancePickOptions{}),
	} {
		steadyScore := scored(t, objective, steady)
		burstyScore := scored(t, objective, bursty)
		if burstyScore <= steadyScore {
			t.Fatalf("%s: want the unreliable node ranked behind, got %v and %v",
				objective.Name(), burstyScore, steadyScore)
		}
	}
}

func TestLongRunFailureRateDoesNotOverturnAClearDifference(t *testing.T) {
	t.Parallel()
	// The estimate is a prior about a node, not a second ranking: a node that is clearly
	// faster still wins, so the group does not start serving the slow one to be safe.
	fastBursty := cleanWindow()
	fastBursty.Median, fastBursty.Floor = 100, 95
	fastBursty.FailureRate = 0.1
	slowSteady := cleanWindow()
	slowSteady.Median, slowSteady.Floor = 400, 390
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	if scored(t, objective, fastBursty) >= scored(t, objective, slowSteady) {
		t.Fatal("want the clearly faster node to stay first")
	}
}

func TestLongRunFailureRateYieldsToTheWindow(t *testing.T) {
	t.Parallel()
	// The window is what the node is doing now. A node whose window holds failures is
	// priced by them even when its record is long clean, and one whose window is clean is
	// priced by its record: the larger share is the cautious reading of the two.
	recovering := cleanWindow()
	recovering.Fail = 2
	if share := recovering.FailureShare(); share != 0.2 {
		t.Fatalf("want the window share, got %v", share)
	}
	recovering.FailureRate = 0.5
	if share := recovering.FailureShare(); share != 0.5 {
		t.Fatalf("want the larger of the two, got %v", share)
	}
}
