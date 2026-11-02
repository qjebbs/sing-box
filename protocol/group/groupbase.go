package group

import (
	"context"
	"io"
	"net"
	"slices"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/balancer"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

// groupBase is the plumbing a group that picks among its nodes shares: the providers it
// collects them from, the health check of those nodes, and the connections it carries.
// What the group does with the nodes is its selection, see balancer.Selection.
type groupBase struct {
	outbound.GroupAdapter
	*balancer.Balancer

	ctx            context.Context
	logger         log.ContextLogger
	outbound       adapter.OutboundManager
	provider       adapter.ProviderManager
	healthCheckMgr adapter.HealthCheckManager
	interruptGroup *interrupt.TaggedGroup
	history        *urltest.HistoryStorage

	interruptExternalConnections bool

	// lastReferenced records the last referenced node set, so references are only
	// published when it actually changes.
	lastReferenced common.TypedValue[[]string]
}

func newGroupBase(
	ctx context.Context, logger log.ContextLogger, typ string, tag string,
	groupOptions option.ProviderGroupCommonOption, interruptExternalConnections bool,
) groupBase {
	return groupBase{
		GroupAdapter: outbound.NewGroupAdapter(
			typ, tag, []string{N.NetworkTCP, N.NetworkUDP}, groupOptions,
		),
		ctx:                          ctx,
		logger:                       logger,
		outbound:                     service.FromContext[adapter.OutboundManager](ctx),
		provider:                     service.FromContext[adapter.ProviderManager](ctx),
		healthCheckMgr:               service.FromContext[adapter.HealthCheckManager](ctx),
		interruptGroup:               interrupt.NewTaggedGroup(),
		history:                      service.PtrFromContext[urltest.HistoryStorage](ctx),
		interruptExternalConnections: interruptExternalConnections,
	}
}

// start starts the group: it collects the providers, attaches the health check, and
// builds the balancer the group picks from, which newBalancer builds with the pick of
// the group.
func (s *groupBase) start(
	stage adapter.StartStage,
	scope *adapter.Scope,
	healthCheckOptions *option.HealthCheckOptions,
	newBalancer func(check *healthcheck.HealthCheck) (*balancer.Balancer, error),
) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	scope.Add(s.Close)
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	healthCheck, err := s.AttachHealthCheck(
		s.ctx, s.logger, s.healthCheckMgr, healthCheckOptions, s.healthChecksUpdated,
	)
	if err != nil {
		return err
	}
	b, err := newBalancer(healthCheck)
	if err != nil {
		return err
	}
	s.Balancer = b
	return s.Balancer.Start()
}

// Close releases the resources of the group, it is registered on the scope by start.
func (s *groupBase) Close() error {
	if s.Balancer == nil {
		return nil
	}
	detachErr := s.DetachHealthCheck(s.Balancer.HealthCheck, s.healthChecksUpdated)
	return E.Append(detachErr, s.Balancer.Close(), func(err error) error {
		return E.Cause(err, "close balancer")
	})
}

// Selected implements adapter.OutboundGroup
func (s *groupBase) Selected(network string) adapter.Outbound {
	if s.Balancer == nil {
		return nil
	}
	node := s.PickNode(context.Background(), network, M.Socksaddr{})
	if node == nil {
		return nil
	}
	return node.Outbound
}

// References implements adapter.Referrer
func (s *groupBase) References() []string {
	if s.Balancer == nil {
		return nil
	}
	// Every node that isn't dead may serve the next connection, so it may keep its
	// idle connections. The objective is deliberately not applied here: it depends
	// on pick thresholds, which would make the references flap on jitter.
	return s.Balancer.AliveTags()
}

// Network implements adapter.OutboundGroup
func (s *groupBase) Network() []string {
	return s.Balancer.Networks()
}

// AttachConnection implements adapter.OutboundGroup
func (s *groupBase) AttachConnection(closer io.Closer, outboundTag string) (detach func()) {
	s.touchHealthCheck()
	// The router knows which node the connection was resolved to, so the connection is
	// bound to it: only the node that leaves the selection is interrupted, and only
	// when the tolerance doesn't hold it, see RetainedNodes.
	return s.interruptGroup.Add(closer, true, outboundTag)
}

// touchHealthCheck keeps the health checks of this group running while it is in
// use, and catches up after it was idle.
func (s *groupBase) touchHealthCheck() {
	if s.Balancer != nil && s.Balancer.HealthCheck != nil {
		s.HealthCheck.Touch(s.Tag())
	}
}

// DialContext implements adapter.Outbound
func (s *groupBase) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	s.touchHealthCheck()
	return tryEach(ctx, s.logger,
		func() (*balancer.Node, bool) {
			node := s.PickNode(ctx, network, destination)
			return node, node != nil
		},
		s.reportConnectionFailure,
		func(node *balancer.Node) (net.Conn, error) {
			conn, err := node.DialContext(ctx, network, destination)
			if err != nil {
				return nil, err
			}
			return s.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx), node.Tag()), nil
		},
	)
}

// ListenPacket implements adapter.Outbound
func (s *groupBase) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	s.touchHealthCheck()
	return tryEach(ctx, s.logger,
		func() (*balancer.Node, bool) {
			node := s.PickNode(ctx, N.NetworkUDP, destination)
			return node, node != nil
		},
		s.reportConnectionFailure,
		func(node *balancer.Node) (net.PacketConn, error) {
			conn, err := node.ListenPacket(ctx, destination)
			if err != nil {
				return nil, err
			}
			return s.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx), node.Tag()), nil
		},
	)
}

func (s *groupBase) reportConnectionFailure(node *balancer.Node) {
	s.HealthCheck.ReportConnectionFailure(node.Outbound)
}

// ReportConnectionFailure implements adapter.OutboundFailureReporter, for the
// connections the root router carries with this group.
func (s *groupBase) ReportConnectionFailure(outbound adapter.Outbound) {
	if s.HealthCheck == nil {
		return
	}
	s.HealthCheck.ReportConnectionFailure(outbound)
}

func (s *groupBase) healthChecksUpdated() {
	if s.Balancer == nil {
		// the listener is registered before the balancer is built
		return
	}
	s.Balancer.Invalidate()
	if s.HealthCheck.IsActive(s.Tag()) {
		// An idle group is not in use, and its statistics only describe the past.
		s.Balancer.LogNodes()
	}
	available := s.Balancer.AvailableNodes()
	var retained []string
	if s.interruptExternalConnections {
		// The nodes that are in use and just left the selection keep their
		// connections, they are not closed by a difference that measurement noise can
		// explain. Without the option the external connections are kept anyway, and
		// the internal ones are interrupted whatever the retention says, so the
		// retention is not computed at all.
		retained = s.Balancer.RetainedNodes(s.interruptGroup.Tags())
	}
	s.interruptGroup.Interrupt(s.interruptExternalConnections, available, retained)
	referenced := s.Balancer.AliveTags()
	if previous := s.lastReferenced.Swap(referenced); !slices.Equal(previous, referenced) {
		// The nodes this group may use changed.
		if s.history != nil {
			s.history.NotifyUpdated()
		}
	}
}

// URLTest implements adapter.URLTestGroup
func (s *groupBase) URLTest(ctx context.Context) (map[string]uint16, error) {
	return s.Balancer.HealthCheck.CheckAll(ctx, s.Tag())
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (s *groupBase) InterfaceUpdated(ctx context.Context) {
	// b can be nil if the parent struct has not initialized it yet.
	if s.Balancer == nil || s.Balancer.HealthCheck == nil {
		return
	}
	go s.Balancer.HealthCheck.CheckAll(ctx, s.Tag())
}
