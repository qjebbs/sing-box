package base

import (
	"context"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type chainTestCaseOutbound struct {
	outbound.Adapter
	err error
}

func newChainTestCaseOutbound(tag string) *chainTestCaseOutbound {
	return &chainTestCaseOutbound{Adapter: outbound.NewAdapter("stub", tag, []string{N.NetworkTCP, N.NetworkUDP}, nil)}
}

func (o *chainTestCaseOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	if o.err != nil {
		return nil, o.err
	}
	conn, _ := net.Pipe()
	return conn, nil
}

func (o *chainTestCaseOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, E.New("not supported")
}

// chainTestCaseStrategy counts the picks, so a test can tell a pick from a read.
type chainTestCaseStrategy struct {
	selection.Strategy
	picks int
}

func (s *chainTestCaseStrategy) Pick(all, filtered []*selection.Node, metadata *adapter.InboundContext) *selection.Node {
	s.picks++
	return s.Strategy.Pick(all, filtered, metadata)
}

// newChainTestCaseGroup builds a started-looking group whose ranking is already cached, so a
// dial needs no health check.
func newChainTestCaseGroup(t *testing.T, strategy selection.Strategy, outbounds ...adapter.Outbound) *Group {
	t.Helper()
	nodes := make([]*selection.Node, 0, len(outbounds))
	for i, outbound := range outbounds {
		nodes = append(nodes, selection.NewNode(outbound, i, 1, healthcheck.Stats{}, selection.StatusAlive))
	}
	objective, err := selection.NewObjective(selection.ObjectiveAlive, selection.ObjectiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return &Group{
		GroupAdapter:   outbound.NewGroupAdapter("stub", "group", []string{N.NetworkTCP, N.NetworkUDP}, option.ProviderGroupCommonOption{}),
		logger:         log.NewNOPFactory().Logger(),
		interruptGroup: interrupt.NewTaggedGroup(),
		pick:           selection.NewBalancing(objective, strategy),
		snapshot: map[string]*nodeSnapshot{
			N.NetworkTCP: {generation: 0, all: nodes, selected: nodes, best: nodes[0]},
			N.NetworkUDP: {generation: 0, all: nodes, selected: nodes, best: nodes[0]},
		},
	}
}

// chainTestCaseContext returns a connection context the way the router carries one: the
// metadata of the connection and the chain it records into.
func chainTestCaseContext() (context.Context, *adapter.OutboundChain) {
	metadata := &adapter.InboundContext{
		Network:     N.NetworkTCP,
		Destination: chainTestCaseDestination,
	}
	chain := adapter.NewOutboundChain()
	ctx := adapter.WithContext(context.Background(), metadata)
	return adapter.WithOutboundChain(ctx, chain), chain
}

var chainTestCaseDestination = M.ParseSocksaddr("example.com:443")

func TestGroupRecordsTheNodeItDials(t *testing.T) {
	leaf := newChainTestCaseOutbound("node")
	group := newChainTestCaseGroup(t, selection.NewRoundRobinStrategy(), leaf)
	ctx, chain := chainTestCaseContext()

	conn, err := group.DialContext(ctx, N.NetworkTCP, chainTestCaseDestination)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	tags := chain.Tags()
	if len(tags) != 2 || tags[0] != "group" || tags[1] != "node" {
		t.Fatalf("want the group and the node it dialed, got %v", tags)
	}
	// The connection is bound to the node that carries it, so it is interrupted with the
	// same node it was recorded with.
	inUse := group.interruptGroup.Tags()
	if len(inUse) != 1 || inUse[0] != "node" {
		t.Fatalf("want the connection bound to the dialed node, got %v", inUse)
	}
}

func TestGroupRecordsTheNodeThatSucceeded(t *testing.T) {
	failing := newChainTestCaseOutbound("failing")
	failing.err = E.New("dial failed")
	working := newChainTestCaseOutbound("working")
	group := newChainTestCaseGroup(t, selection.NewRoundRobinStrategy(), failing, working)
	ctx, chain := chainTestCaseContext()

	conn, err := group.DialContext(ctx, N.NetworkTCP, chainTestCaseDestination)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	tags := chain.Tags()
	if len(tags) != 2 || tags[1] != "working" {
		t.Fatalf("want the node that carried the connection, got %v", tags)
	}
}

func TestGroupDoesNotRepeatTheOutboundItIsDialedBy(t *testing.T) {
	leaf := newChainTestCaseOutbound("node")
	group := newChainTestCaseGroup(t, selection.NewRoundRobinStrategy(), leaf)
	ctx, chain := chainTestCaseContext()
	// The router records the group it routes the connection to, before the group dials.
	chain.Append(group)

	conn, err := group.DialContext(ctx, N.NetworkTCP, chainTestCaseDestination)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	tags := chain.Tags()
	if len(tags) != 2 || tags[0] != "group" || tags[1] != "node" {
		t.Fatalf("want the group once, followed by its node, got %v", tags)
	}
}

func TestSelectedIsNotAPick(t *testing.T) {
	strategy := &chainTestCaseStrategy{Strategy: selection.NewRoundRobinStrategy()}
	leaf := newChainTestCaseOutbound("node")
	group := newChainTestCaseGroup(t, strategy, leaf)
	// Selected only reads the selection the group serves from, so it needs no dial and no
	// health check: the ranking is cached.
	group.HealthCheck = &healthcheck.HealthCheck{}
	for range 3 {
		selected := group.Selected(N.NetworkTCP)
		if selected == nil || selected.Tag() != "node" {
			t.Fatalf("want the node of the selection, got %v", selected)
		}
	}
	if strategy.picks != 0 {
		t.Fatalf("want no pick from reading the selection, got %d", strategy.picks)
	}
	if inUse := group.interruptGroup.Tags(); len(inUse) != 0 {
		t.Fatalf("want no connection bound by a read, got %v", inUse)
	}
}

func TestPickOutboundPicksOnce(t *testing.T) {
	strategy := &chainTestCaseStrategy{Strategy: selection.NewRoundRobinStrategy()}
	leaf := newChainTestCaseOutbound("node")
	group := newChainTestCaseGroup(t, strategy, leaf)
	group.HealthCheck = &healthcheck.HealthCheck{}
	picked := group.PickOutbound(N.NetworkTCP, &adapter.InboundContext{Destination: M.ParseSocksaddr("example.com:443")})
	if picked == nil || picked.Tag() != "node" {
		t.Fatalf("want the picked node, got %v", picked)
	}
	if strategy.picks != 1 {
		t.Fatalf("want one pick for one connection, got %d", strategy.picks)
	}
}
