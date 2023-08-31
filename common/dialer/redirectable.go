package dialer

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// chainRedirectContext is a context that instructs dialers to redirect to form a chain
type chainRedirectContext struct {
	chain   []adapter.Outbound
	current int
}

// WithChainRedirects attaches a context that instructs dialers to redirect to form a chain
func WithChainRedirects(ctx context.Context, chain []adapter.Outbound) context.Context {
	// TODO: attach chainRedirectContext multiple times could cause unpredictable behavior, check?
	return context.WithValue(ctx, (*chainRedirectContext)(nil), &chainRedirectContext{
		chain:   chain,
		current: 0,
	})
}

// ChainRedirectDialer is a dialer that can be redirected to form a chain with others,
// so it works only when all outbounds are built on it.
type ChainRedirectDialer struct {
	// tag is tag of the parent outbound of this dialer
	tag string
	// detourDialer is the dialer configured by DialerOptions.Detour,
	// which is used when no redirect is needed
	detourDialer N.Dialer
	// defaultDialer is used to override the detourDialer
	// when this dialer is the last one of the chain
	defaultDialer N.Dialer
}

// NewChainRedirectDialer returns a new ChainRedirectDialer.
func NewChainRedirectDialer(tag string, detourDialer, defaultDialer N.Dialer) *ChainRedirectDialer {
	return &ChainRedirectDialer{
		tag:           tag,
		detourDialer:  detourDialer,
		defaultDialer: defaultDialer,
	}
}

// DialContext implements N.Dialer.
func (d *ChainRedirectDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if dialer := d.dialerFromContext(ctx); dialer != nil {
		return dialer.DialContext(ctx, network, destination)
	}
	return d.detourDialer.DialContext(ctx, network, destination)
}

// ListenPacket implements N.Dialer.
func (d *ChainRedirectDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if dialer := d.dialerFromContext(ctx); dialer != nil {
		return dialer.ListenPacket(ctx, destination)
	}
	return d.detourDialer.ListenPacket(ctx, destination)
}

func (d *ChainRedirectDialer) dialerFromContext(ctx context.Context) N.Dialer {
	if d.tag == "" {
		return nil
	}
	v := ctx.Value((*chainRedirectContext)(nil))
	if v == nil {
		return nil
	}
	c := v.(*chainRedirectContext)
	if c.current >= len(c.chain) {
		return nil
	}
	if c.chain[c.current].Tag() != d.tag {
		return nil
	}
	c.current++
	if c.current == len(c.chain) {
		// this is the last node in the chain, use defaultDialer
		// no matter what the detourDialer is
		return d.defaultDialer
	}
	return c.chain[c.current]
}
