package outbound

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/balancer"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var (
	_ adapter.Outbound           = (*LoadBalance)(nil)
	_ adapter.OutboundCheckGroup = (*LoadBalance)(nil)
	_ adapter.Service            = (*LoadBalance)(nil)
)

// LoadBalance is a load balance group
type LoadBalance struct {
	myOutboundGroupAdapter
	*balancer.Balancer

	options option.LoadBalanceOutboundOptions
}

// NewLoadBalance creates a new load balance outbound
func NewLoadBalance(router adapter.Router, logger log.ContextLogger, tag string, options option.LoadBalanceOutboundOptions) (*LoadBalance, error) {
	return &LoadBalance{
		myOutboundGroupAdapter: myOutboundGroupAdapter{
			myOutboundAdapter: myOutboundAdapter{
				protocol: C.TypeLoadBalance,
				router:   router,
				logger:   logger,
				tag:      tag,
			},
			options: options.GroupCommonOption,
		},
		options: options,
	}, nil
}

// Now implements adapter.OutboundGroup
func (s *LoadBalance) Now() string {
	picked := s.Pick(context.Background(), N.NetworkTCP, M.Socksaddr{})
	if picked == nil {
		return ""
	}
	return picked.Tag()
}

// All implements adapter.OutboundGroup
func (s *LoadBalance) All() []string {
	s.LogNodes()
	return s.myOutboundGroupAdapter.All()
}

// Network implements adapter.OutboundGroup
func (s *LoadBalance) Network() []string {
	return s.Balancer.Networks()
}

// DialContext implements adapter.Outbound
func (s *LoadBalance) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	var lastErr error
	maxRetry := 5
	for i := 0; i < maxRetry; i++ {
		picked := s.Pick(ctx, network, destination)
		if picked == nil {
			lastErr = E.New("no outbound available")
			break
		}
		conn, err := picked.DialContext(ctx, network, destination)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		s.logger.ErrorContext(ctx, err)
		s.ReportFailure(picked)
	}
	return nil, lastErr
}

// ListenPacket implements adapter.Outbound
func (s *LoadBalance) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	var lastErr error
	maxRetry := 5
	for i := 0; i < maxRetry; i++ {
		picked := s.Pick(ctx, N.NetworkUDP, destination)
		if picked == nil {
			lastErr = E.New("no outbound available")
			break
		}
		conn, err := picked.ListenPacket(ctx, destination)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		s.logger.ErrorContext(ctx, err)
		s.ReportFailure(picked)
	}
	return nil, lastErr
}

// NewConnection implements adapter.Outbound
func (s *LoadBalance) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	return NewConnection(ctx, s, conn, metadata)
}

// NewPacketConnection implements adapter.Outbound
func (s *LoadBalance) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	return NewPacketConnection(ctx, s, conn, metadata)
}

// Close implements adapter.Service
func (s *LoadBalance) Close() error {
	if s.Balancer == nil {
		return nil
	}
	return s.Balancer.Close()
}

// Start implements adapter.Service
func (s *LoadBalance) Start() error {
	if err := s.initProviders(); err != nil {
		return err
	}
	b, err := balancer.New(s.router, s.providers, s.providersByTag, &s.options, s.logger)
	if err != nil {
		return err
	}
	s.Balancer = b
	return s.Balancer.Start()
}
