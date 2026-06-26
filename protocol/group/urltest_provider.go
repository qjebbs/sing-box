package group

import (
	"context"
	"net"
	"sort"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/service/healthcheck"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

func RegisterURLTestProvider(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeURLTest, NewURLTestProvider)
}

var (
	_ adapter.Outbound                = (*URLTestProvider)(nil)
	_ adapter.URLTestGroup            = (*URLTestProvider)(nil)
	_ adapter.InterfaceUpdateListener = (*URLTestProvider)(nil)
	_ adapter.DirectRouteOutbound     = (*URLTestProvider)(nil)
)

type URLTestProvider struct {
	outbound.GroupAdapter
	*healthcheck.HealthCheck

	ctx        context.Context
	router     adapter.Router
	logger     log.ContextLogger
	outbound   adapter.OutboundManager
	provider   adapter.ProviderManager
	connection adapter.ConnectionManager
	serviceMgr adapter.ServiceManager

	checker   string
	tolerance healthcheck.RTT
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (adapter.Outbound, error) {
	tolerance := healthcheck.RTT(options.Tolerance)
	if tolerance == 0 {
		tolerance = 50
	}
	return &URLTestProvider{
		GroupAdapter: outbound.NewGroupAdapter(C.TypeURLTest, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.ProviderGroupCommonOption),
		ctx:          ctx,
		router:       router,
		logger:       logger,
		outbound:     service.FromContext[adapter.OutboundManager](ctx),
		connection:   service.FromContext[adapter.ConnectionManager](ctx),
		provider:     service.FromContext[adapter.ProviderManager](ctx),
		serviceMgr:   service.FromContext[adapter.ServiceManager](ctx),
		checker:      options.Checker,
		tolerance:    tolerance,
	}, nil
}

func (s *URLTestProvider) Start() error {
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	if s.checker == "" {
		return E.New("urltest requires a checker service, set 'checker' in options")
	}
	svc, ok := s.serviceMgr.Get(s.checker)
	if !ok {
		return E.New("health checker service not found: ", s.checker)
	}
	checker, ok := svc.(*healthcheck.Service)
	if !ok {
		return E.New("service [", s.checker, "] is not a health checker service")
	}
	if err := checker.HealthCheck.SetProviders(s.Tag(), s.Providers()); err != nil {
		return err
	}
	s.HealthCheck = checker.HealthCheck
	return nil
}

func (s URLTestProvider) Close() error {
	s.HealthCheck.RemoveProviders(s.Tag())
	if s.HealthCheck == nil {
		return nil
	}
	return nil
}

func (s *URLTestProvider) Now() string {
	outbound, err := s.Select(N.NetworkTCP)
	if err != nil {
		return ""
	}
	return outbound.Tag()
}

func (s *URLTestProvider) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	outbound, err := s.Select(network)
	if err != nil {
		return nil, err
	}
	conn, err := outbound.DialContext(ctx, network, destination)
	if err == nil {
		return conn, nil
	}
	s.logger.ErrorContext(ctx, err)
	s.HealthCheck.ReportFailure(outbound)
	outbounds := s.Fallback(outbound)
	for _, fallback := range outbounds {
		conn, err = fallback.DialContext(ctx, network, destination)
		if err == nil {
			return conn, nil
		}
		s.logger.ErrorContext(ctx, err)
		s.HealthCheck.ReportFailure(fallback)
	}
	return nil, err
}

func (s *URLTestProvider) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	outbound, err := s.Select(N.NetworkUDP)
	if err != nil {
		return nil, err
	}
	conn, err := outbound.ListenPacket(ctx, destination)
	if err == nil {
		return conn, nil
	}
	s.logger.ErrorContext(ctx, err)
	s.HealthCheck.ReportFailure(outbound)
	outbounds := s.Fallback(outbound)
	for _, fallback := range outbounds {
		conn, err = fallback.ListenPacket(ctx, destination)
		if err == nil {
			return conn, nil
		}
		s.logger.ErrorContext(ctx, err)
		s.HealthCheck.ReportFailure(fallback)
	}
	return nil, err
}

func (s *URLTestProvider) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	s.connection.NewConnection(ctx, s, conn, metadata, onClose)
}

func (s *URLTestProvider) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	s.connection.NewPacketConnection(ctx, s, conn, metadata, onClose)
}

// NewDirectRouteConnection implements adapter.DirectRouteOutbound
func (s *URLTestProvider) NewDirectRouteConnection(metadata adapter.InboundContext, routeContext tun.DirectRouteContext, timeout time.Duration) (tun.DirectRouteDestination, error) {
	selected, err := s.Select(metadata.Network)
	if err != nil {
		return nil, err
	}
	// may select the first available outbound for default,
	// need to check if the network is supported
	if !common.Contains(selected.Network(), metadata.Network) {
		return nil, E.New(metadata.Network, " is not supported by outbound: ", selected.Tag())
	}
	dro, ok := selected.(adapter.DirectRouteOutbound)
	if !ok {
		return nil, E.New("outbound does not support direct route: ", selected.Tag())
	}
	return dro.NewDirectRouteConnection(metadata, routeContext, timeout)
}

func (s *URLTestProvider) Select(network string) (adapter.Outbound, error) {
	var minDelay healthcheck.RTT
	var minOutbound adapter.Outbound
	var firstOutbound adapter.Outbound
	for _, provider := range s.provider.Providers() {
		for _, detour := range provider.Outbounds() {
			if !common.Contains(detour.Network(), network) {
				continue
			}
			if firstOutbound == nil {
				firstOutbound = detour
			}
			history := s.getHistory(detour)
			if history == nil || history.Delay == healthcheck.Failed {
				continue
			}
			if minDelay == 0 || minDelay > history.Delay+s.tolerance {
				minDelay = history.Delay
				minOutbound = detour
			}
		}
	}
	if minOutbound != nil {
		return minOutbound, nil
	}
	if firstOutbound != nil {
		return firstOutbound, nil
	}
	return nil, E.New("[", s.Tag(), "]: no outbounds available")
}

func (s *URLTestProvider) Fallback(used adapter.Outbound) []adapter.Outbound {
	outbounds := make([]adapter.Outbound, 0)
	for _, provider := range s.provider.Providers() {
		for _, detour := range provider.Outbounds() {
			if detour == used {
				continue
			}
			outbounds = append(outbounds, detour)
		}
	}
	sort.Slice(outbounds, func(i, j int) bool {
		hi := s.getHistory(outbounds[i])
		hj := s.getHistory(outbounds[j])
		if hi == nil || hi.Delay == healthcheck.Failed {
			return false
		}
		if hj == nil || hi.Delay == healthcheck.Failed {
			return false
		}
		return hi.Delay < hj.Delay
	})
	return outbounds
}

func (s *URLTestProvider) getHistory(outbound adapter.Outbound) *healthcheck.History {
	if group, ok := outbound.(adapter.OutboundGroup); ok {
		real, err := adapter.RealOutbound(group)
		if err != nil {
			return nil
		}
		outbound = real
	}
	return s.HealthCheck.Storage.Latest(outbound.Tag())
}

// URLTest implements adapter.OutboundCheckGroup
func (s *URLTestProvider) URLTest(ctx context.Context) (map[string]uint16, error) {
	return s.HealthCheck.CheckAll(ctx, s.Tag())
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (s *URLTestProvider) InterfaceUpdated() {
	// b can be nil if the parent struct has not initialized it yet.
	if s.HealthCheck == nil {
		return
	}
	go s.HealthCheck.CheckAll(context.Background(), s.Tag())
}
