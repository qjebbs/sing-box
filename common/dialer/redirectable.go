package dialer

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// redirectDialerContext is a context that overrides dialer.
type redirectDialerContext struct{}

// WithRedirectDialer attaches a redirecting dialer to context.
func WithRedirectDialer(ctx context.Context, dialer N.Dialer) context.Context {
	return context.WithValue(ctx, (*redirectDialerContext)(nil), dialer)
}

// RedirectDialerFrom returns the redirecting dialer from context.
func RedirectDialerFrom(ctx context.Context) N.Dialer {
	dialer := ctx.Value((*redirectDialerContext)(nil))
	if dialer == nil {
		return nil
	}
	return dialer.(N.Dialer)
}

// RedirectableDialer is a dialer that can be redirected.
type RedirectableDialer struct {
	dialer N.Dialer
	name   string
}

// NewRedirectable returns a redirectable dialer.
func NewRedirectable(router adapter.Router, dialer N.Dialer) *RedirectableDialer {
	return &RedirectableDialer{
		dialer: dialer,
	}
}

// DialContext implements N.Dialer.
func (d *RedirectableDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if dialer := RedirectDialerFrom(ctx); dialer != nil {
		return dialer.DialContext(ctx, network, destination)
	}
	return d.dialer.DialContext(ctx, network, destination)
}

// ListenPacket implements N.Dialer.
func (d *RedirectableDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if dialer := RedirectDialerFrom(ctx); dialer != nil {
		return dialer.ListenPacket(ctx, destination)
	}
	return d.dialer.ListenPacket(ctx, destination)
}
