package balancer

import (
	"github.com/sagernet/sing-box/adapter"
	E "github.com/sagernet/sing/common/exceptions"
)

// Strategy is the interface for balancer strategies
type Strategy interface {
	// Name returns the strategy the group was configured with.
	Name() string
	// Pick picks a node from the given nodes
	//
	//  - The `all` nodes are stable in both number and order between pickings.
	//  - The `filtered` nodes are filtered and sorted by the objective.
	Pick(all, filtered []*Node, metadata *adapter.InboundContext) *Node
}

// NewStrategy returns the strategy of the name, random when the name is empty.
func NewStrategy(name, objective string) (Strategy, error) {
	if name == "" {
		name = StrategyRandom
	}
	if objective == "" {
		objective = ObjectiveAlive
	}
	switch name {
	case StrategyRandom:
		return NewRandomStrategy(), nil
	case StrategyRoundrobin:
		return NewRoundRobinStrategy(), nil
	case StrategyConsistentHash:
		if objective != ObjectiveAlive {
			return nil, E.New("consistenthash strategy works only with 'alive' objective")
		}
		return NewConsistentHashStrategy(), nil
	default:
		return nil, E.New("unknown strategy: ", name)
	}
}
