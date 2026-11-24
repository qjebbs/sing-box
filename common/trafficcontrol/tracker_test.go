package trafficcontrol

import (
	"context"
	"net"
	"slices"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type trackerTestCaseOutbound struct {
	outbound.Adapter
}

func newTrackerTestCaseOutbound(tag string) *trackerTestCaseOutbound {
	return &trackerTestCaseOutbound{Adapter: outbound.NewAdapter("stub", tag, []string{N.NetworkTCP, N.NetworkUDP}, nil)}
}

func (o *trackerTestCaseOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, E.New("not supported")
}

func (o *trackerTestCaseOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, E.New("not supported")
}

// TestConnectionIsTrackedWhenItsChainIsSealed pins that a connection is only tracked once the
// dial is over: while it is dialed the chain holds only a part of the path, so the connection
// is not reported at all, instead of being reported with a path it does not take.
func TestConnectionIsTrackedWhenItsChainIsSealed(t *testing.T) {
	manager := NewManager()
	root := newTrackerTestCaseOutbound("root")
	gate := newTrackerTestCaseOutbound("gate")
	node := newTrackerTestCaseOutbound("node")

	chain := adapter.NewOutboundChain()
	chain.Append(root)
	ctx := adapter.WithOutboundChain(context.Background(), chain)
	conn, inboundConn := net.Pipe()
	defer conn.Close()
	defer inboundConn.Close()

	manager.RoutedConnection(ctx, conn, adapter.InboundContext{}, nil, root)
	if tracked := manager.ConnectionsLen(); tracked != 0 {
		t.Fatalf("want no connection tracked while it is dialed, got %d", tracked)
	}

	// The dial goes on: the connection passes through the group and the node it picked.
	chain.Append(gate, node)
	if tracked := manager.ConnectionsLen(); tracked != 0 {
		t.Fatalf("want no connection tracked while it is dialed, got %d", tracked)
	}

	chain.Seal()
	if tracked := manager.ConnectionsLen(); tracked != 1 {
		t.Fatalf("want the connection tracked once its chain is sealed, got %d", tracked)
	}
	metadata := manager.Connections()[0]
	if want := []string{"node", "gate", "root"}; !slices.Equal(metadata.Chain, want) {
		t.Fatalf("want %v, got %v", want, metadata.Chain)
	}
	if metadata.Outbound != "node" {
		t.Fatalf("want the outbound that carries the connection, got %s", metadata.Outbound)
	}
}
