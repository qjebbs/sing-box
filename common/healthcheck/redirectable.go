package healthcheck

import (
	"context"
	"errors"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func checkRedirectable(outbound adapter.Outbound) bool {
	t := &testDialer{}
	ctx := context.Background()
	ctx = dialer.WithRedirectDialer(ctx, t)
	conn, err := outbound.DialContext(ctx, N.NetworkUDP, M.Socksaddr{})
	if err == nil {
		conn.Close()
	}
	return t.called
}

type testDialer struct {
	called bool
}

func (d *testDialer) DialContext(ctx context.Context, network string, address M.Socksaddr) (net.Conn, error) {
	d.called = true
	return nil, errors.New("no connection")
}

func (d *testDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	d.called = true
	return nil, errors.New("no connection")
}
