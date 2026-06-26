package healthcheck

import (
	"context"
	"net"

	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type detourVarKey struct{}

// contextWithDetourVar returns a new context with the detour var.
func contextWithDetourVar(ctx context.Context, detour N.Dialer) context.Context {
	if detour == nil {
		return ctx
	}
	return context.WithValue(ctx, detourVarKey{}, detour)
}

// detourVarFromContext returns the detour var from the context.
func detourVarFromContext(ctx context.Context) N.Dialer {
	value := ctx.Value(detourVarKey{})
	if value == nil {
		return nil
	}
	return value.(N.Dialer)
}

// newDetourVar creates a new Dialer that uses the detour from the context.
// To set the detour, use service.ContextWith[DetourVar]().
func newDetourVar() N.Dialer {
	return &detourVar{}
}

var _ N.Dialer = (*detourVar)(nil)

type detourVar struct{}

func (d *detourVar) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	detour := detourVarFromContext(ctx)
	if detour == nil {
		return nil, E.New("not detour var available from context")
	}
	return detour.DialContext(ctx, network, destination)
}

func (d *detourVar) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	detour := detourVarFromContext(ctx)
	if detour == nil {
		return nil, E.New("not detour var available from context")
	}
	return detour.ListenPacket(ctx, destination)
}
