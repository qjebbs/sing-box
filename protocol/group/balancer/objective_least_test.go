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
	selected := objective.Filter(nodes)
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
	selected := balancer.NewLeastPingObjective(option.LoadBalancePickOptions{}).Filter(nodes)
	if len(selected) != 1 {
		t.Fatalf("want 1 node, got %v", len(selected))
	}
	if selected[0].Index != 1 {
		t.Fatalf("want node 1 selected, got %v", selected[0].Index)
	}
}
