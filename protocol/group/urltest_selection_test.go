package group

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	M "github.com/sagernet/sing/common/metadata"
)

type stubOutbound string

func (o stubOutbound) Network() []string      { return []string{"tcp", "udp"} }
func (o stubOutbound) Tag() string            { return string(o) }
func (o stubOutbound) Dependencies() []string { return nil }
func (o stubOutbound) Type() string           { return "stub" }
func (o stubOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, nil
}
func (o stubOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, nil
}

type stubSource []adapter.Outbound

func (s stubSource) Outbounds() []adapter.Outbound {
	return s
}

func newSelectionForTest(t *testing.T, tolerance float32, tags ...string) (*selection, *healthcheck.Storages) {
	t.Helper()
	outbounds := make([]adapter.Outbound, 0, len(tags))
	for _, tag := range tags {
		outbounds = append(outbounds, stubOutbound(tag))
	}
	storage := healthcheck.NewStorages(10, time.Hour)
	return newSelection(stubSource(outbounds), storage, tolerance), storage
}

// fill stores a full sampling window of the same delay, so the node has a median but
// no jitter.
func fill(storage *healthcheck.Storages, tag string, delay healthcheck.RTT) {
	for i := 0; i < storage.Cap(); i++ {
		storage.Put(tag, delay)
	}
}

func selectedTag(selection *selection) string {
	if node := selection.Selected("tcp"); node != nil {
		return node.tag
	}
	return ""
}

func TestSelectionUsesMedianDelay(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 0.0001, "slow", "fast")
	fill(storage, "slow", 200)
	fill(storage, "fast", 100)
	if tag := selectedTag(selection); tag != "fast" {
		t.Fatalf("want fast, got %s", tag)
	}
}

func TestSelectionPenalizesJitter(t *testing.T) {
	t.Parallel()
	// A steady node and a jittery one with the same median are not the same choice: the
	// group serves every connection with the one node it uses, so the deviation decides
	// the quality of all of them. The tolerance cannot replace this, its difference
	// would be zero, and whichever node was selected first would be kept.
	selection, storage := newSelectionForTest(t, 0.0001, "jittery", "steady")
	// 7 fast samples and 3 slow ones: the median is still 100, the deviation is not
	for i := 0; i < storage.Cap(); i++ {
		if i%10 < 7 {
			storage.Put("jittery", 100)
		} else {
			storage.Put("jittery", 850)
		}
		storage.Put("steady", 100)
	}
	if tag := selectedTag(selection); tag != "steady" {
		t.Fatalf("want steady, got %s", tag)
	}
}

func TestSelectionJitterIsWeighted(t *testing.T) {
	t.Parallel()
	// A node that is faster by more than its own deviation still wins: the jitter is a
	// penalty, not a veto.
	selection, storage := newSelectionForTest(t, 0.0001, "jittery", "steady")
	for i := 0; i < storage.Cap(); i++ {
		if i%10 < 7 {
			storage.Put("jittery", 100)
		} else {
			storage.Put("jittery", 160)
		}
		storage.Put("steady", 200)
	}
	if tag := selectedTag(selection); tag != "jittery" {
		t.Fatalf("want jittery, got %s", tag)
	}
}

func TestSelectionScoreUsesJitter(t *testing.T) {
	t.Parallel()
	// score = Cost(MED, STD, jitterWeight)
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		// the deviation is added, so an unstable node costs more than a steady one
		{name: "steady", stats: healthcheck.Stats{All: 10, Median: 160, Deviation: 10, Latest: 160}, want: 170},
		{name: "jittery", stats: healthcheck.Stats{All: 10, Median: 160, Deviation: 100, Latest: 160}, want: 260},
		// the failures are mixed in on top of the cost: (8*170 + 2*510) / 10
		{name: "failures", stats: healthcheck.Stats{All: 10, Fail: 2, Median: 160, Deviation: 10, Latest: 160}, want: 238},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &candidate{stats: testCase.stats}
			if got := node.score(); got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestSelectionWithoutOutbounds(t *testing.T) {
	t.Parallel()
	selection, _ := newSelectionForTest(t, defaultTolerance)
	if node := selection.Selected("tcp"); node != nil {
		t.Fatalf("want no node, got %s", node.tag)
	}
}

func TestSelectionCachedUntilInvalidated(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 0.0001, "fast", "slow")
	fill(storage, "fast", 100)
	fill(storage, "slow", 200)
	if tag := selectedTag(selection); tag != "fast" {
		t.Fatalf("want fast, got %s", tag)
	}
	// the checks made the other node faster, the cached selection doesn't move
	fill(storage, "slow", 50)
	if tag := selectedTag(selection); tag != "fast" {
		t.Fatalf("want the cached node, got %s", tag)
	}
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "slow" {
		t.Fatalf("want slow after invalidation, got %s", tag)
	}
}

func TestSelectionKeepsNodeWithinTolerance(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, defaultTolerance, "selected", "better")
	fill(storage, "selected", 600)
	fill(storage, "better", 700)
	if tag := selectedTag(selection); tag != "selected" {
		t.Fatalf("want selected, got %s", tag)
	}
	// the best node is 120ms better at most (20% of 600), 700 is not worth a switch
	fill(storage, "better", 600)
	fill(storage, "selected", 700)
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "selected" {
		t.Fatalf("want selected within the tolerance, got %s", tag)
	}
}

func TestSelectionSwitchesBeyondTolerance(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, defaultTolerance, "selected", "better")
	fill(storage, "selected", 900)
	fill(storage, "better", 1000)
	if tag := selectedTag(selection); tag != "selected" {
		t.Fatalf("want selected, got %s", tag)
	}
	// 600 is 180ms better at most, well beyond 900
	fill(storage, "better", 600)
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "better" {
		t.Fatalf("want better, got %s", tag)
	}
}

func TestSelectionSwitchesWhenNodeFails(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 0.9, "failed", "alive")
	fill(storage, "failed", 100)
	fill(storage, "alive", 300)
	if tag := selectedTag(selection); tag != "failed" {
		t.Fatalf("want failed, got %s", tag)
	}
	// the latest check failed: the node is down until a check gives it a result again,
	// so the wide tolerance cannot keep it
	storage.Update("failed", healthcheck.Failed)
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "alive" {
		t.Fatalf("want alive, got %s", tag)
	}
}

func TestSelectionScoresReportedFailures(t *testing.T) {
	t.Parallel()
	// A failure of the connections the group carried is an attempt that failed as well,
	// so it is mixed in next to the checks: a few are affordable, a storm is not.
	selection, storage := newSelectionForTest(t, 0.0001, "busy", "alive")
	fill(storage, "busy", 100)
	fill(storage, "alive", 200)
	if tag := selectedTag(selection); tag != "busy" {
		t.Fatalf("want busy, got %s", tag)
	}
	// ten connections carried, one of them failed: (10*100 + 1*300) / 20 = 115, the
	// node is still ahead, it is only scored for the share it drops
	busy := &candidate{outbound: stubOutbound("busy"), tag: "busy"}
	for i := 0; i < 10; i++ {
		selection.ReportConnection(busy, i != 0)
	}
	if tag := selectedTag(selection); tag != "busy" {
		t.Fatalf("want busy after one failure, got %s", tag)
	}
}

func TestSelectionKeepsUnmeasuredNode(t *testing.T) {
	t.Parallel()
	// No checks ran yet: there is no delay to compare, the selection must not move
	// on every call.
	selection, _ := newSelectionForTest(t, defaultTolerance, "a", "b")
	node := selection.Selected("tcp")
	if node == nil {
		t.Fatal("want a node")
	}
	tag := node.tag
	selection.Invalidate()
	if next := selectedTag(selection); next != tag {
		t.Fatalf("want %s kept, got %s", tag, next)
	}
}

func TestSelectionPrefersMeasuredOverUnmeasured(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 0.0001, "unmeasured", "measured")
	fill(storage, "measured", 900)
	if tag := selectedTag(selection); tag != "measured" {
		t.Fatalf("want the measured node, got %s", tag)
	}
}

func TestSelectionFallsBackToFailedNode(t *testing.T) {
	t.Parallel()
	// Nothing passed a check: the group still tries a node instead of failing.
	selection, storage := newSelectionForTest(t, 0.0001, "failed")
	fill(storage, "failed", 100)
	storage.Update("failed", healthcheck.Failed)
	if tag := selectedTag(selection); tag != "failed" {
		t.Fatalf("want the failed node as the last resort, got %s", tag)
	}
}

func TestSelectionReselectReportsChange(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, defaultTolerance, "a", "b")
	fill(storage, "a", 100)
	fill(storage, "b", 200)
	// nothing was selected before, so nothing can be using the group yet
	if node, changed := selection.Reselect("tcp"); node.tag != "a" || changed {
		t.Fatalf("want a unchanged, got %s changed=%v", node.tag, changed)
	}
	// the same selection again is not a change
	if node, changed := selection.Reselect("tcp"); node.tag != "a" || changed {
		t.Fatalf("want a unchanged, got %s changed=%v", node.tag, changed)
	}
	// the other node is now faster by more than the tolerance
	fill(storage, "b", 50)
	if node, changed := selection.Reselect("tcp"); node.tag != "b" || !changed {
		t.Fatalf("want b changed, got %s changed=%v", node.tag, changed)
	}
}

func TestSelectionReselectKeepsWithinTolerance(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, defaultTolerance, "a", "b")
	fill(storage, "a", 100)
	fill(storage, "b", 200)
	selection.Reselect("tcp")
	// 95 is 19ms better at most, within the 20% of the 95ms best node
	fill(storage, "b", 95)
	if node, changed := selection.Reselect("tcp"); node.tag != "a" || changed {
		t.Fatalf("want a unchanged, got %s changed=%v", node.tag, changed)
	}
}

func TestSelectionReselectWithoutCandidates(t *testing.T) {
	t.Parallel()
	selection, _ := newSelectionForTest(t, defaultTolerance)
	if node, changed := selection.Reselect("tcp"); node != nil || changed {
		t.Fatalf("want no node, got %v changed=%v", node, changed)
	}
}

func TestCandidateString(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  string
	}{
		{
			name:  "measured",
			stats: healthcheck.Stats{All: 10, Deviation: 57, Median: 164, Latest: 155},
			want:  "#1 OK [node] STD=57ms MED=164ms Latest=155ms FAIL=0/10",
		},
		// a node whose checks all failed has no successful delay to show
		{
			name:  "all failed",
			stats: healthcheck.Stats{All: 1, Fail: 1},
			want:  "#1 x [node] STD=None MED=None Latest=None FAIL=1/1",
		},
		// a node that was never checked has nothing to show
		{
			name:  "never checked",
			stats: healthcheck.Stats{},
			want:  "#1 ? [node] STD=None MED=None Latest=None FAIL=0/0",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &candidate{tag: "node", stats: testCase.stats, index: 1}
			if got := node.String(); got != testCase.want {
				t.Fatalf("want %q, got %q", testCase.want, got)
			}
		})
	}
}

func TestCandidatesNumberedInConfigOrder(t *testing.T) {
	t.Parallel()
	// The number identifies the node in the configuration, like the load balance log,
	// not its rank: b is first because it is faster, but it is the second configured.
	selection, storage := newSelectionForTest(t, 0.0001, "a", "b")
	fill(storage, "a", 200)
	fill(storage, "b", 100)
	all := selection.candidates("tcp")
	if len(all) != 2 {
		t.Fatalf("want 2 candidates, got %v", len(all))
	}
	if all[0].tag != "b" || all[0].index != 2 {
		t.Fatalf("want b as #2, got %s as #%v", all[0].tag, all[0].index)
	}
	if all[1].tag != "a" || all[1].index != 1 {
		t.Fatalf("want a as #1, got %s as #%v", all[1].tag, all[1].index)
	}
}

func TestCandidateScore(t *testing.T) {
	t.Parallel()
	// The score is the average cost of an attempt: the median of the successful checks
	// mixed with the timeout the failed ones waited for.
	testCases := []struct {
		name  string
		stats healthcheck.Stats
		want  healthcheck.RTT
	}{
		// without a failure the score is the median delay itself
		{name: "measured", stats: healthcheck.Stats{All: 10, Median: 164, Latest: 160}, want: 164},
		// (9*164 + 1*492) / 10, a failed attempt is worth three that worked
		{name: "rare failures", stats: healthcheck.Stats{All: 10, Fail: 1, Median: 164, Latest: 160}, want: 196},
		// (5*164 + 5*492) / 10
		{name: "frequent failures", stats: healthcheck.Stats{All: 10, Fail: 5, Median: 164, Latest: 160}, want: 328},
		// every attempt waits for the timeout
		{name: "all failed", stats: healthcheck.Stats{All: 10, Fail: 10, Latest: healthcheck.Failed}, want: 15000},
		// nothing to compare, and 0ms would read as the best possible result
		{name: "never checked", stats: healthcheck.Stats{}, want: healthcheck.Failed},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &candidate{stats: testCase.stats}
			if got := node.score(); got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestSelectionPenalizesFailures(t *testing.T) {
	t.Parallel()
	// A failed attempt is worth three that worked, in the scale of the node: 3 failures
	// of 10 cost 60ms on a 100ms node, which still leaves it ahead of one that is twice
	// as slow.
	selection, storage := newSelectionForTest(t, defaultTolerance, "flaky", "clean")
	for i := 0; i < storage.Cap(); i++ {
		if i < 3 {
			storage.Put("flaky", healthcheck.Failed)
		} else {
			storage.Put("flaky", 100)
		}
		storage.Put("clean", 200)
	}
	// (7*100 + 3*300) / 10 = 160
	if tag := selectedTag(selection); tag != "flaky" {
		t.Fatalf("want flaky, got %s", tag)
	}
}

func TestSelectionFailurePenaltyGrowsWithTheFailureRate(t *testing.T) {
	t.Parallel()
	// 6 failures of 10 cost 120ms on the same node, which puts it behind the clean one:
	// the penalty follows the failure rate, whatever the delay of the node.
	selection, storage := newSelectionForTest(t, defaultTolerance, "flaky", "clean")
	for i := 0; i < storage.Cap(); i++ {
		if i < 6 {
			storage.Put("flaky", healthcheck.Failed)
		} else {
			storage.Put("flaky", 100)
		}
		storage.Put("clean", 200)
	}
	// (4*100 + 6*300) / 10 = 220
	if tag := selectedTag(selection); tag != "clean" {
		t.Fatalf("want clean, got %s", tag)
	}
}

func TestSelectionFailurePenaltyIsProportional(t *testing.T) {
	t.Parallel()
	// One failure in ten makes a tenth of the attempts wait for the 15s timeout,
	// which is 1.5s of average delay: still better than a node that is 4s slower
	// every time.
	selection, storage := newSelectionForTest(t, 0.0001, "flaky", "slow")
	for i := 0; i < storage.Cap(); i++ {
		if i == 0 {
			storage.Put("flaky", healthcheck.Failed)
		} else {
			storage.Put("flaky", 100)
		}
		storage.Put("slow", 4000)
	}
	if tag := selectedTag(selection); tag != "flaky" {
		t.Fatalf("want flaky, got %s", tag)
	}
}

func TestRelativeTolerance(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		value   float32
		want    float32
		wantErr bool
	}{
		{name: "default", value: 0, want: defaultTolerance},
		{name: "configured", value: 0.35, want: 0.35},
		// a delay in milliseconds used to be the unit, it is a fraction now
		{name: "absolute value from an old configuration", value: 50, wantErr: true},
		{name: "a hundred percent", value: 1, wantErr: true},
		{name: "negative", value: -1, wantErr: true},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := relativeTolerance(testCase.value)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestBand(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		tolerance float32
		bestScore uint32
		want      uint32
	}{
		// a fifth of the best score, so the band follows the quality of the nodes
		{name: "fast set", tolerance: defaultTolerance, bestScore: 150, want: 30},
		{name: "slow set", tolerance: defaultTolerance, bestScore: 500, want: 100},
		// the floor keeps a local node, where a fifth is a few milliseconds, from
		// being split by noise
		{name: "floor", tolerance: defaultTolerance, bestScore: 5, want: uint32(toleranceFloor)},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			selection := &selection{tolerance: testCase.tolerance}
			if got := selection.band(testCase.bestScore); got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestSelectionIsScaleInvariant(t *testing.T) {
	t.Parallel()
	// The same relative tolerance keeps a node that is 15% worse in both a fast and a
	// slow set, and switches away from one that is 35% worse.
	testCases := []struct {
		name     string
		selected healthcheck.RTT
		better   healthcheck.RTT
		want     string
	}{
		{name: "fast set, within", selected: 150, better: 130, want: "selected"},
		{name: "slow set, within", selected: 500, better: 430, want: "selected"},
		{name: "fast set, beyond", selected: 150, better: 100, want: "better"},
		{name: "slow set, beyond", selected: 500, better: 330, want: "better"},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			selection, storage := newSelectionForTest(t, defaultTolerance, "selected", "better")
			fill(storage, "selected", testCase.selected)
			// the other node starts out worse, so the selection is the one under test
			fill(storage, "better", testCase.selected*2)
			selection.Reselect("tcp")
			// it then becomes faster by the amount of the test case
			fill(storage, "better", testCase.better)
			node, _ := selection.Reselect("tcp")
			if node.tag != testCase.want {
				t.Fatalf("want %s, got %s", testCase.want, node.tag)
			}
		})
	}
}

func TestCandidateScoreIsBoundedByTheTimeout(t *testing.T) {
	t.Parallel()
	// A node checked with a longer timeout can have a successful delay above the
	// current one. An attempt still cannot cost more than the timeout it waits for.
	node := &candidate{stats: healthcheck.Stats{All: 1, Median: 30000, Latest: 30000}}
	if got := node.score(); got != 15000 {
		t.Fatalf("want the score capped at 15s, got %v", got)
	}
}
