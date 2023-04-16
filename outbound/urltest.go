package outbound

import (
	"context"
	"net"
	"sort"
	"time"

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
	_ adapter.Outbound           = (*URLTest)(nil)
	_ adapter.OutboundCheckGroup = (*URLTest)(nil)
)

type URLTest struct {
	myOutboundGroupAdapter
	*healthcheck.HealthCheck

	options   option.HealthCheckOptions
	tolerance healthcheck.RTT
}

func NewURLTest(router adapter.Router, logger log.ContextLogger, tag string, options option.URLTestOutboundOptions) (*URLTest, error) {
	link := options.URL
	interval := options.Interval
	tolerance := healthcheck.RTT(options.Tolerance)
	if link == "" {
		//goland:noinspection HttpUrlsUsage
		link = "http://www.gstatic.com/generate_204"
	}
	if interval == 0 {
		interval = option.Duration(C.DefaultURLTestInterval)
	}
	if tolerance == 0 {
		tolerance = 50
	}
	outbound := &URLTest{
		myOutboundGroupAdapter: myOutboundGroupAdapter{
			myOutboundAdapter: myOutboundAdapter{
				protocol: C.TypeURLTest,
				router:   router,
				logger:   logger,
				tag:      tag,
			},
			options: options.GroupCommonOption,
		},
		options: option.HealthCheckOptions{
			Sampling:    1,
			Interval:    interval,
			Destination: link,
		},
		tolerance: tolerance,
	}
	return outbound, nil
}

func (s *URLTest) Network() []string {
	if s == nil {
		return []string{N.NetworkTCP, N.NetworkUDP}
	}
	outbound, err := s.Select(N.NetworkTCP)
	if err != nil {
		return []string{N.NetworkTCP, N.NetworkUDP}
	}
	return outbound.Network()
}

func (s *URLTest) Start() error {
	if err := s.initProviders(); err != nil {
		return err
	}
	s.HealthCheck = healthcheck.New(s.router, s.providers, s.providersByTag, &s.options, s.logger)
	return s.HealthCheck.Start()
}

func (s URLTest) Close() error {
	if s.HealthCheck == nil {
		return nil
	}
	return s.HealthCheck.Close()
}

func (s *URLTest) Now() string {
	outbound, err := s.Select(N.NetworkTCP)
	if err != nil {
		return ""
	}
	return outbound.Tag()
}

func (s *URLTest) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
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

func (s *URLTest) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
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

func (s *URLTest) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	return NewConnection(ctx, s, conn, metadata)
}

func (s *URLTest) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	return NewPacketConnection(ctx, s, conn, metadata)
}

func (s *URLTest) Select(network string) (adapter.Outbound, error) {
	var minDelay healthcheck.RTT
	var minTime time.Time
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
			if minDelay == 0 || minDelay > history.Delay+s.tolerance || minDelay > history.Delay-s.tolerance && minTime.Before(history.Time) {
				minDelay = history.Delay
				minTime = history.Time
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

func (s *URLTest) Fallback(used adapter.Outbound) []adapter.Outbound {
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

func (s *URLTest) getHistory(outbound adapter.Outbound) *healthcheck.History {
	if group, ok := outbound.(adapter.OutboundGroup); ok {
		real, err := adapter.RealOutbound(s.router, group)
		if err != nil {
			return nil
		}
		outbound = real
	}
	return s.HealthCheck.Storage.Latest(outbound.Tag())
}
