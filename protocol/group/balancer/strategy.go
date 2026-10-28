package balancer

import (
	"github.com/sagernet/sing-box/adapter"
	E "github.com/sagernet/sing/common/exceptions"
)

// Strategy is the interface for balancer strategies
type Strategy interface {
	// Pick picks a node from the given nodes
	//
	//  - The `all` nodes are stable in both number and order between pickings.
	//  - The `filtered` nodes are filtered and sorted by the objective.
	Pick(all, filtered []*Node, metadata *adapter.InboundContext) *Node
}

// newStrategy returns the strategy resolved from the configuration.
func newStrategy(cfg balancerConfig) (Strategy, error) {
	switch cfg.Strategy {
	case StrategyRandom:
		return NewRandomStrategy(), nil
	case StrategyRoundrobin:
		return NewRoundRobinStrategy(), nil
	case StrategyConsistentHash:
		if cfg.Objective != ObjectiveAlive {
			return nil, E.New("consistenthash strategy works only with 'alive' objective")
		}
		return NewConsistentHashStrategy(), nil
	default:
		return nil, E.New("unknown strategy: ", cfg.Strategy)
	}
}
