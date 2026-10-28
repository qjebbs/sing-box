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

func newSelectionForTest(t *testing.T, tolerance healthcheck.RTT, tags ...string) (*selection, *healthcheck.Storages) {
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
	selection, storage := newSelectionForTest(t, 0, "slow", "fast")
	fill(storage, "slow", 200)
	fill(storage, "fast", 100)
	if tag := selectedTag(selection); tag != "fast" {
		t.Fatalf("want fast, got %s", tag)
	}
}

func TestSelectionIgnoresJitter(t *testing.T) {
	t.Parallel()
	// A steady node and a jittery one with the same median are the same choice: this
	// group uses one outbound for everything and takes the fastest one.
	selection, storage := newSelectionForTest(t, 0, "steady", "jittery")
	for i := 0; i < storage.Cap(); i++ {
		storage.Put("steady", 100)
	}
	// 7 fast samples and 3 slow ones: the median is still 100, the deviation is not
	for i := 0; i < storage.Cap(); i++ {
		if i%10 < 7 {
			storage.Put("jittery", 100)
		} else {
			storage.Put("jittery", 850)
		}
	}
	// the medians are equal, the declaration order decides
	if tag := selectedTag(selection); tag != "steady" {
		t.Fatalf("want steady, got %s", tag)
	}
	// a jittery node that is faster by more than the tolerance still wins
	selection, storage = newSelectionForTest(t, 0, "steady", "jittery")
	for i := 0; i < storage.Cap(); i++ {
		storage.Put("steady", 200)
	}
	for i := 0; i < storage.Cap(); i++ {
		if i%10 < 7 {
			storage.Put("jittery", 100)
		} else {
			storage.Put("jittery", 850)
		}
	}
	if tag := selectedTag(selection); tag != "jittery" {
		t.Fatalf("want jittery, got %s", tag)
	}
}

func TestSelectionWithoutOutbounds(t *testing.T) {
	t.Parallel()
	selection, _ := newSelectionForTest(t, 50)
	if node := selection.Selected("tcp"); node != nil {
		t.Fatalf("want no node, got %s", node.tag)
	}
}

func TestSelectionCachedUntilInvalidated(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 0, "fast", "slow")
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
	selection, storage := newSelectionForTest(t, 50, "selected", "better")
	fill(storage, "selected", 100)
	fill(storage, "better", 130)
	if tag := selectedTag(selection); tag != "selected" {
		t.Fatalf("want selected, got %s", tag)
	}
	// 100 is within the 50ms tolerance of 60, switching would only follow noise
	fill(storage, "better", 60)
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "selected" {
		t.Fatalf("want selected within the tolerance, got %s", tag)
	}
}

func TestSelectionSwitchesBeyondTolerance(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 20, "selected", "better")
	fill(storage, "selected", 100)
	fill(storage, "better", 130)
	if tag := selectedTag(selection); tag != "selected" {
		t.Fatalf("want selected, got %s", tag)
	}
	// 100 is beyond the 20ms tolerance of 60
	fill(storage, "better", 60)
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "better" {
		t.Fatalf("want better, got %s", tag)
	}
}

func TestSelectionSwitchesWhenNodeFails(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 1000, "failed", "alive")
	fill(storage, "failed", 100)
	fill(storage, "alive", 300)
	if tag := selectedTag(selection); tag != "failed" {
		t.Fatalf("want failed, got %s", tag)
	}
	// the tolerance is wide, but a failed node is never kept
	storage.Update("failed", healthcheck.Failed)
	selection.Invalidate()
	if tag := selectedTag(selection); tag != "alive" {
		t.Fatalf("want alive, got %s", tag)
	}
}

func TestSelectionKeepsUnmeasuredNode(t *testing.T) {
	t.Parallel()
	// No checks ran yet: there is no delay to compare, the selection must not move
	// on every call.
	selection, _ := newSelectionForTest(t, 50, "a", "b")
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
	selection, storage := newSelectionForTest(t, 0, "unmeasured", "measured")
	fill(storage, "measured", 900)
	if tag := selectedTag(selection); tag != "measured" {
		t.Fatalf("want the measured node, got %s", tag)
	}
}

func TestSelectionFallsBackToFailedNode(t *testing.T) {
	t.Parallel()
	// Nothing passed a check: the group still tries a node instead of failing.
	selection, storage := newSelectionForTest(t, 0, "failed")
	fill(storage, "failed", 100)
	storage.Update("failed", healthcheck.Failed)
	if tag := selectedTag(selection); tag != "failed" {
		t.Fatalf("want the failed node as the last resort, got %s", tag)
	}
}

func TestSelectionReselectReportsChange(t *testing.T) {
	t.Parallel()
	selection, storage := newSelectionForTest(t, 20, "a", "b")
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
	selection, storage := newSelectionForTest(t, 100, "a", "b")
	fill(storage, "a", 100)
	fill(storage, "b", 200)
	selection.Reselect("tcp")
	// within the 100ms tolerance: the change would only follow noise
	fill(storage, "b", 50)
	if node, changed := selection.Reselect("tcp"); node.tag != "a" || changed {
		t.Fatalf("want a unchanged, got %s changed=%v", node.tag, changed)
	}
}

func TestSelectionReselectWithoutCandidates(t *testing.T) {
	t.Parallel()
	selection, _ := newSelectionForTest(t, 50)
	if node, changed := selection.Reselect("tcp"); node != nil || changed {
		t.Fatalf("want no node, got %v changed=%v", node, changed)
	}
}
