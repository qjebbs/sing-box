package group

import (
	"context"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type stubOutbound struct {
	tag     string
	network []string
	err     error
}

func (s *stubOutbound) Type() string           { return "stub" }
func (s *stubOutbound) Tag() string            { return s.tag }
func (s *stubOutbound) Network() []string      { return s.network }
func (s *stubOutbound) Dependencies() []string { return nil }
func (s *stubOutbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return nil, s.err
}

func (s *stubOutbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, s.err
}

func TestFallbackIsNotOutboundGroup(t *testing.T) {
	t.Parallel()
	// The router resolves an adapter.OutboundGroup to the node it selects before
	// dialing, so a group would never reach Fallback.DialContext.
	if _, isGroup := any(&Fallback{}).(adapter.OutboundGroup); isGroup {
		t.Fatal("fallback must not implement adapter.OutboundGroup")
	}
}

func TestDialFirstTriesNodesInOrder(t *testing.T) {
	t.Parallel()
	nodes := []adapter.Outbound{
		&stubOutbound{tag: "first", err: E.New("dial failed")},
		&stubOutbound{tag: "second"},
		&stubOutbound{tag: "third"},
	}
	var tried []string
	_, err := dialFirst(context.Background(), log.NewNOPFactory().Logger(), nodes, nil, dialStub(&tried))
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if !slices.Equal(tried, []string{"first", "second"}) {
		t.Fatalf("want the first two nodes tried in order, got %v", tried)
	}
}

func TestDialFirstReportsEveryNode(t *testing.T) {
	t.Parallel()
	nodes := []adapter.Outbound{
		&stubOutbound{tag: "first", err: E.New("dial failed")},
		&stubOutbound{tag: "second", err: E.New("reset by peer")},
	}
	_, err := dialFirst(context.Background(), log.NewNOPFactory().Logger(), nodes, nil, dialStub(nil))
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	for _, tag := range []string{"first", "second"} {
		if !strings.Contains(err.Error(), tag) {
			t.Fatalf("want the error to mention %s, got %v", tag, err)
		}
	}
}

func TestDialFirstStopsOnCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nodes := []adapter.Outbound{
		&stubOutbound{tag: "first"},
		&stubOutbound{tag: "second"},
	}
	var tried []string
	_, err := dialFirst(ctx, log.NewNOPFactory().Logger(), nodes, nil, dialStub(&tried))
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if len(tried) != 0 {
		t.Fatalf("want no attempt with a canceled context, got %v", tried)
	}
}

func TestFilterCandidates(t *testing.T) {
	t.Parallel()
	outbounds := []adapter.Outbound{
		&stubOutbound{tag: "tcp-only", network: []string{N.NetworkTCP}},
		&stubOutbound{tag: "both", network: []string{N.NetworkTCP, N.NetworkUDP}},
		&stubOutbound{tag: "unknown"},
	}
	udpNodes := filterCandidates(outbounds, N.NetworkUDP)
	if !slices.Equal(tagsOf(udpNodes), []string{"both", "unknown"}) {
		t.Fatalf("unexpected udp candidates: %v", tagsOf(udpNodes))
	}
	tcpNodes := filterCandidates(outbounds, N.NetworkTCP)
	if !slices.Equal(tagsOf(tcpNodes), []string{"tcp-only", "both", "unknown"}) {
		t.Fatalf("unexpected tcp candidates: %v", tagsOf(tcpNodes))
	}
}

// dialStub dials like the fallback would, and records the tags it was given when
// tried is not nil.
func dialStub(tried *[]string) func(node adapter.Outbound) (string, error) {
	return func(node adapter.Outbound) (string, error) {
		if tried != nil {
			*tried = append(*tried, node.Tag())
		}
		_, err := node.DialContext(context.Background(), N.NetworkTCP, M.Socksaddr{})
		return node.Tag(), err
	}
}

func tagsOf(outbounds []adapter.Outbound) []string {
	tags := make([]string, 0, len(outbounds))
	for _, outbound := range outbounds {
		tags = append(tags, outbound.Tag())
	}
	return tags
}

func TestOrderCandidates(t *testing.T) {
	t.Parallel()
	outbounds := []adapter.Outbound{
		&stubOutbound{tag: "a"},
		&stubOutbound{tag: "b"},
		&stubOutbound{tag: "c"},
		&stubOutbound{tag: "d"},
	}
	alive := map[string]bool{"c": true, "d": true}
	if got := tagsOf(orderCandidates(outbounds, func(tag string) bool { return alive[tag] })); !slices.Equal(got, []string{"c", "d", "a", "b"}) {
		t.Fatalf("want the alive nodes first in declared order, got %v", got)
	}
	// nothing was checked yet: the declared order is kept as it is
	if got := tagsOf(orderCandidates(outbounds, func(string) bool { return false })); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("want the declared order without any alive node, got %v", got)
	}
	// a group whose checks all fail is still tried in the declared order
	if got := tagsOf(orderCandidates(outbounds, nil)); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("want the declared order without a checker, got %v", got)
	}
}

func TestDialFirstObservesEveryAttempt(t *testing.T) {
	t.Parallel()
	nodes := []adapter.Outbound{
		&stubOutbound{tag: "first", err: E.New("dial failed")},
		&stubOutbound{tag: "second"},
	}
	var observed []string
	_, err := dialFirst(context.Background(), log.NewNOPFactory().Logger(), nodes,
		func(node adapter.Outbound, established bool) {
			observed = append(observed, node.Tag()+"="+strconv.FormatBool(established))
		},
		dialStub(nil),
	)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if !slices.Equal(observed, []string{"first=false", "second=true"}) {
		t.Fatalf("want both outcomes observed, got %v", observed)
	}
}
