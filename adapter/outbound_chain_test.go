package adapter

import (
	"context"
	"net"
	"testing"

	M "github.com/sagernet/sing/common/metadata"

	"github.com/stretchr/testify/require"
)

type chainTestOutbound struct {
	tag string
}

func (o *chainTestOutbound) Type() string           { return "stub" }
func (o *chainTestOutbound) Tag() string            { return o.tag }
func (o *chainTestOutbound) Network() []string      { return nil }
func (o *chainTestOutbound) Dependencies() []string { return nil }
func (o *chainTestOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, nil
}
func (o *chainTestOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, nil
}

// TestChainIsSealedOnce pins that a chain that ends in a dial and a chain that ends in a
// hand-over are both sealed, that sealing twice changes nothing, and that a consumer that
// arrives late is served right away.
func TestChainIsSealedOnce(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		seal func(*OutboundChain)
	}{
		{"dial", (*OutboundChain).Seal},
		{"hand-over", (*OutboundChain).HandOver},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			chain := NewOutboundChain()
			calls := 0
			chain.WhenSealed(func() { calls++ })
			testCase.seal(chain)
			testCase.seal(chain)
			require.Equal(t, 1, calls, "want the chain sealed once")
			require.True(t, chain.Sealed())
			chain.WhenSealed(func() { calls++ })
			require.Equal(t, 2, calls, "want a late consumer served right away")
		})
	}
}

// TestFailedChainIsNeverSealed pins that a chain whose dial failed is not sealed and reports
// the failure: it is what tells the routing that a chain left unsealed is expected rather
// than a dial path that forgot to report its connection.
func TestFailedChainIsNeverSealed(t *testing.T) {
	t.Parallel()
	chain := NewOutboundChain()
	calls := 0
	chain.WhenSealed(func() { calls++ })
	chain.Fail()
	require.True(t, chain.Failed())
	require.False(t, chain.Sealed())
	require.Equal(t, 0, calls, "want a failed chain not sealed")
}

// TestChainKeepsLateHops pins that a hop that records after the seal is kept: the chain is
// what the APIs show, so a late hop is reported rather than rejected.
func TestChainKeepsLateHops(t *testing.T) {
	t.Parallel()
	chain := NewOutboundChain()
	chain.Append(&chainTestOutbound{tag: "routed"})
	chain.Seal()
	chain.Append(&chainTestOutbound{tag: "late"})
	require.Equal(t, []string{"routed", "late"}, chain.Tags())
}
