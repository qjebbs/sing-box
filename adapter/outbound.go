package adapter

import (
	"context"
	"net/netip"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	N "github.com/sagernet/sing/common/network"
)

// Note: for proxy protocols, outbound creates early connections by default.

type Outbound interface {
	Type() string
	Tag() string
	Network() []string
	Dependencies() []string
	N.Dialer
}

type OutboundWithPreferredRoutes interface {
	Outbound
	PreferredDomain(metadata *InboundContext, domain string) bool
	PreferredAddress(metadata *InboundContext, address netip.Addr) bool
}

type OutboundWithMultiplex interface {
	Outbound
	MultiplexEnabled() bool
}

type FlowOutbound interface {
	Outbound
	tun.Port
	PreMatchFlow(network string, destination netip.Addr) PreMatchAction
}

type OutboundRegistry interface {
	option.OutboundOptionsRegistry
	CreateOutbound(ctx context.Context, router Router, logger log.ContextLogger, tag string, outboundType string, options any) (Outbound, error)
}

type OutboundManager interface {
	Lifecycle
	Outbounds() []Outbound
	Outbound(tag string) (Outbound, bool)
	Default() Outbound
	Remove(tag string) error
	Create(ctx context.Context, router Router, logger log.ContextLogger, tag string, outboundType string, options any) error
	// CreateDetour creates a copy of the outbound with the specified tag, dialing through detour
	// instead of its configured detour. The copy keeps the tag of the outbound it copies and is
	// not managed by the manager: it starts in the given scope and lives until that scope is
	// closed, so several detoured outbounds can share one scope.
	// The original outbound is not affected.
	CreateDetour(ctx context.Context, router Router, logger log.ContextLogger, tag string, detour N.Dialer, scope *Scope) (Outbound, error)
}

type IdleConnectionKeeper interface {
	SetKeepIdleConnections(keep bool)
	CloseIdleConnections()
}

type Referrer interface {
	References() []string
}
