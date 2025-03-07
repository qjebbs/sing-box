package dialer

import (
	"context"
	"net"

	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type detourVarKey struct{}

// ContextWithDetourVar returns a new context with the detour var.
func ContextWithDetourVar(ctx context.Context, detour N.Dialer) context.Context {
	if detour == nil {
		return ctx
	}
	return context.WithValue(ctx, detourVarKey{}, detour)
}

// DetourVarFromContext returns the detour var from the context.
func DetourVarFromContext(ctx context.Context) N.Dialer {
	value := ctx.Value(detourVarKey{})
	if value == nil {
		return nil
	}
	return value.(N.Dialer)
}

// NewDetourVar creates a new Dialer that uses the detour from the context.
// To set the detour, use service.ContextWith[DetourVar]().
func NewDetourVar() N.Dialer {
	return &detourVar{}
}

var _ N.Dialer = (*detourVar)(nil)

type detourVar struct{}

func (d *detourVar) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	detour := DetourVarFromContext(ctx)
	if detour == nil {
		return nil, E.New("not detour var available from context")
	}
	return detour.DialContext(ctx, network, destination)
}

func (d *detourVar) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	detour := DetourVarFromContext(ctx)
	if detour == nil {
		return nil, E.New("not detour var available from context")
	}
	return detour.ListenPacket(ctx, destination)
}
