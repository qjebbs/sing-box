package balancer_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/balancer"
)

func deviationRTT(node *balancer.Node) healthcheck.RTT {
	return node.Deviation
}

func newNode(status balancer.Status, deviation healthcheck.RTT) *balancer.Node {
	return &balancer.Node{Status: status, Stats: healthcheck.Stats{Deviation: deviation}}
}

func TestSelectCluster(t *testing.T) {
	t.Parallel()
	// Average values from a real subscription: the first node is far better than the
	// rest, so only it must be selected.
	realData := []*balancer.Node{
		newNode(balancer.StatusQualified, 174),
		newNode(balancer.StatusQualified, 504),
		newNode(balancer.StatusQualified, 505),
		newNode(balancer.StatusQualified, 536),
		newNode(balancer.StatusQualified, 544),
		newNode(balancer.StatusQualified, 561),
		newNode(balancer.StatusQualified, 577),
	}
	// Tiny values, such as the deviation of stable nodes, use the absolute floor.
	tinyValues := []*balancer.Node{
		newNode(balancer.StatusQualified, 3),
		newNode(balancer.StatusQualified, 5),
		newNode(balancer.StatusQualified, 8),
		newNode(balancer.StatusQualified, 12),
	}
	// Average values from another subscription: the first two nodes are close, the
	// third one is clearly worse.
	closePair := []*balancer.Node{
		newNode(balancer.StatusQualified, 161),
		newNode(balancer.StatusQualified, 164),
		newNode(balancer.StatusQualified, 423),
		newNode(balancer.StatusQualified, 442),
	}
	equalNodes := make([]*balancer.Node, 0, 8)
	for i := 0; i < 8; i++ {
		equalNodes = append(equalNodes, newNode(balancer.StatusQualified, 500))
	}
	testCases := []struct {
		name      string
		nodes     []*balancer.Node
		tolerance float32
		minNodes  int
		maxNodes  int
		want      int
	}{
		{name: "empty", nodes: nil, tolerance: balancer.DefaultTolerance, want: 0},
		{name: "single", nodes: realData[:1], tolerance: balancer.DefaultTolerance, want: 1},
		{name: "real data", nodes: realData, tolerance: balancer.DefaultTolerance, want: 1},
		{name: "close pair", nodes: closePair, tolerance: balancer.DefaultTolerance, want: 2},
		{name: "equal nodes", nodes: equalNodes, tolerance: balancer.DefaultTolerance, want: len(equalNodes)},
		{name: "tiny values", nodes: tinyValues, tolerance: balancer.DefaultTolerance, want: len(tinyValues)},
		{name: "wide tolerance", nodes: realData, tolerance: 2, want: 3},
		{name: "min nodes", nodes: realData, tolerance: balancer.DefaultTolerance, minNodes: 3, want: 3},
		{name: "min nodes larger than available", nodes: closePair, tolerance: balancer.DefaultTolerance, minNodes: 9, want: len(closePair)},
		{name: "min nodes within tolerance", nodes: closePair, tolerance: balancer.DefaultTolerance, minNodes: 1, want: 2},
		{name: "max nodes", nodes: equalNodes, tolerance: balancer.DefaultTolerance, maxNodes: 3, want: 3},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			balancer.SortByLeast(testCase.nodes, deviationRTT)
			got := balancer.SelectCluster(
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
	objective := balancer.NewLeastLoadObjective(option.LoadBalancePickOptions{})
	nodes := []*balancer.Node{
		{Index: 0, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 120, All: 10}},
		{Index: 1, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 15, All: 10}},
		{Index: 2, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 18, All: 10}},
		{Index: 3, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 191, All: 10}},
	}
	selected := balancer.Filter(objective, nodes)
	if len(selected) != 2 {
		t.Fatalf("want 2 nodes, got %v", len(selected))
	}
	if selected[0].Index != 1 || selected[1].Index != 2 {
		t.Fatalf("want nodes [1 2], got [%v %v]", selected[0].Index, selected[1].Index)
	}
}

func TestSelectClusterAndStatus(t *testing.T) {
	t.Parallel()
	nodes := []*balancer.Node{
		newNode(balancer.StatusQualified, 50),
		newNode(balancer.StatusQualified, 52),
		newNode(balancer.StatusAlive, 20),
		newNode(balancer.StatusAlive, 22),
		newNode(balancer.StatusAlive, 100),
		newNode(balancer.StatusUnknown, 0),
	}
	// Sorted by status first: qualified nodes are selected, and the selection never
	// crosses into a fallback status, even when its value is lower.
	balancer.SortByLeast(nodes, deviationRTT)
	selected := balancer.SelectCluster(nodes, balancer.DefaultTolerance, 0, 0, deviationRTT)
	if len(selected) != 2 {
		t.Fatalf("want 2 qualified nodes, got %v", len(selected))
	}
	for _, node := range selected {
		if node.Status != balancer.StatusQualified {
			t.Fatalf("selection crossed the status boundary: %v", node.Status)
		}
	}
}

func TestLeastSort(t *testing.T) {
	t.Parallel()
	nodes := []*balancer.Node{
		{Index: 0, Status: balancer.StatusUnknown, Stats: healthcheck.Stats{Deviation: 0, All: 0, Fail: 0}},
		{Index: 1, Status: balancer.StatusDead, Stats: healthcheck.Stats{Deviation: 0, All: 1, Fail: 1}},
		{Index: 2, Status: balancer.StatusDead, Stats: healthcheck.Stats{Deviation: 70, All: 10, Fail: 4}},
		{Index: 3, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 100, All: 10, Fail: 1}},
		{Index: 4, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 100, All: 10, Fail: 0}},
		{Index: 5, Status: balancer.StatusAlive, Stats: healthcheck.Stats{Deviation: 110, All: 10, Fail: 3}},
		{Index: 6, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 120, All: 10, Fail: 0}},
		{Index: 7, Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 150, All: 10, Fail: 0}},
	}
	want := []int{4, 3, 6, 7, 5, 0, 2, 1}
	balancer.SortByLeast(
		nodes,
		func(node *balancer.Node) healthcheck.RTT {
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
	spiky := &balancer.Node{
		Index:  1,
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Average: 600, Median: 160},
	}
	steady := &balancer.Node{
		Index:  2,
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Average: 520, Median: 500},
	}
	nodes := []*balancer.Node{steady, spiky}
	selected := balancer.Filter(balancer.NewLeastPingObjective(option.LoadBalancePickOptions{}), nodes)
	if len(selected) != 1 {
		t.Fatalf("want 1 node, got %v", len(selected))
	}
	if selected[0].Index != 1 {
		t.Fatalf("want node 1 selected, got %v", selected[0].Index)
	}
}

func TestLeastPingScore(t *testing.T) {
	t.Parallel()
	objective := balancer.NewLeastPingObjective(option.LoadBalancePickOptions{})
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		{name: "clear", stats: healthcheck.Stats{Median: 160, Deviation: 10, All: 10}, want: 170},
		// the jitter is added, an unstable node costs more than a steady one
		{name: "jitter", stats: healthcheck.Stats{Median: 160, Deviation: 600, All: 10}, want: 760},
		// failures are already part of the base, the statistics count a failed check
		// with the delay of the timeout, they are not scored again here
		{name: "failures", stats: healthcheck.Stats{Median: 160, Deviation: 10, All: 10, Fail: 2}, want: 170},
		{name: "never checked", stats: healthcheck.Stats{}, want: 0},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &balancer.Node{Status: balancer.StatusQualified, Stats: testCase.stats}
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
	objective := balancer.NewLeastLoadObjective(option.LoadBalancePickOptions{JitterWeight: 1})
	node := &balancer.Node{Status: balancer.StatusQualified, Stats: healthcheck.Stats{
		Median: 160, Deviation: 40, All: 10,
	}}
	if got := objective.Score(node); got != 40 {
		t.Fatalf("want 40, got %v", got)
	}
}

func TestLeastScoreOptionWeights(t *testing.T) {
	t.Parallel()
	node := &balancer.Node{Status: balancer.StatusQualified, Stats: healthcheck.Stats{
		Median: 100, Deviation: 20, All: 10,
	}}
	testCases := []struct {
		name    string
		options option.LoadBalancePickOptions
		want    healthcheck.RTT
	}{
		// 100 + 20
		{name: "defaults", options: option.LoadBalancePickOptions{}, want: 120},
		// 100 + 0.5 * 20
		{name: "custom", options: option.LoadBalancePickOptions{JitterWeight: 0.5}, want: 110},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := balancer.NewLeastPingObjective(testCase.options).Score(node)
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
	flaky := &balancer.Node{
		Index: 1, Status: balancer.StatusQualified,
		Stats: healthcheck.Stats{Median: 161, Deviation: 10, All: 10},
	}
	steady := &balancer.Node{
		Index: 2, Status: balancer.StatusQualified,
		Stats: healthcheck.Stats{Median: 164, Deviation: 8, All: 10},
	}
	// 161 + 10 = 171, 164 + 8 = 172, both are in the cluster, the score orders them
	selected := balancer.Filter(balancer.NewLeastPingObjective(option.LoadBalancePickOptions{}), []*balancer.Node{flaky, steady})
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
	// ordering term besides the base.
	fast := &balancer.Node{
		Index: 1, Status: balancer.StatusQualified,
		Stats: healthcheck.Stats{Median: 161, Deviation: 10, All: 10},
	}
	slow := &balancer.Node{
		Index: 2, Status: balancer.StatusQualified,
		Stats: healthcheck.Stats{Median: 170, Deviation: 10, All: 10},
	}
	selected := balancer.Filter(balancer.NewLeastPingObjective(option.LoadBalancePickOptions{}), []*balancer.Node{slow, fast})
	if len(selected) == 0 || selected[0].Index != 1 {
		t.Fatalf("want node 1 first, got %v", selected)
	}
}

func TestLeastScoreClamp(t *testing.T) {
	t.Parallel()
	objective := balancer.NewLeastPingObjective(option.LoadBalancePickOptions{})
	node := &balancer.Node{Status: balancer.StatusQualified, Stats: healthcheck.Stats{
		Median: 65000, Deviation: 65000, All: 10, Fail: 10,
	}}
	if got := objective.Score(node); got != 65535 {
		t.Fatalf("want the score clamped to 65535, got %v", got)
	}
}

func TestLeastPingKeep(t *testing.T) {
	t.Parallel()
	// Real figures: best is 161+57=218, the band is 20% of it.
	best := &balancer.Node{
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Median: 161, Deviation: 57},
	}
	testCases := []struct {
		name string
		node *balancer.Node
		want bool
	}{
		{
			name: "in the selection",
			node: &balancer.Node{
				Status: balancer.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 193, Deviation: 61},
			},
			want: true,
		},
		{
			// 193+126=319, the band ends at 261 but the deviation is large, so the
			// excess over the band is within its own noise
			name: "out of the band, within the noise",
			node: &balancer.Node{
				Status: balancer.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 178, Deviation: 126},
			},
			want: true,
		},
		{
			// a quiet node is measured accurately, an excess is a real excess
			name: "out of the band, quiet",
			node: &balancer.Node{
				Status: balancer.StatusQualified,
				Stats:  healthcheck.Stats{All: 30, Median: 400, Deviation: 2},
			},
			want: false,
		},
		{
			// 421+978=1399, far beyond anything noise can explain
			name: "far worse",
			node: &balancer.Node{
				Status: balancer.StatusQualified,
				Stats:  healthcheck.Stats{All: 10, Median: 421, Deviation: 978},
			},
			want: false,
		},
		{
			name: "dead",
			node: &balancer.Node{
				Status: balancer.StatusDead,
				Stats:  healthcheck.Stats{All: 10, Median: 100, Deviation: 10},
			},
			want: false,
		},
	}
	objective := balancer.NewLeastPingObjective(option.LoadBalancePickOptions{})
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
	best := &balancer.Node{
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 2, Median: 100, Deviation: 4000},
	}
	// 70% worse than the best, beyond the 40% of twice the tolerance
	node := &balancer.Node{
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 2, Median: 3000, Deviation: 4000},
	}
	if balancer.NewLeastPingObjective(option.LoadBalancePickOptions{}).Keep(node, best) {
		t.Fatalf("a node beyond twice the tolerance was kept")
	}
}

func TestLeastLoadKeep(t *testing.T) {
	t.Parallel()
	// leastload scores by the deviation, which is measured more accurately than the
	// median: 40ms with 10 samples, so the band of 8ms plus the noise of two such
	// estimates keeps a node up to roughly 55ms.
	best := &balancer.Node{
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Deviation: 40},
	}
	objective := balancer.NewLeastLoadObjective(option.LoadBalancePickOptions{})
	close := &balancer.Node{
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Deviation: 52},
	}
	if !objective.Keep(close, best) {
		t.Fatalf("a node within the noise of the deviation was dropped, score %v", objective.Score(close))
	}
	far := &balancer.Node{
		Status: balancer.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Deviation: 300},
	}
	if objective.Keep(far, best) {
		t.Fatalf("a node with 7 times the deviation was kept")
	}
}
