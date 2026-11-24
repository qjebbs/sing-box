package group

import (
	"context"
	"net"
	"slices"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type chainTestCaseNode struct {
	outbound.Adapter
	detour N.Dialer
}

func newChainTestCaseNode(tag string) *chainTestCaseNode {
	return &chainTestCaseNode{Adapter: outbound.NewAdapter("stub", tag, []string{N.NetworkTCP, N.NetworkUDP}, nil)}
}

func (n *chainTestCaseNode) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if n.detour != nil {
		// A proxy dials the server it connects to through its detour, which is what the
		// relay hands it as its own detour.
		detourConn, err := n.detour.DialContext(ctx, network, destination)
		if err != nil {
			return nil, err
		}
		detourConn.Close()
	}
	conn, _ := net.Pipe()
	return conn, nil
}

func (n *chainTestCaseNode) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, E.New("not supported")
}

func newChainTestCaseSelector(tag string, selected adapter.Outbound) *SelectorProvider {
	selector := &SelectorProvider{
		GroupAdapter:   outbound.NewGroupAdapter("selector", tag, []string{N.NetworkTCP, N.NetworkUDP}, option.ProviderGroupCommonOption{}),
		interruptGroup: interrupt.NewGroup(),
	}
	selector.selected.Store(selected)
	return selector
}

// TestRelayRecordsThePathItCarries pins the chain of the topology the fork is used with: a
// selector over a relay, whose own path is a selector over the node it exits from.
func TestRelayRecordsThePathItCarries(t *testing.T) {
	node := newChainTestCaseNode("node")
	gate := newChainTestCaseSelector("gate", node)
	exit := newChainTestCaseNode("exit")
	// The exit is the copy the relay creates: it reaches the target, and the hops before it
	// are dialed as its detour.
	exit.detour = gate
	relay := &Relay{
		Adapter:      outbound.NewAdapter("relay", "relay", []string{N.NetworkTCP, N.NetworkUDP}, []string{"gate", "exit"}),
		outboundTags: []string{"gate", "exit"},
		outbounds:    []adapter.Outbound{gate, exit},
	}
	root := newChainTestCaseSelector("root", relay)

	chain := adapter.NewOutboundChain()
	chain.Append(root)
	ctx := adapter.WithOutboundChain(adapter.WithContext(context.Background(), &adapter.InboundContext{}), chain)

	conn, err := root.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr("example.com:443"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// The path is recorded in the order the connection passes through it, and a group is
	// followed by the node it picked.
	want := []string{"root", "relay", "gate", "node", "exit"}
	if tags := chain.Tags(); !slices.Equal(tags, want) {
		t.Fatalf("want %v, got %v", want, tags)
	}
}
