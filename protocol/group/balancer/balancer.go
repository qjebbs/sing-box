package balancer

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/service/healthcheck"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
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
//
// The globalHistory is optional and is only used to sync latency history
// between different health checkers. Each HealthCheck will maintain its own
// history storage since different ones can have different check destinations,
// sampling numbers, etc.
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
	var (
		objective Objective
		strategy  Strategy
	)
	switch cfg.Objective {
	case ObjectiveAlive:
		objective = NewAliveObjective()
	case ObjectiveQualified:
		objective = NewQualifiedObjective()
	case ObjectiveLeastLoad:
		objective = NewLeastLoadObjective(options)
	case ObjectiveLeastPing:
		objective = NewLeastPingObjective(options)
	default:
		return nil, E.New("unknown objective: ", cfg.Objective)
	}
	switch cfg.Strategy {
	case StrategyRandom:
		strategy = NewRandomStrategy()
	case StrategyRoundrobin:
		strategy = NewRoundRobinStrategy()
	case StrategyConsistentHash:
		if cfg.Objective != ObjectiveAlive {
			return nil, E.New("consistenthash strategy works only with 'alive' objective")
		}
		strategy = NewConsistentHashStrategy()
	default:
		return nil, E.New("unknown strategy: ", cfg.Strategy)
	}

	return &Balancer{
		cfg:         cfg,
		logger:      logger,
		Adapter:     adapter,
		HealthCheck: hc,
		Objective:   objective,
		Strategy:    strategy,
	}, nil
}

// Pick picks a node
func (b *Balancer) Pick(ctx context.Context, network string, destination M.Socksaddr) adapter.Outbound {
	metadata := adapter.ContextFrom(ctx)
	if metadata == nil {
		metadata = &adapter.InboundContext{}
	}
	metadata.Destination = destination
	all := b.Nodes(network)
	filtered := b.Objective.Filter(all)
	picked := b.Strategy.Pick(all, filtered, metadata)
	if picked == nil {
		return nil
	}
	return picked.Outbound
}

// Networks returns all networks supported by this balancer
func (b *Balancer) Networks() []string {
	if b.networks == nil {
		b.networks = b.availableNetworks()
	}
	return b.networks
}

// Nodes returns all Nodes for the network
func (b *Balancer) Nodes(network string) []*Node {
	all := make([]*Node, 0)
	idx := 0
	for _, provider := range b.Adapter.Providers() {
		for _, outbound := range provider.Outbounds() {
			idx++
			networks := outbound.Network()
			if network != "" && !common.Contains(networks, network) {
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
			node := NewNode(outbound, idx, scale, stats, status)
			all = append(all, node)
		}
	}
	return all
}

// availableNetworks returns available networks of qualified nodes
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

// GetNodes returns all nodes and filtered nodes, and logs the nodes if logging is true
func (b *Balancer) GetNodes(logging bool) (all, filtered []*Node) {
	all = b.Nodes("")
	filtered = b.Objective.Filter(all)
	if !logging {
		return all, filtered
	}
	available := len(filtered)
	b.logger.Info(
		b.cfg.Objective, "/", b.cfg.Strategy, ", ",
		available, " of ", len(all), " nodes available",
	)
	b.logger.Info("=== nodes available ===")
	b.Objective.Sort(all)
	for i, n := range all {
		if i == available {
			b.logger.Info("=== nodes unavailable ===")
		}
		b.logger.Info(n.String())
	}
	return all, filtered
}
