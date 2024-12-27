package group

import (
	"context"
	"net"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ N.Dialer = (*errDailer)(nil)

type errDailer struct {
	err error
}

func newErrDailer(err error) *errDailer {
	return &errDailer{err: err}
}

func (d *errDailer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return nil, d.err
}

func (d *errDailer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, d.err
}
