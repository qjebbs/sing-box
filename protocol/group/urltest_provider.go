package group

import (
	"context"
	"net"
	"sort"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

func RegisterURLTestProvider(registry *outbound.Registry) {
	outbound.Register[option.ProviderURLTestOptions](registry, C.TypeURLTest, NewURLTestProvider)
}

var (
	_ adapter.Outbound                = (*URLTestProvider)(nil)
	_ adapter.OutboundCheckGroup      = (*URLTestProvider)(nil)
	_ adapter.InterfaceUpdateListener = (*URLTestProvider)(nil)
)

type URLTestProvider struct {
	outbound.GroupAdapter
	*healthcheck.HealthCheck

	ctx        context.Context
	logger     log.ContextLogger
	outbound   adapter.OutboundManager
	provider   adapter.ProviderManager
	connection adapter.ConnectionManager

	options   option.HealthCheckOptions
	tolerance healthcheck.RTT
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (adapter.Outbound, error) {
	link := options.URL
	interval := options.Interval
	tolerance := healthcheck.RTT(options.Tolerance)
	if link == "" {
		link = "https://www.gstatic.com/generate_204"
	}
	if interval == 0 {
		interval = badoption.Duration(C.DefaultURLTestInterval)
	}
	if tolerance == 0 {
		tolerance = 50
	}
	outbound := &URLTestProvider{
		GroupAdapter: outbound.NewGroupAdapter(C.TypeURLTest, tag, []string{N.NetworkTCP, N.NetworkUDP}, router, options.ProviderGroupCommonOption),
		ctx:          ctx,
		logger:       logger,
		outbound:     service.FromContext[adapter.OutboundManager](ctx),
		connection:   service.FromContext[adapter.ConnectionManager](ctx),
		provider:     service.FromContext[adapter.ProviderManager](ctx),
		options: option.HealthCheckOptions{
			Sampling:    1,
			Interval:    interval,
			Destination: link,
		},
		tolerance: tolerance,
	}
	return outbound, nil
}

func (s *URLTestProvider) Start() error {
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	s.HealthCheck = healthcheck.New(s.ctx, s.outbound, s.Providers(), &s.options, s.logger)
	return s.HealthCheck.Start()
}

func (s URLTestProvider) Close() error {
	if s.HealthCheck == nil {
		return nil
	}
	return s.HealthCheck.Close()
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
