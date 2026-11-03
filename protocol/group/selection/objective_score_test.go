package selection_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
)

func jitterRTT(node *selection.Node) healthcheck.RTT {
	return node.Jitter
}

func newNode(status selection.Status, jitter healthcheck.RTT) *selection.Node {
	return &selection.Node{Status: status, Stats: healthcheck.Stats{Jitter: jitter}}
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
	// Tiny values, such as the jitter of stable nodes, use the absolute floor.
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
	// The band is the policy of an objective, not of the cluster: the tests below only
	// check that a prefix of the ranking is taken, so any monotonic limit will do.
	relative := func(tolerance float32) func(best *selection.Node) healthcheck.RTT {
		return func(best *selection.Node) healthcheck.RTT {
			return healthcheck.RTT(float64(jitterRTT(best)) * (1 + float64(tolerance)))
		}
	}
	testCases := []struct {
		name     string
		nodes    []*selection.Node
		limit    func(best *selection.Node) healthcheck.RTT
		minNodes int
		maxNodes int
		want     int
	}{
		{name: "empty", nodes: nil, limit: relative(selection.DefaultTolerance), want: 0},
		{name: "single", nodes: realData[:1], limit: relative(selection.DefaultTolerance), want: 1},
		{name: "real data", nodes: realData, limit: relative(selection.DefaultTolerance), want: 1},
		{name: "close pair", nodes: closePair, limit: relative(selection.DefaultTolerance), want: 2},
		{name: "equal nodes", nodes: equalNodes, limit: relative(selection.DefaultTolerance), want: len(equalNodes)},
		{name: "tiny values", nodes: tinyValues, limit: relative(selection.DefaultTolerance), want: 1},
		{name: "wide tolerance", nodes: realData, limit: relative(2), want: 3},
		{name: "min nodes", nodes: realData, limit: relative(selection.DefaultTolerance), minNodes: 3, want: 3},
		{name: "min nodes larger than available", nodes: closePair, limit: relative(selection.DefaultTolerance), minNodes: 9, want: len(closePair)},
		{name: "min nodes within tolerance", nodes: closePair, limit: relative(selection.DefaultTolerance), minNodes: 1, want: 2},
		{name: "max nodes", nodes: equalNodes, limit: relative(selection.DefaultTolerance), maxNodes: 3, want: 3},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			selection.SortByLeast(testCase.nodes, jitterRTT)
			got := selection.SelectCluster(
				testCase.nodes, testCase.minNodes, testCase.maxNodes, jitterRTT, testCase.limit,
			)
			if len(got) != testCase.want {
				t.Errorf("want %v nodes, got %v", testCase.want, len(got))
			}
		})
	}
}

func TestSelectClusterSortsFirst(t *testing.T) {
	t.Parallel()
	objective := selection.NewSteadyObjective(option.LoadBalancePickOptions{})
	nodes := []*selection.Node{
		{Index: 0, Status: selection.StatusQualified, Stats: healthcheck.Stats{MeanDeviation: 120, All: 10}},
		{Index: 1, Status: selection.StatusQualified, Stats: healthcheck.Stats{MeanDeviation: 15, All: 10}},
		{Index: 2, Status: selection.StatusQualified, Stats: healthcheck.Stats{MeanDeviation: 18, All: 10}},
		{Index: 3, Status: selection.StatusQualified, Stats: healthcheck.Stats{MeanDeviation: 191, All: 10}},
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
	selection.SortByLeast(nodes, jitterRTT)
	selected := selection.SelectCluster(nodes, 0, 0, jitterRTT, func(best *selection.Node) healthcheck.RTT {
		return healthcheck.RTT(float64(jitterRTT(best)) * (1 + float64(selection.DefaultTolerance)))
	})
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
		{Index: 0, Status: selection.StatusUnknown, Stats: healthcheck.Stats{Jitter: 0, All: 0, Fail: 0}},
		{Index: 1, Status: selection.StatusDead, Stats: healthcheck.Stats{Jitter: 0, All: 1, Fail: 1}},
		{Index: 2, Status: selection.StatusDead, Stats: healthcheck.Stats{Jitter: 70, All: 10, Fail: 4}},
		{Index: 3, Status: selection.StatusQualified, Stats: healthcheck.Stats{Jitter: 100, All: 10, Fail: 1}},
		{Index: 4, Status: selection.StatusQualified, Stats: healthcheck.Stats{Jitter: 100, All: 10, Fail: 0}},
		{Index: 5, Status: selection.StatusAlive, Stats: healthcheck.Stats{Jitter: 110, All: 10, Fail: 3}},
		{Index: 6, Status: selection.StatusQualified, Stats: healthcheck.Stats{Jitter: 120, All: 10, Fail: 0}},
		{Index: 7, Status: selection.StatusQualified, Stats: healthcheck.Stats{Jitter: 150, All: 10, Fail: 0}},
	}
	want := []int{4, 3, 6, 7, 5, 0, 2, 1}
	selection.SortByLeast(
		nodes,
		func(node *selection.Node) healthcheck.RTT {
			return node.Jitter
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
	selected := selection.Filter(selection.NewQuickObjective(option.LoadBalancePickOptions{}), nodes)
	if len(selected) != 1 {
		t.Fatalf("want 1 node, got %v", len(selected))
	}
	if selected[0].Index != 1 {
		t.Fatalf("want node 1 selected, got %v", selected[0].Index)
	}
}

func TestLeastPingScore(t *testing.T) {
	t.Parallel()
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		{name: "clear", stats: healthcheck.Stats{Median: 160, Jitter: 10, All: 10}, want: 170},
		// the jitter is added, an unstable node costs more than a steady one
		{name: "jitter", stats: healthcheck.Stats{Median: 160, Jitter: 600, All: 10}, want: 760},
		// (8*170 + 2*510) / 10: a failed attempt is worth three that worked, the value
		// itself describes the checks that succeeded
		{name: "failures", stats: healthcheck.Stats{Median: 160, Jitter: 10, All: 10, Fail: 2}, want: 238},
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

func TestLeastLoadScore(t *testing.T) {
	t.Parallel()
	// steady scores by how much the node strays from its own median, whatever that
	// median is: the distance of the node is not part of the question.
	objective := selection.NewSteadyObjective(option.LoadBalancePickOptions{})
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		// the mean distance to the median, the jitter is not used
		{name: "clear", stats: healthcheck.Stats{Median: 160, Jitter: 90, MeanDeviation: 40, All: 10}, want: 40},
		// 4s of median and 40ms of mean deviation is as steady as 160ms and 40ms
		{name: "far but steady", stats: healthcheck.Stats{Median: 4000, Jitter: 90, MeanDeviation: 40, All: 10}, want: 40},
		// (8*40 + 2*120) / 10, the failures are still mixed in
		{name: "failures", stats: healthcheck.Stats{MeanDeviation: 40, All: 10, Fail: 2}, want: 56},
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

func TestLeastPingScoreDoesNotUseMeanDeviation(t *testing.T) {
	t.Parallel()
	// A node that stalled once has a large mean deviation and a small jitter. quick
	// ranks what the node typically costs, so the stall does not turn it into a slow
	// node; that is what steady is for.
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	node := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 160, Jitter: 10, MeanDeviation: 457, All: 10,
	}}
	if got := objective.Score(node); got != 170 {
		t.Fatalf("want 170, got %v", got)
	}
}

func TestLeastLoadScoreDoesNotUseJitter(t *testing.T) {
	t.Parallel()
	// A node with one stall and a node without any rank the same by steady when their
	// mean deviations match: the jitter is the typical one, and it ignores the stall.
	objective := selection.NewSteadyObjective(option.LoadBalancePickOptions{})
	stalled := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 160, Jitter: 13, MeanDeviation: 457, All: 10,
	}}
	swinging := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 160, Jitter: 300, MeanDeviation: 457, All: 10,
	}}
	if a, b := objective.Score(stalled), objective.Score(swinging); a != b {
		t.Fatalf("want the same score, got %v and %v", a, b)
	}
}

func TestLeastPingScoreRanks(t *testing.T) {
	t.Parallel()
	// Both nodes are fast enough to be qualified, but the first one is the jittery
	// one, the score pushes it behind the second one.
	flaky := &selection.Node{
		Index: 1, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 161, Jitter: 10, All: 10},
	}
	steady := &selection.Node{
		Index: 2, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 164, Jitter: 8, All: 10},
	}
	// 161 + 10 = 171, 164 + 8 = 172, both are in the cluster, the score orders them
	selected := selection.Filter(selection.NewQuickObjective(option.LoadBalancePickOptions{}), []*selection.Node{flaky, steady})
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
		Stats: healthcheck.Stats{Median: 161, Jitter: 10, All: 10},
	}
	slow := &selection.Node{
		Index: 2, Status: selection.StatusQualified,
		Stats: healthcheck.Stats{Median: 170, Jitter: 10, All: 10},
	}
	selected := selection.Filter(selection.NewQuickObjective(option.LoadBalancePickOptions{}), []*selection.Node{slow, fast})
	if len(selected) == 0 || selected[0].Index != 1 {
		t.Fatalf("want node 1 first, got %v", selected)
	}
}

func TestLeastScoreIsBoundedByTheTimeout(t *testing.T) {
	t.Parallel()
	// A node checked with a longer timeout can have a delay above the current one. An
	// attempt still cannot cost more than the timeout it waits for.
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	node := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 65000, Jitter: 65000, All: 10, Latest: 65000,
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
		Stats:  healthcheck.Stats{All: 10, Median: 161, Jitter: 57},
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
				Stats:  healthcheck.Stats{All: 10, Median: 193, Jitter: 61},
			},
			want: true,
		},
		{
			// 193+126=319, the band ends at 261 but the jitter is large, so the
			// excess over the band is within its own noise
			name: "out of the band, within the noise",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 178, Jitter: 126},
			},
			want: true,
		},
		{
			// a quiet node is measured accurately, an excess is a real excess
			name: "out of the band, quiet",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 30, Median: 400, Jitter: 2},
			},
			want: false,
		},
		{
			// 421+978=1399, far beyond anything noise can explain
			name: "far worse",
			node: &selection.Node{
				Status: selection.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 421, Jitter: 978},
			},
			want: false,
		},
		{
			name: "dead",
			node: &selection.Node{
				Status: selection.StatusDead,
				Stats:  healthcheck.Stats{All: 10, Median: 100, Jitter: 10},
			},
			want: false,
		},
	}
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
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
	// Both nodes are extremely noisy (a jitter of 4s from 2 samples), so the
	// noise allowance would be enormous. It is capped at the band, a node cannot
	// survive more than twice the tolerance however unreliable its statistics are.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 2, Median: 100, Jitter: 4000},
	}
	// 70% worse than the best, beyond the 40% of twice the tolerance
	node := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 2, Median: 3000, Jitter: 4000},
	}
	if selection.NewQuickObjective(option.LoadBalancePickOptions{}).Keep(node, best) {
		t.Fatalf("a node beyond twice the tolerance was kept")
	}
}

func TestLeastLoadKeep(t *testing.T) {
	t.Parallel()
	// steady scores by the mean distance to the median: 40ms with 10 samples, so the
	// band of 8ms plus the noise of two such estimates keeps a node up to roughly 55ms.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, MeanDeviation: 40},
	}
	objective := selection.NewSteadyObjective(option.LoadBalancePickOptions{})
	close := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, MeanDeviation: 52},
	}
	if !objective.Keep(close, best) {
		t.Fatalf("a node within the noise of the spread was dropped, score %v", objective.Score(close))
	}
	far := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, MeanDeviation: 300},
	}
	if objective.Keep(far, best) {
		t.Fatalf("a node with 7 times the spread was kept")
	}
}

func TestLeastLoadRanksSteadyFirst(t *testing.T) {
	t.Parallel()
	// A node whose spread is exactly zero has a cost of zero, which must not read as
	// a missing score: steady would rank the steadiest node behind every other one.
	steady := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 10, Median: 100, MeanDeviation: 0, Latest: 100,
	}}
	unstable := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 10, Median: 100, MeanDeviation: 50, Latest: 100,
	}}
	objective := selection.NewSteadyObjective(option.LoadBalancePickOptions{MinNodes: 1, MaxNodes: 1})
	ranked := objective.Rank([]*selection.Node{unstable, steady})
	if ranked[0] != steady {
		t.Fatalf("want the steady node first, got %v to %v", objective.Score(ranked[0]), objective.Score(ranked[1]))
	}
	if selected := selection.Filter(objective, []*selection.Node{unstable, steady}); len(selected) != 1 || selected[0] != steady {
		t.Fatalf("want only the steady node selected, got %d", len(selected))
	}
}
