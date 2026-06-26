package group

import (
	"context"
	"net"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/balancer"
	"github.com/sagernet/sing-box/service/healthcheck"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

func RegisterLoadBalance(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeLoadBalance, NewLoadBalance)
}

var (
	_ adapter.Outbound                = (*LoadBalance)(nil)
	_ adapter.URLTestGroup            = (*LoadBalance)(nil)
	_ adapter.DirectRouteOutbound     = (*LoadBalance)(nil)
	_ adapter.SimpleLifecycle         = (*LoadBalance)(nil)
	_ adapter.InterfaceUpdateListener = (*LoadBalance)(nil)
)

// LoadBalance is a load balance group
type LoadBalance struct {
	outbound.GroupAdapter
	*balancer.Balancer

	ctx        context.Context
	router     adapter.Router
	logger     log.ContextLogger
	outbound   adapter.OutboundManager
	provider   adapter.ProviderManager
	connection adapter.ConnectionManager
	serviceMgr adapter.ServiceManager
	options    option.LoadBalanceOutboundOptions
}

// NewLoadBalance creates a new load balance outbound
func NewLoadBalance(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.LoadBalanceOutboundOptions) (adapter.Outbound, error) {
	return &LoadBalance{
		GroupAdapter: outbound.NewGroupAdapter(
			C.TypeLoadBalance, tag, []string{N.NetworkTCP, N.NetworkUDP},
			options.ProviderGroupCommonOption,
		),
		ctx:        ctx,
		router:     router,
		logger:     logger,
		outbound:   service.FromContext[adapter.OutboundManager](ctx),
		provider:   service.FromContext[adapter.ProviderManager](ctx),
		connection: service.FromContext[adapter.ConnectionManager](ctx),
		serviceMgr: service.FromContext[adapter.ServiceManager](ctx),
		options:    options,
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
	// s.LogNodes()
	// return s.GroupAdapter.All()

	_, filtered := s.GetNodes(true)
	return common.Map(filtered, func(node *balancer.Node) string {
		return node.Tag()
	})
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

// NewConnectionEx implements adapter.TCPInjectableInbound
func (s *LoadBalance) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	selected := s.Pick(ctx, N.NetworkUDP, metadata.Destination)
	if selected == nil {
		s.connection.NewConnection(ctx, newErrDailer(E.New("no outbound available")), conn, metadata, onClose)
		return
	}
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	if outboundHandler, isHandler := selected.(adapter.ConnectionHandlerEx); isHandler {
		outboundHandler.NewConnectionEx(ctx, conn, metadata, onClose)
	} else {
		s.connection.NewConnection(ctx, selected, conn, metadata, onClose)
	}
}

// NewPacketConnectionEx implements adapter.UDPInjectableInbound
func (s *LoadBalance) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	selected := s.Pick(ctx, N.NetworkUDP, metadata.Destination)
	if selected == nil {
		s.connection.NewPacketConnection(ctx, newErrDailer(E.New("no outbound available")), conn, metadata, onClose)
		return
	}
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	if outboundHandler, isHandler := selected.(adapter.PacketConnectionHandlerEx); isHandler {
		outboundHandler.NewPacketConnectionEx(ctx, conn, metadata, onClose)
	} else {
		s.connection.NewPacketConnection(ctx, selected, conn, metadata, onClose)
	}
}

// NewDirectRouteConnection implements adapter.DirectRouteOutbound
func (s *LoadBalance) NewDirectRouteConnection(metadata adapter.InboundContext, routeContext tun.DirectRouteContext, timeout time.Duration) (tun.DirectRouteDestination, error) {
	ctx := adapter.WithContext(context.Background(), &metadata)
	destination := metadata.Destination
	picked := s.Pick(ctx, N.NetworkICMP, destination)
	if picked == nil {
		return nil, E.New("no outbound available for network: ", metadata.Network)
	}
	if !common.Contains(picked.Network(), metadata.Network) {
		return nil, E.New(metadata.Network, " is not supported by outbound: ", picked.Tag())
	}
	dro, ok := picked.(adapter.DirectRouteOutbound)
	if !ok {
		return nil, E.New("outbound does not support direct route: ", picked.Tag())
	}
	return dro.NewDirectRouteConnection(metadata, routeContext, timeout)
}

// Close implements adapter.Service
func (s *LoadBalance) Close() error {
	s.HealthCheck.RemoveProviders(s.Tag())
	if s.Balancer != nil {
		return s.Balancer.Close()
	}
	return nil
}

// Start implements adapter.Service
func (s *LoadBalance) Start() error {
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	if s.options.Checker == "" {
		s.options.Checker = healthcheck.DefaultServiceTag
		// return E.New("loadbalance requires a checker service, set 'checker' in options")
	}
	svc, ok := s.serviceMgr.Get(C.TypeHealthChecker, s.options.Checker)
	if !ok {
		return E.New("health checker service not found: ", s.options.Checker)
	}
	checker, ok := svc.(*healthcheck.Service)
	if !ok {
		return E.New("service [", s.options.Checker, "] is not a health checker service")
	}
	// Submit all providers to the shared checker.
	if err := checker.HealthCheck.SetProviders(s.Tag(), s.Providers()); err != nil {
		return err
	}
	b, err := balancer.New(s.logger, &s.GroupAdapter, checker.HealthCheck, s.options.Pick)
	if err != nil {
		return err
	}
	s.Balancer = b
	return s.Balancer.Start()
}

// URLTest implements adapter.OutboundCheckGroup
func (s *LoadBalance) URLTest(ctx context.Context) (map[string]uint16, error) {
	return s.Balancer.HealthCheck.CheckAll(ctx, s.Tag())
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (s *LoadBalance) InterfaceUpdated() {
	// b can be nil if the parent struct has not initialized it yet.
	if s.Balancer == nil || s.Balancer.HealthCheck == nil {
		return
	}
	go s.Balancer.HealthCheck.CheckAll(context.Background(), s.Tag())
}
