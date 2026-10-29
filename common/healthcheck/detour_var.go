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

// sharedDetourVar is the innermost detour of every detour_of chain. It carries no state: the
// outbound it stands for is taken from the context of each check, so every checker can share one.
var sharedDetourVar N.Dialer = &detourVar{}

var _ N.Dialer = (*detourVar)(nil)

// detourVar stands for the outbound a check carries, which is only known at dial time. It is
// handed to CreateDetour as the innermost detour and resolves the outbound from the context of
// the dial, see contextWithDetourVar.
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
