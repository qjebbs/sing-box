package balancer

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.InterfaceUpdateListener = (*Balancer)(nil)

// Balancer is the load balancer
type Balancer struct {
	*healthcheck.HealthCheck
	Adapter providersAdapter

	logger log.ContextLogger
	cfg    balancerConfig

	Objective Objective
	Strategy  Strategy

	networks []string
}

type providersAdapter interface {
	Providers() []adapter.Provider
}

// New creates a new load balancer
func New(
	logger log.ContextLogger,
	adapter providersAdapter,
	hc *healthcheck.HealthCheck,
	options option.LoadBalancePickOptions,
) (*Balancer, error) {
	cfg, err := configFromOptions(hc.Storage.Cap(), options)
	if err != nil {
		return nil, err
	}
	objective, err := newObjective(cfg)
	if err != nil {
		return nil, err
	}
	strategy, err := newStrategy(cfg)
	if err != nil {
		return nil, err
	}
	return &Balancer{
		HealthCheck: hc,
		Adapter:     adapter,
		logger:      logger,
		cfg:         cfg,
		Objective:   objective,
		Strategy:    strategy,
	}, nil
}

// PickNode picks the node for a connection, nil when no node is available.
func (b *Balancer) PickNode(ctx context.Context, network string, destination M.Socksaddr) *Node {
	metadata := adapter.ContextFrom(ctx)
	if metadata == nil {
		metadata = &adapter.InboundContext{}
	}
	metadata.Destination = destination
	all := b.Nodes(network)
	selected := Filter(b.Objective, all)
	return b.Strategy.Pick(all, selected, metadata)
}

// AvailableNodes returns the tags of the nodes new connections may use
func (b *Balancer) AvailableNodes() []string {
	_, selected := b.GetNodes()
	if len(selected) == 0 {
		return []string{}
	}
	return common.Uniq(common.Map(
		selected, func(node *Node) string {
			return node.Tag()
		},
	))
}

// LogNodes logs the statistics of every node, the selected ones first.
func (b *Balancer) LogNodes() {
	all, selected := b.GetNodes()
	if len(all) == 0 {
		return
	}
	available := len(selected)
	b.logger.Info(
		b.cfg.Objective, "/", b.cfg.Strategy, ", ",
		available, " of ", len(all), " nodes available",
	)
	b.logger.Info("=== nodes available ===")
	scorer, isScorer := b.Objective.(Scorer)
	for i, n := range b.Objective.Rank(all) {
		if i == available {
			b.logger.Info("=== nodes unavailable ===")
		}
		if isScorer {
			if score := scorer.Score(n); score != healthcheck.Failed {
				b.logger.Info(n.String(), " SCORE=", score)
			} else {
				// healthcheck.Failed means there is no score to show, printing
				// 0ms would read as the best possible result.
				b.logger.Info(n.String(), " SCORE=None")
			}
			continue
		}
		b.logger.Info(n.String())
	}
}

// RetainedNodes returns the tags to keep alive: the selected nodes, plus the nodes
// in use that just left the selection because of the measurement noise. The extra
// ones only serve the connections they already have, new connections keep to the
// selection.
func (b *Balancer) RetainedNodes(inUse []string) []string {
	all, selected := b.GetNodes()
	if len(selected) == 0 {
		return []string{}
	}
	keeper, isKeeper := b.Objective.(Keeper)
	if !isKeeper || len(inUse) == 0 {
		return nodeTags(selected)
	}
	retained := RetainNodes(all, selected, inUse, keeper)
	if len(retained) > len(selected) {
		b.logger.Debug(
			"load balancing keeps ", len(retained)-len(selected),
			" node(s) out of the selection for their existing connections",
		)
	}
	return retained
}

// Nodes returns all nodes of the network with their health status. A node of an
// outbound group is replaced by the outbound it resolves to, so nested groups are
// measured and picked by the node they actually use.
func (b *Balancer) Nodes(network string) []*Node {
	all := make([]*Node, 0)
	idx := 0
	for _, provider := range b.Adapter.Providers() {
		for _, outbound := range provider.Outbounds() {
			idx++
			if network != "" && !common.Contains(outbound.Network(), network) {
				continue
			}
			if group, ok := outbound.(adapter.OutboundGroup); ok {
				real, err := adapter.RealOutbound(group)
				if err != nil {
					continue
				}
				outbound = real
			}
			scale := calcFactor(outbound.Tag(), b.cfg.pickBiases)
			stats := b.HealthCheck.Storage.Stats(outbound.Tag())
			status := calcStatus(&stats, b.cfg.maxRTT, b.cfg.maxFailRate)
			all = append(all, NewNode(outbound, idx, scale, stats, status))
		}
	}
	return all
}

// Networks returns the networks supported by the nodes of the group.
func (b *Balancer) Networks() []string {
	if b.networks == nil {
		b.networks = b.availableNetworks()
	}
	return b.networks
}

// AliveTags returns the tags of the nodes that are not dead. Unlike the objective,
// it ignores the selection, so the result only changes when a node's health
// changes, not when it merely stops being the preferred one.
func (b *Balancer) AliveTags() []string {
	nodes := b.Nodes("")
	alive := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Status != StatusDead {
			alive = append(alive, node.Tag())
		}
	}
	if len(alive) == 0 {
		// No node is alive: nothing can be served, but outbounds may still be
		// tried, keep them referenced like the selection does.
		return common.Uniq(nodeTags(nodes))
	}
	return common.Uniq(alive)
}

// availableNetworks returns available networks of the nodes of the group.
func (b *Balancer) availableNetworks() []string {
	var hasTCP, hasUDP bool
	nodes := b.Nodes("")
	for _, n := range nodes {
		if !hasTCP && common.Contains(n.Network(), N.NetworkTCP) {
			hasTCP = true
		}
		if !hasUDP && common.Contains(n.Network(), N.NetworkUDP) {
			hasUDP = true
		}
		if hasTCP && hasUDP {
			break
		}
	}
	switch {
	case hasTCP && hasUDP:
		return []string{N.NetworkTCP, N.NetworkUDP}
	case hasTCP:
		return []string{N.NetworkTCP}
	case hasUDP:
		return []string{N.NetworkUDP}
	default:
		return []string{}
	}
}

// GetNodes returns all nodes and the selected ones, in preference order
func (b *Balancer) GetNodes() (all, selected []*Node) {
	all = b.Nodes(N.NetworkTCP)
	selected = Filter(b.Objective, all)
	return all, selected
}

// RetainNodes returns the tags of selected, plus the tags of inUse that the keeper
// still accepts, in the order of inUse.
func RetainNodes(all, selected []*Node, inUse []string, keeper Keeper) []string {
	retained := nodeTags(selected)
	if len(selected) == 0 || len(inUse) == 0 {
		return retained
	}
	selectedTags := make(map[string]bool, len(retained))
	for _, tag := range retained {
		selectedTags[tag] = true
	}
	nodesByTag := make(map[string]*Node, len(all))
	for _, node := range all {
		nodesByTag[node.Tag()] = node
	}
	best := selected[0]
	for _, tag := range inUse {
		if selectedTags[tag] {
			continue
		}
		node, loaded := nodesByTag[tag]
		if !loaded || !keeper.Keep(node, best) {
			continue
		}
		retained = append(retained, tag)
	}
	return retained
}

func nodeTags(nodes []*Node) []string {
	return common.Uniq(common.Map(nodes, func(node *Node) string {
		return node.Tag()
	}))
}
