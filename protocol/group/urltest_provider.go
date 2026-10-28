package group

import (
	"context"
	"io"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

func RegisterURLTestProvider(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeURLTest, NewURLTestProvider)
}

var (
	_ adapter.Outbound                   = (*URLTestProvider)(nil)
	_ adapter.URLTestGroup               = (*URLTestProvider)(nil)
	_ adapter.OutboundConnectionReporter = (*URLTestProvider)(nil)
	_ adapter.Referrer                   = (*URLTestProvider)(nil)
	_ adapter.InterfaceUpdateListener    = (*URLTestProvider)(nil)
	_ adapter.Lifecycle                  = (*URLTestProvider)(nil)
)

// URLTestProvider is the URL test group. It uses the outbound with the best score of
// the health check results, and only moves to another one when it is better by more
// than the tolerance.
type URLTestProvider struct {
	outbound.GroupAdapter
	*healthcheck.HealthCheck

	ctx            context.Context
	logger         log.ContextLogger
	outbound       adapter.OutboundManager
	provider       adapter.ProviderManager
	connection     adapter.ConnectionManager
	healthCheckMgr adapter.HealthCheckManager

	checker   *option.HealthCheckOptions
	tolerance float32
	history   *urltest.HistoryStorage

	selection *selection

	interruptGroup               *interrupt.Group
	interruptExternalConnections bool
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (adapter.Outbound, error) {
	tolerance, err := relativeTolerance(options.Tolerance)
	if err != nil {
		return nil, err
	}
	return &URLTestProvider{
		GroupAdapter:                 outbound.NewGroupAdapter(C.TypeURLTest, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.ProviderGroupCommonOption),
		ctx:                          ctx,
		logger:                       logger,
		outbound:                     service.FromContext[adapter.OutboundManager](ctx),
		connection:                   service.FromContext[adapter.ConnectionManager](ctx),
		provider:                     service.FromContext[adapter.ProviderManager](ctx),
		healthCheckMgr:               service.FromContext[adapter.HealthCheckManager](ctx),
		checker:                      options.HealthCheck,
		tolerance:                    tolerance,
		history:                      service.PtrFromContext[urltest.HistoryStorage](ctx),
		interruptGroup:               interrupt.NewGroup(),
		interruptExternalConnections: options.InterruptExistConnections,
	}, nil
}

func (s *URLTestProvider) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	scope.Add(s.Close)
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	healthCheck, err := s.AttachHealthCheck(
		s.ctx, s.logger, s.healthCheckMgr, s.checker, s.healthChecksUpdated,
	)
	if err != nil {
		return err
	}
	s.HealthCheck = healthCheck
	s.selection = newSelection(&s.GroupAdapter, healthCheck.Storage, s.tolerance)
	return nil
}

// Close implements the cleanup registered on the scope by Start.
func (s *URLTestProvider) Close() error {
	return s.DetachHealthCheck(s.HealthCheck, s.healthChecksUpdated)
}

func (s *URLTestProvider) healthChecksUpdated() {
	if s.selection == nil {
		// the listener is registered before the selection is built
		return
	}
	_, tcpChanged := s.selection.Reselect(N.NetworkTCP)
	_, udpChanged := s.selection.Reselect(N.NetworkUDP)
	if s.HealthCheck.IsActive(s.Tag()) {
		// An idle group is not in use, and its statistics only describe the past.
		s.selection.LogNodes(s.logger, N.NetworkTCP)
	}
	if !tcpChanged && !udpChanged {
		return
	}
	s.interruptGroup.Interrupt(s.interruptExternalConnections)
	if s.history != nil {
		s.history.NotifyUpdated()
	}
}

// touchHealthCheck keeps the health checks of this group running while it is in
// use, and catches up after it was idle.
func (s *URLTestProvider) touchHealthCheck() {
	if s.HealthCheck != nil {
		s.HealthCheck.Touch(s.Tag())
	}
}

// Selected implements adapter.OutboundGroup
func (s *URLTestProvider) Selected(network string) adapter.Outbound {
	if s.selection == nil {
		return nil
	}
	node := s.selection.Selected(network)
	if node == nil {
		return nil
	}
	return node.outbound
}

// References implements adapter.Referrer
func (s *URLTestProvider) References() []string {
	tcpOutbound := s.Selected(N.NetworkTCP)
	udpOutbound := s.Selected(N.NetworkUDP)
	var references []string
	if tcpOutbound != nil {
		references = append(references, tcpOutbound.Tag())
	}
	if udpOutbound != nil && udpOutbound != tcpOutbound {
		references = append(references, udpOutbound.Tag())
	}
	return references
}

// AttachConnection implements adapter.OutboundGroup
func (s *URLTestProvider) AttachConnection(closer io.Closer) (detach func()) {
	s.touchHealthCheck()
	return s.interruptGroup.Add(closer, true)
}

func (s *URLTestProvider) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	s.touchHealthCheck()
	if s.selection == nil {
		return nil, E.New("urltest group is not started")
	}
	return tryEach(ctx, s.logger,
		func() (*candidate, bool) {
			node := s.selection.Selected(network)
			return node, node != nil
		},
		s.reportConnection,
		func(node *candidate) (net.Conn, error) {
			return node.outbound.DialContext(ctx, network, destination)
		},
	)
}

func (s *URLTestProvider) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	s.touchHealthCheck()
	if s.selection == nil {
		return nil, E.New("urltest group is not started")
	}
	return tryEach(ctx, s.logger,
		func() (*candidate, bool) {
			node := s.selection.Selected(N.NetworkUDP)
			return node, node != nil
		},
		s.reportConnection,
		func(node *candidate) (net.PacketConn, error) {
			return node.outbound.ListenPacket(ctx, destination)
		},
	)
}

// URLTest implements adapter.URLTestGroup
func (s *URLTestProvider) URLTest(ctx context.Context) (map[string]uint16, error) {
	return s.HealthCheck.CheckAll(ctx, s.Tag())
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (s *URLTestProvider) InterfaceUpdated(ctx context.Context) {
	// s.HealthCheck can be nil if the parent struct has not initialized it yet.
	if s.HealthCheck == nil {
		return
	}
	go s.HealthCheck.CheckAll(ctx, s.Tag())
}

// reportConnection records the outcome of an attempt, so a failed node is scored and the
// next pick avoids it.
func (s *URLTestProvider) reportConnection(node *candidate, established bool) {
	s.selection.ReportConnection(node, established)
}

// ReportConnection implements adapter.OutboundConnectionReporter, for the connections the
// root router carries with this group.
func (s *URLTestProvider) ReportConnection(outbound adapter.Outbound, established bool) {
	if s.selection == nil {
		return
	}
	s.selection.ReportConnection(&candidate{
		outbound: outbound,
		tag:      healthCheckTag(outbound),
	}, established)
}
