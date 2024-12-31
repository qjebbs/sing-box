package group

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
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
	_ adapter.Outbound = (*Chain)(nil)
)

// Chain is a chain of outbounds.
type Chain struct {
	outbound.Adapter
	ctx        context.Context
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
		ctx:        ctx,
		router:     router,
		logger:     logger,
		outbound:   service.FromContext[adapter.OutboundManager](ctx),
		connection: service.FromContext[adapter.ConnectionManager](ctx),

		outboundTags: options.Outbounds,
		outbounds:    make([]adapter.Outbound, len(options.Outbounds)-1),
	}
	return Chain, nil
}

// Start starts the chain.
func (s *Chain) Start() error {
	lastTag := s.outboundTags[len(s.outboundTags)-1]
	detour, loaded := s.outbound.Outbound(lastTag)
	if !loaded {
		return E.New("["+lastTag, "] not found")
	}
	for i := len(s.outboundTags) - 2; i >= 0; i-- {
		tag := s.outboundTags[i]
		outbound, err := s.outbound.DupOverrideDetour(s.ctx, s.router, tag, detour)
		if err != nil {
			return E.New("failed to create [", tag, "] for chain [", s.Tag(), "]: ", err)
		}
		s.outbounds[i] = outbound
		detour = outbound
	}
	return nil
}

// Close implements the adapter.Closable interface.
func (s *Chain) Close() error {
	var err error
	for _, outbound := range s.outbounds {
		if err2 := common.Close(outbound); err2 != nil {
			err = E.Append(err, err2, func(err error) error {
				return E.New("close [", outbound.Tag(), "]: ", err)
			})
		}
	}
	return err
}

// DialContext implements the network.Dialer interface.
func (s *Chain) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return s.outbounds[0].DialContext(ctx, network, destination)
}

// ListenPacket implements the network.Dialer interface.
func (s *Chain) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return s.outbounds[0].ListenPacket(ctx, destination)
}
