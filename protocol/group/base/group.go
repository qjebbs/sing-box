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

var _ adapter.OutboundPicker = (*Group)(nil)

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
	// attachAccess serializes binding a connection to a node with the round of checks
	// that forgets the nodes that left the selection, so a connection is not bound to
	// a node that is already gone.
	attachAccess sync.Mutex
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
	idleProbe bool,
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
	healthCheck.SetIdleProbe(g.Tag(), idleProbe)
	nodes, err := selection.NewNodeBuilder(nodeOptions)
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

// Selected implements adapter.OutboundGroup.
//
// It is the node the group serves from, not the node that serves this connection: a
// pick depends on the connection, so it is made by PickOutbound when the connection is
// resolved, and asking the group which node it uses neither moves the balance nor
// answers for a connection it will not carry.
func (g *Group) Selected(network string) adapter.Outbound {
	if g.HealthCheck == nil || g.pick == nil {
		return nil
	}
	_, selected, _ := g.selection(network)
	if len(selected) == 0 {
		return nil
	}
	return selected[0].Outbound
}

// PickOutbound implements adapter.OutboundPicker
func (g *Group) PickOutbound(network string, metadata *adapter.InboundContext) adapter.Outbound {
	if g.HealthCheck == nil || g.pick == nil {
		return nil
	}
	if metadata == nil {
		metadata = &adapter.InboundContext{}
	}
	all, selected, _ := g.selection(network)
	node := g.pick.Pick(all, selected, metadata)
	if node == nil {
		return nil
	}
	return node.Outbound
}

// References implements adapter.Referrer
func (g *Group) References() (outbounds []string, dnsServers []string) {
	if g.HealthCheck == nil {
		return nil, nil
	}
	// Every node that isn't dead may serve the next connection, so it may keep its
	// idle connections. The objective is deliberately not applied here: it depends
	// on pick thresholds, which would make the references flap on jitter.
	return g.AliveTags(), nil
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
	//
	// The resolution happened before this call, so the node may have left the selection
	// in between. Such a connection would only be interrupted by the next round of
	// checks, minutes away, so it is not bound at all: it is closed now instead, which
	// is what the round would have done with it.
	g.attachAccess.Lock()
	attached := g.isAvailableNode(outboundTag)
	var remove func()
	if attached {
		remove = g.interruptGroup.Add(closer, true, outboundTag)
	}
	g.attachAccess.Unlock()
	if !attached {
		if g.interruptExternalConnections {
			common.Close(closer)
		}
		return func() {}
	}
	return remove
}

// isAvailableNode reports whether the node may still serve new connections. The caller
// must hold attachAccess, so a round of checks cannot change the answer before the
// connection is bound.
func (g *Group) isAvailableNode(outboundTag string) bool {
	if g.HealthCheck == nil || g.pick == nil {
		// the group is not started, so nothing can leave the selection yet
		return true
	}
	return slices.Contains(g.AvailableNodes(), outboundTag)
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
	return tryNodes(ctx, g, network, destination, func(node *selection.Node) (net.Conn, error) {
		conn, err := node.DialContext(ctx, network, destination)
		if err != nil {
			return nil, err
		}
		return g.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx), node.Tag()), nil
	})
}

// ListenPacket implements adapter.Outbound
func (g *Group) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return tryNodes(ctx, g, N.NetworkUDP, destination, func(node *selection.Node) (net.PacketConn, error) {
		conn, err := node.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return g.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx), node.Tag()), nil
	})
}

// tryNodes picks a node for the connection and calls connect with it until a call
// succeeds, recording the node the connection is carried with before each attempt.
func tryNodes[T any](ctx context.Context, g *Group, network string, destination M.Socksaddr, connect func(node *selection.Node) (T, error)) (T, error) {
	g.touchHealthCheck()
	return tryEach(ctx, g.logger,
		func() (*selection.Node, bool) {
			node := g.PickNode(ctx, network, destination)
			return node, node != nil
		},
		g.reportConnectionFailure,
		func(node *selection.Node) (T, error) {
			result, err := connect(node)
			if err != nil {
				return result, err
			}
			// The pick is only made here, so this is the only place that knows the node
			// the connection is carried with. A node that was tried and failed is not
			// part of the chain: the connection does not go through it.
			adapter.RecordOutboundChain(ctx, g, node.Outbound)
			return result, nil
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
	// The connections are bound under attachAccess, so the nodes they are bound to and
	// the nodes that are about to leave the selection are not read apart.
	g.attachAccess.Lock()
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
	g.attachAccess.Unlock()
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
