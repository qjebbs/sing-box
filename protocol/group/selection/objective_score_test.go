package selection_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
)

// testTolerance is the share the cluster tests compare with, the band is the policy of an
// objective and any monotonic limit will do here.
const testTolerance = 0.2

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
		{name: "empty", nodes: nil, limit: relative(testTolerance), want: 0},
		{name: "single", nodes: realData[:1], limit: relative(testTolerance), want: 1},
		{name: "real data", nodes: realData, limit: relative(testTolerance), want: 1},
		{name: "close pair", nodes: closePair, limit: relative(testTolerance), want: 2},
		{name: "equal nodes", nodes: equalNodes, limit: relative(testTolerance), want: len(equalNodes)},
		{name: "tiny values", nodes: tinyValues, limit: relative(testTolerance), want: 1},
		{name: "wide tolerance", nodes: realData, limit: relative(2), want: 3},
		{name: "min nodes", nodes: realData, limit: relative(testTolerance), minNodes: 3, want: 3},
		{name: "min nodes larger than available", nodes: closePair, limit: relative(testTolerance), minNodes: 9, want: len(closePair)},
		{name: "min nodes within tolerance", nodes: closePair, limit: relative(testTolerance), minNodes: 1, want: 2},
		{name: "max nodes", nodes: equalNodes, limit: relative(testTolerance), maxNodes: 3, want: 3},
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
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	nodes := []*selection.Node{
		{Index: 0, Status: selection.StatusQualified, Stats: healthcheck.Stats{Floor: 100, Median: 220, All: 10}},
		{Index: 1, Status: selection.StatusQualified, Stats: healthcheck.Stats{Floor: 100, Median: 115, All: 10}},
		{Index: 2, Status: selection.StatusQualified, Stats: healthcheck.Stats{Floor: 100, Median: 118, All: 10}},
		{Index: 3, Status: selection.StatusQualified, Stats: healthcheck.Stats{Floor: 100, Median: 291, All: 10}},
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
		return healthcheck.RTT(float64(jitterRTT(best)) * (1 + float64(testTolerance)))
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
	// idle scores by how far the node is above its own floor, as a share of that floor,
	// and how often it falls twice below it: the distance of the node is not part of the
	// question, so two nodes at the same share score the same.
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		// 60% over a floor of 100ms, no bad check: 0.6 of the 1000ms scale
		{name: "clear", stats: healthcheck.Stats{Floor: 100, Median: 160, Jitter: 90, MeanDeviation: 40, All: 10}, want: 600},
		// the same share over a floor of 2500ms: 4000ms of median costs as much as 160ms,
		// because both are 60% over what the node can do
		{name: "far but the same share", stats: healthcheck.Stats{Floor: 2500, Median: 4000, Jitter: 90, All: 10}, want: 600},
		// 60% over, and four of the ten checks fell to twice the floor: 600 + 0.5 × 400
		{name: "bad checks", stats: healthcheck.Stats{Floor: 100, Median: 160, Bad: 4, All: 10}, want: 800},
		// two failures of ten attempts: each counts as one bad check, so the frequency term
		// gains 0.5 × 2/10 = 0.1 of the scale, and the depth is untouched
		{name: "failures", stats: healthcheck.Stats{Floor: 100, Median: 160, All: 10, Fail: 2}, want: 700},
		// the failures cannot enter the depth: a node that answers slowly but always is
		// scored by its median, however many attempts failed
		{name: "failures do not deepen", stats: healthcheck.Stats{Floor: 100, Median: 160, All: 10, Fail: 5}, want: 850},
		{name: "at its floor", stats: healthcheck.Stats{Floor: 160, Median: 160, All: 10}, want: healthcheck.Millisecond},
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
	// node; that is what idle is for.
	objective := selection.NewQuickObjective(option.LoadBalancePickOptions{})
	node := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Median: 160, Jitter: 10, MeanDeviation: 457, All: 10,
	}}
	if got := objective.Score(node); got != 170 {
		t.Fatalf("want 170, got %v", got)
	}
}

func TestLeastLoadScoreOneSample(t *testing.T) {
	t.Parallel()
	// A window with one successful check has no floor of its own and no variation to
	// measure, so both shares of idle are zero and every node would tie at the smallest
	// score there is. It ranks by the delay until a second check arrives: a node whose one
	// check came out low must not look like the steadiest node there is.
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	fast := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 1, Median: 100,
	}}
	slow := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 1, Median: 900,
	}}
	if objective.Score(fast) >= objective.Score(slow) {
		t.Fatalf("one sample did not rank by the delay: %v for 100ms, %v for 900ms",
			objective.Score(fast), objective.Score(slow))
	}
	// quick reads the same window in the same order, since its jitter is half the delay
	// when there is one check: the two objectives must not disagree before there is enough
	// to tell queueing from distance.
	if quick := selection.NewQuickObjective(option.LoadBalancePickOptions{}); quick.Score(fast) >= quick.Score(slow) {
		t.Fatalf("quick did not rank the same window by the delay either")
	}
	// A second check ends the fallback: the score is the share over the floor again, which
	// for a node at its floor is the smallest score there is whatever the distance.
	atFloor := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 2, Floor: 900, Median: 900,
	}}
	if got := objective.Score(atFloor); got != healthcheck.Millisecond {
		t.Fatalf("two samples still read the distance: got %v for a node at its floor", got)
	}
}

func TestLeastLoadScoreDoesNotUseJitter(t *testing.T) {
	t.Parallel()
	// A node with one stall and a node without any rank the same by idle when their floor,
	// median and count of bad checks match: the jitter is a description of the spread, and
	// the score is built from the shares instead.
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	stalled := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Floor: 100, Median: 160, Jitter: 13, MeanDeviation: 457, All: 10,
	}}
	swinging := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		Floor: 100, Median: 160, Jitter: 300, MeanDeviation: 457, All: 10,
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
	// Both nodes are noisy (a jitter of 400ms from 10 samples), so the noise allowance
	// exceeds the band of 50ms plus its margin, 408ms. The allowance is capped at the
	// band: a node cannot survive more than twice it, however unreliable the statistics.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Median: 100, Jitter: 400},
	}
	// 1900ms worse than the best, beyond the 816ms of twice the band
	node := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Median: 2000, Jitter: 400},
	}
	if selection.NewQuickObjective(option.LoadBalancePickOptions{}).Keep(node, best) {
		t.Fatalf("a node beyond twice the tolerance was kept")
	}
}

// The frequency enters the score, but not the band: an uncertain share must not buy a node
// a place next to the best one. Sizing the band by it made the band depend on whether the
// best node happened to catch one bad check in the window, which on a ten-check window is
// most rounds of a node that is not even falling behind.
func TestLeastLoadBandIgnoresBadShare(t *testing.T) {
	t.Parallel()
	clean := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Floor: 100, Median: 130, Jitter: 8},
	}
	stalled := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Floor: 100, Median: 130, Jitter: 8, Bad: 4},
	}
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	band := func(node *selection.Node) healthcheck.RTT {
		return objective.Limit(node) - objective.Score(node)
	}
	if band(clean) != band(stalled) {
		t.Fatalf("the bad checks widened the band: %v for a clean best, %v for one that stalled",
			band(clean), band(stalled))
	}
	if objective.Score(stalled) <= objective.Score(clean) {
		t.Fatalf("the bad checks did not count in the score: %v against %v",
			objective.Score(stalled), objective.Score(clean))
	}
}

func TestLeastLoadKeep(t *testing.T) {
	t.Parallel()
	// idle scores by the share of the queueing: a floor of 100ms and a median of 130ms is
	// 30% over, 300 of the 1000ms scale. The band is the 50ms tolerance plus the margin of
	// an 8ms typical deviation, about 140ms, and the noise of two such estimates is capped
	// at it, so a node up to about 528 is kept.
	best := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Floor: 100, Median: 130, Jitter: 8},
	}
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{})
	close := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Floor: 100, Median: 140, Jitter: 10},
	}
	if !objective.Keep(close, best) {
		t.Fatalf("a node within the noise of the typical deviation was dropped, score %v", objective.Score(close))
	}
	far := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 10, Floor: 100, Median: 300, Jitter: 10},
	}
	if objective.Keep(far, best) {
		t.Fatalf("a node at three times the floor was kept")
	}
	// A node whose jitter was moved by one slow check and whose floor and median did not
	// move: the band is sized by the jitter, so a node 20% further over is not equally
	// good. Sized by the spread of 122ms instead, the band would be far wider and it would
	// be.
	spiky := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 5, Floor: 500, Median: 510, Jitter: 3, Spread: 122},
	}
	behind := &selection.Node{
		Status: selection.StatusQualified,
		Stats:  healthcheck.Stats{All: 5, Floor: 500, Median: 600, Jitter: 10, Spread: 30},
	}
	if objective.Keep(behind, spiky) {
		t.Fatalf("a node at a fifth more over its floor was kept, the band of %v took in the spread instead of the typical deviation", objective.Limit(spiky))
	}
}

func TestLeastLoadRanksIdleFirst(t *testing.T) {
	t.Parallel()
	// A node at its floor has no queueing at all, a score of zero, which must not read as
	// a missing score: idle would rank the node at its best behind every other one.
	idle := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 10, Floor: 100, Median: 100, Latest: 100,
	}}
	unstable := &selection.Node{Status: selection.StatusQualified, Stats: healthcheck.Stats{
		All: 10, Floor: 50, Median: 100, Latest: 100,
	}}
	objective := selection.NewIdleObjective(option.LoadBalancePickOptions{MinNodes: 1, MaxNodes: 1})
	ranked := objective.Rank([]*selection.Node{unstable, idle})
	if ranked[0] != idle {
		t.Fatalf("want the idle node first, got %v to %v", objective.Score(ranked[0]), objective.Score(ranked[1]))
	}
	if selected := selection.Filter(objective, []*selection.Node{unstable, idle}); len(selected) != 1 || selected[0] != idle {
		t.Fatalf("want only the idle node selected, got %d", len(selected))
	}
}
