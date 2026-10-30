package group

import (
	"context"
	"errors"
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

// RegisterRelay registers the relay provider to the outbound registry.
func RegisterRelay(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeRelay, NewRelay)
}

var (
	_ adapter.Outbound  = (*Relay)(nil)
	_ adapter.Lifecycle = (*Relay)(nil)
)

// Relay is a chain of outbounds the connection passes through in order.
type Relay struct {
	outbound.Adapter
	router     adapter.Router
	logger     log.ContextLogger
	outbound   adapter.OutboundManager
	connection adapter.ConnectionManager

	outboundTags []string
	outbounds    []adapter.Outbound
}

// NewRelay creates a new relay outbound.
func NewRelay(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.RelayOptions) (adapter.Outbound, error) {
	if len(options.Outbounds) < 2 {
		return nil, E.New("relay requires 2 or more outbounds")
	}
	relay := &Relay{
		Adapter:    outbound.NewAdapter(C.TypeRelay, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.Outbounds),
		router:     router,
		logger:     logger,
		outbound:   service.FromContext[adapter.OutboundManager](ctx),
		connection: service.FromContext[adapter.ConnectionManager](ctx),

		outboundTags: options.Outbounds,
		outbounds:    make([]adapter.Outbound, len(options.Outbounds)),
	}
	return relay, nil
}

// Start starts the chain. The duplicated outbounds are created in the scope owned by the
// chain, so they are torn down together with it.
//
// The nodes are listed in the order the connection passes through them, so each of them dials
// through the one before it: the first node is reached directly and keeps its own dialer, the
// others are copies with their detour set to it. The connection then enters at the first node
// and leaves at the last one.
func (s *Relay) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	firstTag := s.outboundTags[0]
	detour, loaded := s.outbound.Outbound(firstTag)
	if !loaded {
		return E.New("["+firstTag, "] not found")
	}
	s.outbounds[0] = detour
	for i := 1; i < len(s.outboundTags); i++ {
		tag := s.outboundTags[i]
		outbound, err := s.outbound.CreateDetour(scope.Context(), s.router, s.logger, tag, detour, scope)
		if err != nil {
			var noDialFields *adapter.NoDialFieldsError
			if errors.As(err, &noDialFields) {
				role := "a hop"
				if i == len(s.outboundTags)-1 {
					role = "the exit"
				}
				return E.New("[", tag, "] cannot be ", role, " of relay [", s.Tag(), "]: ", noDialFields, ", only the entry can be an outbound group")
			}
			return E.New("failed to create [", tag, "] for relay [", s.Tag(), "]: ", err)
		}
		s.outbounds[i] = outbound
		detour = outbound
	}
	return nil
}

// DialContext implements the network.Dialer interface.
func (s *Relay) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return s.exit().DialContext(ctx, network, destination)
}

// ListenPacket implements the network.Dialer interface.
func (s *Relay) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return s.exit().ListenPacket(ctx, destination)
}

// exit returns the node the connection leaves from, which is the last one of the chain.
func (s *Relay) exit() adapter.Outbound {
	return s.outbounds[len(s.outbounds)-1]
}
