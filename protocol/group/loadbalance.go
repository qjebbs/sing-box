package group

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/balancer"
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
	_ adapter.SimpleLifecycle         = (*LoadBalance)(nil)
	_ adapter.InterfaceUpdateListener = (*LoadBalance)(nil)
)

// LoadBalance is a load balance group
type LoadBalance struct {
	outbound.GroupAdapter
	*balancer.Balancer

	ctx            context.Context
	router         adapter.Router
	logger         log.ContextLogger
	outbound       adapter.OutboundManager
	provider       adapter.ProviderManager
	connection     adapter.ConnectionManager
	healthCheckMgr adapter.HealthCheckManager
	options        option.LoadBalanceOutboundOptions
	interruptGroup *interrupt.Group
}

// NewLoadBalance creates a new load balance outbound
func NewLoadBalance(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.LoadBalanceOutboundOptions) (adapter.Outbound, error) {
	return &LoadBalance{
		GroupAdapter: outbound.NewGroupAdapter(
			C.TypeLoadBalance, tag, []string{N.NetworkTCP, N.NetworkUDP},
			options.ProviderGroupCommonOption,
		),
		ctx:            ctx,
		router:         router,
		logger:         logger,
		outbound:       service.FromContext[adapter.OutboundManager](ctx),
		provider:       service.FromContext[adapter.ProviderManager](ctx),
		connection:     service.FromContext[adapter.ConnectionManager](ctx),
		healthCheckMgr: service.FromContext[adapter.HealthCheckManager](ctx),
		options:        options,
		interruptGroup: interrupt.NewGroup(),
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

	_, filtered := s.GetNodes()
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
			return s.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx), picked.Tag()), nil
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
			return s.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx), picked.Tag()), nil
		}
		lastErr = err
		s.logger.ErrorContext(ctx, err)
		s.ReportFailure(picked)
	}
	return nil, lastErr
}

// Close implements adapter.Service
func (s *LoadBalance) Close() error {
	if s.Balancer != nil {
		s.Balancer.HealthCheck.UnregisterPostCheckListener(s.interruptOutdatedConnections)
		s.Balancer.HealthCheck.RemoveProviders(s.Tag())
		return s.Balancer.Close()
	}
	return nil
}

// Start implements adapter.Service
func (s *LoadBalance) Start() error {
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	if s.options.HealthCheck == nil || s.options.HealthCheck.IsEmpty() {
		return E.New("loadbalance requires 'health_check' in options")
	}
	checker, err := s.healthCheckMgr.Get(s.ctx, s.logger, *s.options.HealthCheck)
	if err != nil {
		return err
	}
	checker.RegisterPostCheckListener(s.interruptOutdatedConnections)
	// Submit all providers to the shared checker.
	if err := checker.SetProviders(s.Tag(), s.Providers()); err != nil {
		return err
	}
	healthCheck, ok := checker.(*healthcheck.HealthCheck)
	if !ok {
		return E.New("health check is not a health check")
	}
	b, err := balancer.New(s.logger, &s.GroupAdapter, healthCheck, s.options.Pick)
	if err != nil {
		return err
	}
	s.Balancer = b
	return s.Balancer.Start()
}

func (s *LoadBalance) interruptOutdatedConnections() {
	s.interruptGroup.Interrupt(s.options.InterruptExistConnections, s.Balancer.AvailableNodes(s.options.LogHealth))
}

// URLTest implements adapter.URLTestGroup
func (s *LoadBalance) URLTest(ctx context.Context) (map[string]uint16, error) {
	return s.Balancer.HealthCheck.CheckAll(ctx, s.Tag())
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (s *LoadBalance) InterfaceUpdated(ctx context.Context) {
	// b can be nil if the parent struct has not initialized it yet.
	if s.Balancer == nil || s.Balancer.HealthCheck == nil {
		return
	}
	go s.Balancer.HealthCheck.CheckAll(ctx, s.Tag())
}
