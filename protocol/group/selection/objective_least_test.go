package selection_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
)

func deviationRTT(node *selection.Node) healthcheck.RTT {
	return node.Deviation
}

func newNode(status selection.Status, deviation healthcheck.RTT) *selection.Node {
	return &selection.Node{Status: status, Stats: healthcheck.Stats{Deviation: deviation}}
}

func TestSelectCluster(t *testing.T) {
	t.Parallel()
	// Average values from a real subscription: the first node is far better than the
	// rest, so only it must be selected.
	realData := []*selection.Node{
		newNode(selection.StatusQualified, 174),
		newNode(selection.StatusQualified, 504),
		newNode(selection.StatusQualified, 505),
		newNode(selection.StatusQualified, 536),
		newNode(selection.StatusQualified, 544),
		newNode(selection.StatusQualified, 561),
		newNode(selection.StatusQualified, 577),
	}
	// Tiny values, such as the deviation of stable nodes, use the absolute floor.
	tinyValues := []*selection.Node{
		newNode(selection.StatusQualified, 3),
		newNode(selection.StatusQualified, 5),
		newNode(selection.StatusQualified, 8),
		newNode(selection.StatusQualified, 12),
	}
	// Average values from another subscription: the first two nodes are close, the
	// third one is clearly worse.
	closePair := []*selection.Node{
		newNode(selection.StatusQualified, 161),
		newNode(selection.StatusQualified, 164),
		newNode(selection.StatusQualified, 423),
		newNode(selection.StatusQualified, 442),
	}
	equalNodes := make([]*selection.Node, 0, 8)
	for i := 0; i < 8; i++ {
		equalNodes = append(equalNodes, newNode(selection.StatusQualified, 500))
	}
	testCases := []struct {
		name      string
		nodes     []*selection.Node
		tolerance float32
		minNodes  int
		maxNodes  int
		want      int
	}{
		{name: "empty", nodes: nil, tolerance: selection.DefaultTolerance, want: 0},
		{name: "single", nodes: realData[:1], tolerance: selection.DefaultTolerance, want: 1},
		{name: "real data", nodes: realData, tolerance: selection.DefaultTolerance, want: 1},
		{name: "close pair", nodes: closePair, tolerance: selection.DefaultTolerance, want: 2},
		{name: "equal nodes", nodes: equalNodes, tolerance: selection.DefaultTolerance, want: len(equalNodes)},
		{name: "tiny values", nodes: tinyValues, tolerance: selection.DefaultTolerance, want: len(tinyValues)},
		{name: "wide tolerance", nodes: realData, tolerance: 2, want: 3},
		{name: "min nodes", nodes: realData, tolerance: selection.DefaultTolerance, minNodes: 3, want: 3},
		{name: "min nodes larger than available", nodes: closePair, tolerance: selection.DefaultTolerance, minNodes: 9, want: len(closePair)},
		{name: "min nodes within tolerance", nodes: closePair, tolerance: selection.DefaultTolerance, minNodes: 1, want: 2},
		{name: "max nodes", nodes: equalNodes, tolerance: selection.DefaultTolerance, maxNodes: 3, want: 3},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			selection.SortByLeast(testCase.nodes, deviationRTT)
			got := selection.SelectCluster(
				testCase.nodes, testCase.tolerance, testCase.minNodes, testCase.maxNodes, deviationRTT,
			)
			if len(got) != testCase.want {
				t.Errorf("want %v nodes, got %v", testCase.want, len(got))
			}
		})
	}
}

func TestSelectClusterSortsFirst(t *testing.T) {
	t.Parallel()
	objective := selection.NewLeastLoadObjective(option.LoadBalancePickOptions{})
	nodes := []*selection.Node{
		{Index: 0, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 120, All: 10}},
		{Index: 1, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 15, All: 10}},
		{Index: 2, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 18, All: 10}},
		{Index: 3, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 191, All: 10}},
	}
	selected := selection.Filter(objective, nodes)
	if len(selected) != 2 {
		t.Fatalf("want 2 nodes, got %v", len(selected))
	}
	if selected[0].Index != 1 || selected[1].Index != 2 {
		t.Fatalf("want nodes [1 2], got [%v %v]", selected[0].Index, selected[1].Index)
	}
}

func TestSelectClusterAndStatus(t *testing.T) {
	t.Parallel()
	nodes := []*selection.Node{
		newNode(selection.StatusQualified, 50),
		newNode(selection.StatusQualified, 52),
		newNode(selection.StatusAlive, 20),
		newNode(selection.StatusAlive, 22),
		newNode(selection.StatusAlive, 100),
		newNode(selection.StatusUnknown, 0),
	}
	// Sorted by status first: qualified nodes are selected, and the selection never
	// crosses into a fallback status, even when its value is lower.
	selection.SortByLeast(nodes, deviationRTT)
	selected := selection.SelectCluster(nodes, selection.DefaultTolerance, 0, 0, deviationRTT)
	if len(selected) != 2 {
		t.Fatalf("want 2 qualified nodes, got %v", len(selected))
	}
	for _, node := range selected {
		if node.Status != selection.StatusQualified {
			t.Fatalf("selection crossed the status boundary: %v", node.Status)
		}
	}
}

func TestLeastSort(t *testing.T) {
	t.Parallel()
	nodes := []*selection.Node{
		{Index: 0, Status: selection.StatusUnknown, Stats: healthcheck.Stats{Deviation: 0, All: 0, Fail: 0}},
		{Index: 1, Status: selection.StatusDead, Stats: healthcheck.Stats{Deviation: 0, All: 1, Fail: 1}},
		{Index: 2, Status: selection.StatusDead, Stats: healthcheck.Stats{Deviation: 70, All: 10, Fail: 4}},
		{Index: 3, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 100, All: 10, Fail: 1}},
		{Index: 4, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 100, All: 10, Fail: 0}},
		{Index: 5, Status: selection.StatusAlive, Stats: healthcheck.Stats{Deviation: 110, All: 10, Fail: 3}},
		{Index: 6, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 120, All: 10, Fail: 0}},
		{Index: 7, Status: selection.StatusQualified, Stats: healthcheck.Stats{Deviation: 150, All: 10, Fail: 0}},
	}
	want := []int{4, 3, 6, 7, 5, 0, 2, 1}
	selection.SortByLeast(
		nodes,
		func(node *selection.Node) healthcheck.RTT {
			return node.Deviation
		},
	)
	for i, node := range nodes {
		if node.Index != want[i] {
			t.Errorf("SortByLeast() failed")
			break
		}
	}
	if t.Failed() {
		for _, node := range nodes {
			t.Log(node.String())
		}
		t.Logf("want: %v", want)
	}
}

func TestLeastPingUsesMedian(t *testing.T) {
	t.Parallel()
	// Node 1 is spiky: one bad round dragged its average up while its typical RTT
	// stayed low. Median ranks it first, the average would rank it last.
	spiky := &selection.Node{
		Index:  1,
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Average: 600, Median: 160},
	}
	steady := &selection.Node{
		Index:  2,
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Average: 520, Median: 500},
	}
	nodes := []*selection.Node{steady, spiky}
	selected := selection.Filter(selection.NewLeastPingObjective(option.LoadBalancePickOptions{}), nodes)
	if len(selected) != 1 {
		t.Fatalf("want 1 node, got %v", len(selected))
	}
	if selected[0].Index != 1 {
		t.Fatalf("want node 1 selected, got %v", selected[0].Index)
	}
}

func TestLeastPingScore(t *testing.T) {
	t.Parallel()
	objective := selection.NewLeastPingObjective(option.LoadBalancePickOptions{JitterWeight: 1})
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		{name: "clear", stats: healthcheck.Stats{Median: 160, Deviation: 10, All: 10}, want: 170},
		// the jitter is added, an unstable node costs more than a steady one
		{name: "jitter", stats: healthcheck.Stats{Median: 160, Deviation: 600, All: 10}, want: 760},
		// (8*170 + 2*510) / 10: a failed attempt is worth three that worked, the value
		// itself describes the checks that succeeded
		{name: "failures", stats: healthcheck.Stats{Median: 160, Deviation: 10, All: 10, Fail: 2}, want: 238},
		// no attempt ever succeeded, the timeout is all the node costs
		{name: "never succeeded", stats: healthcheck.Stats{All: 10, Fail: 10}, want: 15000},
		{name: "never checked", stats: healthcheck.Stats{}, want: 0},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &selection.Node{Status: selection.StatusQualified, Stats: testCase.stats}
			got := objective.Score(node)
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestLeastPingScoreWithoutJitter(t *testing.T) {
	t.Parallel()
	// adding the deviation is an opt-in, so an unset weight scores the delay alone
	objective := selection.NewLeastPingObjective(option.LoadBalancePickOptions{})
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		{name: "clear", stats: healthcheck.Stats{Median: 160, Deviation: 10, All: 10}, want: 160},
		// an unstable node is not ranked worse, the caller asked for the delay
		{name: "jitter", stats: healthcheck.Stats{Median: 160, Deviation: 600, All: 10}, want: 160},
		// the failures are still mixed in, in the scale of the node
		{name: "failures", stats: healthcheck.Stats{Median: 160, Deviation: 10, All: 10, Fail: 2}, want: 224},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &selection.Node{Status: selection.StatusQualified, Stats: testCase.stats}
			got := objective.Score(node)
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestLeastLoadScoreIgnoresJitter(t *testing.T) {
	t.Parallel()
	// leastload scores by the standard deviation itself, adding it again would only
	// scale every score, so the jitter weight is not applied.
	objective := selection.NewLeastLoadObjective(option.LoadBalancePickOptions{JitterWeight: 1})
	node := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 160, Deviation: 40, All: 10,
	}}
	if got := objective.Score(node); got != 40 {
		t.Fatalf("want 40, got %v", got)
	}
}

func TestLeastScoreOptionWeights(t *testing.T) {
	t.Parallel()
	node := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 100, Deviation: 20, All: 10,
	}}
	testCases := []struct {
		name    string
		options option.LoadBalancePickOptions
		want    healthcheck.RTT
	}{
		// the delay alone, the jitter is an opt-in
		{name: "defaults", options: option.LoadBalancePickOptions{}, want: 100},
		// 100 + 1 * 20
		{name: "opt-in", options: option.LoadBalancePickOptions{JitterWeight: 1}, want: 120},
		// 100 + 0.5 * 20
		{name: "custom", options: option.LoadBalancePickOptions{JitterWeight: 0.5}, want: 110},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := selection.NewLeastPingObjective(testCase.options).Score(node)
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestLeastPingScoreRanks(t *testing.T) {
	t.Parallel()
	// Both nodes are fast enough to be qualified, but the first one is the jittery
	// one, the score pushes it behind the second one.
	flaky := &selection.Node{
		Index: 1, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 161, Deviation: 10, All: 10},
	}
	steady := &selection.Node{
		Index: 2, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 164, Deviation: 8, All: 10},
	}
	// 161 + 10 = 171, 164 + 8 = 172, both are in the cluster, the score orders them
	selected := selection.Filter(selection.NewLeastPingObjective(option.LoadBalancePickOptions{}), []*selection.Node{flaky, steady})
	if len(selected) != 2 {
		t.Fatalf("want both nodes, got %v", selected)
	}
	if selected[0].Index != 1 {
		t.Fatalf("want node 1 first, got %v", selected[0].Index)
	}
}

func TestLeastPingOrdersFasterFirst(t *testing.T) {
	t.Parallel()
	// With the same jitter the faster node wins, the jitter weight is the only
	// ordering term besides the selection.
	fast := &selection.Node{
		Index: 1, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 161, Deviation: 10, All: 10},
	}
	slow := &selection.Node{
		Index: 2, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 170, Deviation: 10, All: 10},
	}
	selected := selection.Filter(selection.NewLeastPingObjective(option.LoadBalancePickOptions{}), []*selection.Node{slow, fast})
	if len(selected) == 0 || selected[0].Index != 1 {
		t.Fatalf("want node 1 first, got %v", selected)
	}
}

func TestLeastScoreIsBoundedByTheTimeout(t *testing.T) {
	t.Parallel()
	// A node checked with a longer timeout can have a delay above the current one. An
	// attempt still cannot cost more than the timeout it waits for.
	objective := selection.NewLeastPingObjective(option.LoadBalancePickOptions{})
	node := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 65000, Deviation: 65000, All: 10, Latest: 65000,
	}}
	if got := objective.Score(node); got != 15000 {
		t.Fatalf("want the score bounded by the 15s timeout, got %v", got)
	}
}

func TestLeastPingKeep(t *testing.T) {
	t.Parallel()
	// Real figures: best is 161+57=218, the band is 20% of it.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Median: 161, Deviation: 57},
	}
	testCases := []struct {
		name string
		node *selection.Node
		want bool
	}{
		{
			name: "in the selection",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 193, Deviation: 61},
			},
			want: true,
		},
		{
			// 193+126=319, the band ends at 261 but the deviation is large, so the
			// excess over the band is within its own noise
			name: "out of the band, within the noise",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 178, Deviation: 126},
			},
			want: true,
		},
		{
			// a quiet node is measured accurately, an excess is a real excess
			name: "out of the band, quiet",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 30, Median: 400, Deviation: 2},
			},
			want: false,
		},
		{
			// 421+978=1399, far beyond anything noise can explain
			name: "far worse",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 421, Deviation: 978},
			},
			want: false,
		},
		{
			name: "dead",
			node: &selection.Node{
				Status: selection.StatusDead,
				Stats:  healthcheck.Stats{All: 10, Median: 100, Deviation: 10},
			},
			want: false,
		},
	}
	objective := selection.NewLeastPingObjective(option.LoadBalancePickOptions{})
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := objective.Keep(testCase.node, best); got != testCase.want {
				t.Fatalf("want %v, got %v (score %v)", testCase.want, got, objective.Score(testCase.node))
			}
		})
	}
}

func TestLeastKeepIsCappedByTheBand(t *testing.T) {
	t.Parallel()
	// Both nodes are extremely noisy (a deviation of 4s from 2 samples), so the
	// noise allowance would be enormous. It is capped at the band, a node cannot
	// survive more than twice the tolerance however unreliable its statistics are.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 2, Median: 100, Deviation: 4000},
	}
	// 70% worse than the best, beyond the 40% of twice the tolerance
	node := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 2, Median: 3000, Deviation: 4000},
	}
	if selection.NewLeastPingObjective(option.LoadBalancePickOptions{}).Keep(node, best) {
		t.Fatalf("a node beyond twice the tolerance was kept")
	}
}

func TestLeastLoadKeep(t *testing.T) {
	t.Parallel()
	// leastload scores by the deviation, which is measured more accurately than the
	// median: 40ms with 10 samples, so the band of 8ms plus the noise of two such
	// estimates keeps a node up to roughly 55ms.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Deviation: 40},
	}
	objective := selection.NewLeastLoadObjective(option.LoadBalancePickOptions{})
	close := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Deviation: 52},
	}
	if !objective.Keep(close, best) {
		t.Fatalf("a node within the noise of the deviation was dropped, score %v", objective.Score(close))
	}
	far := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Deviation: 300},
	}
	if objective.Keep(far, best) {
		t.Fatalf("a node with 7 times the deviation was kept")
	}
}

func TestLeastLoadRanksSteadyFirst(t *testing.T) {
	t.Parallel()
	// A node whose deviation is exactly zero has a cost of zero, which must not read as
	// a missing score: leastload would rank the steadiest node behind every other one.
	steady := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 10, Median: 100, Deviation: 0, Latest: 100,
	}}
	unstable := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 10, Median: 100, Deviation: 50, Latest: 100,
	}}
	objective := selection.NewLeastLoadObjective(option.LoadBalancePickOptions{MinNodes: 1, MaxNodes: 1})
	ranked := objective.Rank([]*selection.Node{unstable, steady})
	if ranked[0] != steady {
		t.Fatalf("want the steady node first, got %v to %v", objective.Score(ranked[0]), objective.Score(ranked[1]))
	}
	if selected := selection.Filter(objective, []*selection.Node{unstable, steady}); len(selected) != 1 || selected[0] != steady {
		t.Fatalf("want only the steady node selected, got %d", len(selected))
	}
}
