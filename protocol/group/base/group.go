// Package base is the base of the groups that pick among their nodes: it collects the
// nodes of the group, checks their health, ranks them, caches the ranking, and carries
// the connections. What the group picks among them is a Selection.
package base

import (
	"context"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

var _ adapter.InterfaceUpdateListener = (*Group)(nil)

// Group is the base of a group that picks among its nodes: it collects the nodes from
// the providers of the group, checks their health, keeps them ranked, and carries the
// connections with the node its selection returns.
//
// The group itself decides nothing about the pick: the selection it is started with is
// the only difference between a load balance group and a url test group.
type Group struct {
	outbound.GroupAdapter
	*healthcheck.HealthCheck

	ctx            context.Context
	logger         log.ContextLogger
	outbound       adapter.OutboundManager
	provider       adapter.ProviderManager
	healthCheckMgr adapter.HealthCheckManager
	interruptGroup *interrupt.TaggedGroup
	history        *urltest.HistoryStorage
	lastReferenced common.TypedValue[[]string]

	interruptExternalConnections bool

	pick  selection.Selection
	nodes *selection.NodeBuilder

	networks []string

	// snapshotAccess guards the cached ranking of the nodes, so a pick does not
	// rebuild and re-sort every node of the group for every connection it carries.
	snapshotAccess sync.RWMutex
	snapshot       map[string]*nodeSnapshot
	// generation is bumped whenever the results that the ranking reads are updated,
	// see Invalidate, so a cached ranking is only reused while it describes them.
	generation atomic.Uint64
}

// NewGroup creates the base of a group with the given type and tag. The connection
// managers and the health check manager are resolved from the context, and used when
// the group is started.
func NewGroup(
	ctx context.Context, logger log.ContextLogger, typ string, tag string,
	groupOptions option.ProviderGroupCommonOption, interruptExternalConnections bool,
) *Group {
	return &Group{
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

// Start starts the group: it collects the providers, attaches the health check, and
// starts the checks of the nodes. The selection is how the group turns the ranked nodes
// into the node that serves a connection, and nodeOptions describe how the health of a
// node becomes its status.
func (g *Group) Start(
	stage adapter.StartStage,
	scope *adapter.Scope,
	healthCheckOptions *option.HealthCheckOptions,
	pick selection.Selection,
	nodeOptions selection.NodeOptions,
) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	scope.Add(g.Close)
	if err := g.InitProviders(g.outbound, g.provider); err != nil {
		return err
	}
	healthCheck, err := g.AttachHealthCheck(
		g.ctx, g.logger, g.healthCheckMgr, healthCheckOptions, g.healthChecksUpdated,
	)
	if err != nil {
		return err
	}
	nodes, err := selection.NewNodeBuilder(healthCheck.Storage.Cap(), nodeOptions)
	if err != nil {
		return err
	}
	g.HealthCheck = healthCheck
	g.nodes = nodes
	// The pick is set last, so a check that arrives while the group is starting finds
	// it missing and stops, see healthChecksUpdated.
	g.pick = pick
	return healthCheck.Start()
}

// Close releases the resources of the group, it is registered on the scope by Start.
func (g *Group) Close() error {
	if g.HealthCheck == nil {
		return nil
	}
	detachErr := g.DetachHealthCheck(g.HealthCheck, g.healthChecksUpdated)
	return E.Append(detachErr, g.HealthCheck.Close(), func(err error) error {
		return E.Cause(err, "close health check")
	})
}

// Selected implements adapter.OutboundGroup
func (g *Group) Selected(network string) adapter.Outbound {
	if g.HealthCheck == nil {
		return nil
	}
	node := g.PickNode(context.Background(), network, M.Socksaddr{})
	if node == nil {
		return nil
	}
	return node.Outbound
}

// References implements adapter.Referrer
func (g *Group) References() []string {
	if g.HealthCheck == nil {
		return nil
	}
	// Every node that isn't dead may serve the next connection, so it may keep its
	// idle connections. The objective is deliberately not applied here: it depends
	// on pick thresholds, which would make the references flap on jitter.
	return g.AliveTags()
}

// Network implements adapter.OutboundGroup
func (g *Group) Network() []string {
	return g.Networks()
}

// AttachConnection implements adapter.OutboundGroup
func (g *Group) AttachConnection(closer io.Closer, outboundTag string) (detach func()) {
	g.touchHealthCheck()
	// The router knows which node the connection was resolved to, so the connection is
	// bound to it: only the node that leaves the selection is interrupted, and only
	// when the tolerance doesn't hold it, see RetainedNodes.
	return g.interruptGroup.Add(closer, true, outboundTag)
}

// touchHealthCheck keeps the health checks of this group running while it is in
// use, and catches up after it was idle.
func (g *Group) touchHealthCheck() {
	if g.HealthCheck != nil {
		g.HealthCheck.Touch(g.Tag())
	}
}

// DialContext implements adapter.Outbound
func (g *Group) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	g.touchHealthCheck()
	return tryEach(ctx, g.logger,
		func() (*selection.Node, bool) {
			node := g.PickNode(ctx, network, destination)
			return node, node != nil
		},
		g.reportConnectionFailure,
		func(node *selection.Node) (net.Conn, error) {
			conn, err := node.DialContext(ctx, network, destination)
			if err != nil {
				return nil, err
			}
			return g.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx), node.Tag()), nil
		},
	)
}

// ListenPacket implements adapter.Outbound
func (g *Group) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	g.touchHealthCheck()
	return tryEach(ctx, g.logger,
		func() (*selection.Node, bool) {
			node := g.PickNode(ctx, N.NetworkUDP, destination)
			return node, node != nil
		},
		g.reportConnectionFailure,
		func(node *selection.Node) (net.PacketConn, error) {
			conn, err := node.ListenPacket(ctx, destination)
			if err != nil {
				return nil, err
			}
			return g.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx), node.Tag()), nil
		},
	)
}

func (g *Group) reportConnectionFailure(node *selection.Node) {
	g.HealthCheck.ReportConnectionFailure(node.Outbound)
}

// ReportConnectionFailure implements adapter.OutboundFailureReporter, for the
// connections the root router carries with this group.
func (g *Group) ReportConnectionFailure(outbound adapter.Outbound) {
	if g.HealthCheck == nil {
		return
	}
	g.HealthCheck.ReportConnectionFailure(outbound)
}

func (g *Group) healthChecksUpdated() {
	if g.pick == nil {
		// the listener is registered before the group is started
		return
	}
	g.Invalidate()
	if g.HealthCheck.IsActive(g.Tag()) {
		// An idle group is not in use, and its statistics only describe the past.
		g.LogNodes()
	}
	available := g.AvailableNodes()
	var retained []string
	if g.interruptExternalConnections {
		// The nodes that are in use and just left the selection keep their
		// connections, they are not closed by a difference that measurement noise can
		// explain. Without the option the external connections are kept anyway, and
		// the internal ones are interrupted whatever the retention says, so the
		// retention is not computed at all.
		retained = g.RetainedNodes(g.interruptGroup.Tags())
	}
	g.interruptGroup.Interrupt(g.interruptExternalConnections, available, retained)
	referenced := g.AliveTags()
	if previous := g.lastReferenced.Swap(referenced); !slices.Equal(previous, referenced) {
		// The nodes this group may use changed.
		if g.history != nil {
			g.history.NotifyUpdated()
		}
	}
}

// URLTest implements adapter.URLTestGroup
func (g *Group) URLTest(ctx context.Context) (map[string]uint16, error) {
	return g.HealthCheck.CheckAll(ctx, g.Tag())
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (g *Group) InterfaceUpdated(ctx context.Context) {
	if g.HealthCheck == nil {
		return
	}
	go g.HealthCheck.CheckAll(ctx, g.Tag())
}
