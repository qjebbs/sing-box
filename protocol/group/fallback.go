package group

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

func RegisterFallback(registry *outbound.Registry) {
	outbound.Register[option.FallbackOutboundOptions](registry, C.TypeFallback, NewFallback)
}

var (
	_ adapter.Outbound  = (*Fallback)(nil)
	_ adapter.Referrer  = (*Fallback)(nil)
	_ adapter.Lifecycle = (*Fallback)(nil)
)

// Fallback tries its outbounds one by one, in the declared order, until one of them
// accepts the connection.
//
// It deliberately does not implement adapter.OutboundGroup: the router resolves a group
// to the node it selects before dialing, so a group would never carry the fallback and
// the connections would not be retried on the next node.
type Fallback struct {
	outbound.GroupAdapter
	ctx            context.Context
	logger         log.ContextLogger
	outbound       adapter.OutboundManager
	provider       adapter.ProviderManager
	healthCheckMgr adapter.HealthCheckManager
	options        option.FallbackOutboundOptions

	// healthCheck is nil unless `health_check` is configured, see Start.
	healthCheck *healthcheck.HealthCheck
}

func NewFallback(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.FallbackOutboundOptions) (adapter.Outbound, error) {
	return &Fallback{
		GroupAdapter:   outbound.NewGroupAdapter(C.TypeFallback, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.ProviderGroupCommonOption),
		ctx:            ctx,
		logger:         logger,
		outbound:       service.FromContext[adapter.OutboundManager](ctx),
		provider:       service.FromContext[adapter.ProviderManager](ctx),
		healthCheckMgr: service.FromContext[adapter.HealthCheckManager](ctx),
		options:        options,
	}, nil
}

// Start implements adapter.Lifecycle
func (f *Fallback) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	if err := f.InitProviders(f.outbound, f.provider); err != nil {
		return err
	}
	if f.options.HealthCheck == nil || f.options.HealthCheck.IsEmpty() {
		// No checker is attached unless it is configured: the checks are what makes
		// the outbounds look alive, and a group that only carries the connections it
		// is asked to must not start rounds of its own.
		return nil
	}
	healthCheck, err := f.AttachHealthCheck(f.ctx, f.logger, f.healthCheckMgr, f.options.HealthCheck, f.healthChecksUpdated)
	if err != nil {
		return err
	}
	f.healthCheck = healthCheck
	scope.Add(f.Close)
	return nil
}

// Close releases the health check, it is registered on the scope by Start.
func (f *Fallback) Close() error {
	return f.DetachHealthCheck(f.healthCheck, f.healthChecksUpdated)
}

// healthChecksUpdated is the post-check listener. The order is resolved per connection,
// so a finished round needs no action here, but the listener is what binds the group to
// the checker.
func (f *Fallback) healthChecksUpdated() {}

// References implements adapter.Referrer
func (f *Fallback) References() []string {
	outbounds := f.Outbounds()
	if len(outbounds) == 0 {
		return f.Dependencies()
	}
	// Any node may serve the next connection, so all of them are kept ready.
	return common.Map(outbounds, func(it adapter.Outbound) string {
		return it.Tag()
	})
}

// DialContext implements adapter.Outbound
func (f *Fallback) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	nodes := f.candidates(network)
	if len(nodes) == 0 {
		return nil, E.New("no outbound available for ", network)
	}
	return dialFirst(ctx, f.logger, nodes, f.reportConnection, func(node adapter.Outbound) (net.Conn, error) {
		return node.DialContext(ctx, network, destination)
	})
}

// ListenPacket implements adapter.Outbound
func (f *Fallback) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	nodes := f.candidates(N.NetworkUDP)
	if len(nodes) == 0 {
		return nil, E.New("no outbound available for ", N.NetworkUDP)
	}
	return dialFirst(ctx, f.logger, nodes, f.reportConnection, func(node adapter.Outbound) (net.PacketConn, error) {
		return node.ListenPacket(ctx, destination)
	})
}

// candidates returns the outbounds that may be used for the network, in the order they
// are tried: the explicit tags first, then the nodes of the providers, with the nodes
// that passed their latest health check ahead of the others.
func (f *Fallback) candidates(network string) []adapter.Outbound {
	if f.healthCheck != nil {
		f.healthCheck.Touch(f.Tag())
	}
	return orderCandidates(filterCandidates(f.Outbounds(), network), f.alive)
}

// alive reports whether the node passed its latest health check. A node that was never
// checked is not alive, but it keeps its place: see orderCandidates.
func (f *Fallback) alive(tag string) bool {
	if f.healthCheck == nil {
		return false
	}
	stats := f.healthCheck.Storage.Stats(tag)
	return stats.All > 0 && stats.Latest != healthcheck.Failed
}

func (f *Fallback) reportConnection(node adapter.Outbound, established bool) {
	if f.healthCheck == nil {
		return
	}
	f.healthCheck.ReportConnection(node, established)
}

// filterCandidates keeps the nodes that support the network, in order. A node that
// declares no network is kept: an empty list means unknown, not unsupported.
func filterCandidates(outbounds []adapter.Outbound, network string) []adapter.Outbound {
	nodes := make([]adapter.Outbound, 0, len(outbounds))
	for _, node := range outbounds {
		if networks := node.Network(); len(networks) > 0 && !common.Contains(networks, network) {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// orderCandidates moves the nodes that are not alive behind the alive ones, keeping the
// declared order inside both groups. The nodes that are not alive are still tried, so a
// group whose checks all fail keeps working, like the fallback group of the other
// clients. A nil alive keeps the order as it is.
func orderCandidates(outbounds []adapter.Outbound, alive func(tag string) bool) []adapter.Outbound {
	if alive == nil {
		return outbounds
	}
	nodes := make([]adapter.Outbound, 0, len(outbounds))
	var notAlive []adapter.Outbound
	for _, node := range outbounds {
		if alive(node.Tag()) {
			nodes = append(nodes, node)
		} else {
			notAlive = append(notAlive, node)
		}
	}
	if len(nodes) == 0 {
		return notAlive
	}
	return append(nodes, notAlive...)
}

// dialFirst calls connect with each node in order and returns the first attempt that
// succeeds. Every failure is kept, so the error reports all the nodes that were tried.
func dialFirst[T any](
	ctx context.Context,
	logger log.ContextLogger,
	nodes []adapter.Outbound,
	observe func(node adapter.Outbound, established bool),
	connect func(node adapter.Outbound) (T, error),
) (T, error) {
	var (
		value      T
		connErrors []error
	)
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			connErrors = append(connErrors, err)
			break
		}
		attempt, err := connect(node)
		if observe != nil {
			observe(node, err == nil)
		}
		if err == nil {
			return attempt, nil
		}
		logger.DebugContext(ctx, "fallback: ", node.Tag(), " failed: ", err)
		connErrors = append(connErrors, E.Cause(err, node.Tag()))
	}
	if len(connErrors) == 0 {
		return value, E.New("no outbound available")
	}
	return value, E.Cause(E.Errors(connErrors...), "all outbounds failed")
}
