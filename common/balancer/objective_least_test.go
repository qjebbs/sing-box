package balancer_test

import (
	"strconv"
	"testing"

	"github.com/sagernet/sing-box/common/balancer"
	"github.com/sagernet/sing-box/common/healthcheck"
)

func TestLeastNodes(t *testing.T) {
	t.Parallel()
	nodes := []*balancer.Node{
		{Stats: healthcheck.Stats{Deviation: 50}},
		{Stats: healthcheck.Stats{Deviation: 70}},
		{Stats: healthcheck.Stats{Deviation: 100}},
		{Stats: healthcheck.Stats{Deviation: 110}},
		{Stats: healthcheck.Stats{Deviation: 120}},
		{Stats: healthcheck.Stats{Deviation: 150}},
	}
	testCases := []struct {
		expected  int
		baselines []healthcheck.RTT
		want      int
	}{
		// typical cases
		{want: 1},
		{baselines: []healthcheck.RTT{100}, want: 2},
		{expected: 3, want: 3},
		{expected: 3, baselines: []healthcheck.RTT{50, 100, 150}, want: 5},

		// edge cases
		{expected: 0, baselines: nil, want: 1},
		{expected: 1, baselines: nil, want: 1},
		{expected: 0, baselines: []healthcheck.RTT{10}, want: 1},
		{expected: 0, baselines: []healthcheck.RTT{80, 100}, want: 2},
		{expected: 2, baselines: []healthcheck.RTT{50, 100}, want: 2},
		{expected: 0, baselines: []healthcheck.RTT{10}, want: 1},
		{expected: 9999, want: len(nodes)},
		{expected: 9999, baselines: []healthcheck.RTT{50, 100, 150}, want: len(nodes)},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			if got := balancer.LeastNodes(
				nodes, tc.expected, tc.baselines,
				func(node *balancer.Node) healthcheck.RTT {
					return node.Deviation
				},
			); len(got) != tc.want {
				t.Errorf("want: %v, got: %v", tc.want, len(got))
			}
		})
	}
}

func TestLeastNodesAndStatus(t *testing.T) {
	t.Parallel()
	nodes := []*balancer.Node{
		{Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 50}},
		{Status: balancer.StatusQualified, Stats: healthcheck.Stats{Deviation: 80}},
		{Status: balancer.StatusAlive, Stats: healthcheck.Stats{Deviation: 20}},
		{Status: balancer.StatusAlive, Stats: healthcheck.Stats{Deviation: 50}},
		{Status: balancer.StatusAlive, Stats: healthcheck.Stats{Deviation: 70}},
		{Status: balancer.StatusAlive, Stats: healthcheck.Stats{Deviation: 100}},
		{Status: balancer.StatusAlive, Stats: healthcheck.Stats{Deviation: 110}},
		{Status: balancer.StatusUnknown, Stats: healthcheck.Stats{Deviation: 0}},
	}
	testCases := []struct {
		expected  int
		baselines []healthcheck.RTT
		want      int
	}{
		{expected: 1, want: 1},
		{expected: 3, want: 3},
		{expected: 1, baselines: []healthcheck.RTT{100}, want: 2},
		{expected: 3, baselines: []healthcheck.RTT{100}, want: 5},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			if got := balancer.LeastNodes(
				nodes, tc.expected, tc.baselines,
				func(node *balancer.Node) healthcheck.RTT {
					return node.Deviation
				},
			); len(got) != tc.want {
				t.Errorf("want %v, got = %v", tc.want, len(got))
			}
		})
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
			t.Logf(node.String())
		}
		t.Logf("want: %v", want)
	}
}
