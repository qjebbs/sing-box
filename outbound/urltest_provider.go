package outbound

import (
	"context"
	"net"
	"sort"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var (
	_ adapter.Outbound                = (*URLTestProvider)(nil)
	_ adapter.OutboundCheckGroup      = (*URLTestProvider)(nil)
	_ adapter.InterfaceUpdateListener = (*URLTestProvider)(nil)
)

type URLTestProvider struct {
	myOutboundGroupAdapter
	*healthcheck.HealthCheck

	ctx       context.Context
	options   option.HealthCheckOptions
	tolerance healthcheck.RTT
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (*URLTestProvider, error) {
	link := options.URL
	interval := options.Interval
	tolerance := healthcheck.RTT(options.Tolerance)
	if link == "" {
		link = "https://www.gstatic.com/generate_204"
	}
	if interval == 0 {
		interval = option.Duration(C.DefaultURLTestInterval)
	}
	if tolerance == 0 {
		tolerance = 50
	}
	outbound := &URLTestProvider{
		myOutboundGroupAdapter: myOutboundGroupAdapter{
			myOutboundAdapter: myOutboundAdapter{
				protocol:     C.TypeURLTest,
				network:      []string{N.NetworkTCP, N.NetworkUDP},
				router:       router,
				logger:       logger,
				tag:          tag,
				dependencies: options.Outbounds,
			},
			options: options.ProviderGroupCommonOption,
		},
		ctx: ctx,
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
	if err := s.initProviders(); err != nil {
		return err
	}
	s.HealthCheck = healthcheck.New(s.ctx, s.router, s.providers, s.providersByTag, &s.options, s.logger)
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

func (s *URLTestProvider) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	return NewConnection(ctx, s, conn, metadata)
}

func (s *URLTestProvider) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	return NewPacketConnection(ctx, s, conn, metadata)
}

func (s *URLTestProvider) Select(network string) (adapter.Outbound, error) {
	var minDelay healthcheck.RTT
	var minOutbound adapter.Outbound
	var firstOutbound adapter.Outbound
	for _, provider := range s.providers {
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
	return nil, E.New("[", s.tag, "]: no outbounds available")
}

func (s *URLTestProvider) Fallback(used adapter.Outbound) []adapter.Outbound {
	outbounds := make([]adapter.Outbound, 0)
	for _, provider := range s.providers {
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
