package balancer

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.InterfaceUpdateListener = (*Balancer)(nil)

// Balancer is the load balancer
type Balancer struct {
	*healthcheck.HealthCheck

	router    adapter.Router
	providers []adapter.Provider
	logger    log.ContextLogger
	options   *option.LoadBalanceOutboundOptions

	objective Objective
	strategy  Strategy

	maxRTT      healthcheck.RTT
	maxFailRate float32
	networks    []string
}

// New creates a new load balancer
//
// The globalHistory is optional and is only used to sync latency history
// between different health checkers. Each HealthCheck will maintain its own
// history storage since different ones can have different check destinations,
// sampling numbers, etc.
func New(
	ctx context.Context,
	router adapter.Router,
	providers []adapter.Provider, providersByTag map[string]adapter.Provider,
	options *option.LoadBalanceOutboundOptions, logger log.ContextLogger,
) (*Balancer, error) {
	if options == nil {
		options = &option.LoadBalanceOutboundOptions{}
	}
	if options.Pick.Strategy == "" {
		options.Pick.Strategy = StrategyRandom
	}
	if options.Pick.Objective == "" {
		options.Pick.Objective = ObjectiveAlive
	}

	var (
		objective Objective
		strategy  Strategy
	)
	switch options.Pick.Objective {
	case ObjectiveAlive:
		objective = NewAliveObjective()
	case ObjectiveQualified:
		objective = NewQualifiedObjective()
	case ObjectiveLeastLoad:
		objective = NewLeastObjective(
			options.Check.Sampling, options.Pick,
			func(node *Node) healthcheck.RTT {
				return node.Deviation
			},
		)
	case ObjectiveLeastPing:
		objective = NewLeastObjective(
			options.Check.Sampling, options.Pick,
			func(node *Node) healthcheck.RTT {
				return node.Average
			},
		)
	default:
		return nil, E.New("unknown objective: ", options.Pick.Objective)
	}
	switch options.Pick.Strategy {
	case StrategyRandom:
		strategy = NewRandomStrategy()
	case StrategyRoundrobin:
		strategy = NewRoundRobinStrategy()
	case StrategyConsistentHash:
		if options.Pick.Objective != ObjectiveAlive {
			return nil, E.New("consistenthash strategy works only with 'alive' objective")
		}
		strategy = NewConsistentHashStrategy()
	default:
		return nil, E.New("unknown strategy: ", options.Pick.Strategy)
	}

	if options.Check.Interval == 0 {
		options.Check.Interval = option.Duration(5 * time.Minute)
	}
	// healthcheck.New() may apply default values to options, e.g. the `sampling` which
	// is used to calculate the maxFailRate.
	hc := healthcheck.New(ctx, router, providers, providersByTag, &options.Check, logger)

	return &Balancer{
		router:      router,
		options:     options,
		logger:      logger,
		providers:   providers,
		HealthCheck: hc,
		objective:   objective,
		strategy:    strategy,

		maxRTT:      healthcheck.RTTOf(options.Pick.MaxRTT),
		maxFailRate: float32(options.Pick.MaxFail) / float32(options.Check.Sampling),
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
	filtered := b.objective.Filter(all)
	picked := b.strategy.Pick(all, filtered, metadata)
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
	for _, provider := range b.providers {
		for _, outbound := range provider.Outbounds() {
			idx++
			node := &Node{
				Outbound: outbound,
				Index:    idx,
				rand:     rand.Intn(math.MaxInt32),
			}
			networks := node.Network()
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
			node.Stats = b.HealthCheck.Storage.Stats(outbound.Tag())
			node.CalcStatus(b.maxRTT, b.maxFailRate)
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

// LogNodes logs all nodes status
func (b *Balancer) LogNodes() {
	all := b.Nodes("")
	filtered := b.objective.Filter(all)
	available := len(filtered)
	b.logger.Info(
		b.options.Pick.Objective, "/", b.options.Pick.Strategy, ", ",
		available, " of ", len(all), " nodes available",
	)
	b.logger.Info("=== nodes available ===")
	b.objective.Sort(all)
	for i, n := range all {
		if i == available {
			b.logger.Info("=== nodes unavailable ===")
		}
		b.logger.Info(n.String())
	}
}

// InterfaceUpdated implements adapter.InterfaceUpdateListener
func (b *Balancer) InterfaceUpdated() {
	// b can be nil if the parent struct has not initialized it yet.
	if b == nil || b.HealthCheck == nil {
		return
	}
	go b.HealthCheck.CheckAll(context.Background())
	return
}
