package group

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

// RegisterChain registers the chain provider to the outbound registry.
func RegisterChain(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeChain, NewChain)
}

var (
	_ adapter.Outbound  = (*Chain)(nil)
	_ adapter.Lifecycle = (*Chain)(nil)
)

// Chain is a chain of outbounds.
type Chain struct {
	outbound.Adapter
	router     adapter.Router
	logger     log.ContextLogger
	outbound   adapter.OutboundManager
	connection adapter.ConnectionManager

	outboundTags []string
	outbounds    []adapter.Outbound
}

// NewChain creates a new chain outbound.
func NewChain(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ChainOptions) (adapter.Outbound, error) {
	if len(options.Outbounds) < 2 {
		return nil, E.New("chain requires 2 or more outbounds")
	}
	Chain := &Chain{
		Adapter:    outbound.NewAdapter(C.TypeChain, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.Outbounds),
		router:     router,
		logger:     logger,
		outbound:   service.FromContext[adapter.OutboundManager](ctx),
		connection: service.FromContext[adapter.ConnectionManager](ctx),

		outboundTags: options.Outbounds,
		outbounds:    make([]adapter.Outbound, len(options.Outbounds)-1),
	}
	return Chain, nil
}

// Start starts the chain. The duplicated outbounds are created in the scope owned by the
// chain, so they are torn down together with it.
func (s *Chain) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	lastTag := s.outboundTags[len(s.outboundTags)-1]
	detour, loaded := s.outbound.Outbound(lastTag)
	if !loaded {
		return E.New("["+lastTag, "] not found")
	}
	for i := len(s.outboundTags) - 2; i >= 0; i-- {
		tag := s.outboundTags[i]
		outbound, err := s.outbound.CreateDetour(scope.Context(), s.router, s.logger, tag, detour, scope)
		if err != nil {
			return E.New("failed to create [", tag, "] for chain [", s.Tag(), "]: ", err)
		}
		s.outbounds[i] = outbound
		detour = outbound
	}
	return nil
}

// DialContext implements the network.Dialer interface.
func (s *Chain) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return s.outbounds[0].DialContext(ctx, network, destination)
}

// ListenPacket implements the network.Dialer interface.
func (s *Chain) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return s.outbounds[0].ListenPacket(ctx, destination)
}
