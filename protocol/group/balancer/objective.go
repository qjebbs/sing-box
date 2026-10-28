package balancer

import (
	"github.com/sagernet/sing-box/common/healthcheck"
	E "github.com/sagernet/sing/common/exceptions"
)

// Objective is the interface for balancer objectives
type Objective interface {
	// Rank orders the nodes from the most to the least preferred, and returns them
	// as a new slice, so the number and order of `all` are kept, which matters for
	// the consistent hash strategy.
	//
	// The status is part of the order: the nodes of a better class come first, so
	// Select can take a head of the result.
	Rank(all []*Node) []*Node
	// Select returns the nodes for new connections, a head of ranked in practice.
	//
	// Conventions：
	//  1. takes care of the fallback logic, it never returns an empty slice if
	//     ranked is not empty.
	//  2. only returns nodes of a single class, a selection never mixes a fallback
	//     status in.
	Select(ranked []*Node) []*Node
}

// Filter returns the nodes for new connections in preference order.
func Filter(objective Objective, all []*Node) []*Node {
	return objective.Select(objective.Rank(all))
}

// newObjective returns the objective resolved from the configuration.
func newObjective(cfg balancerConfig) (Objective, error) {
	switch cfg.Objective {
	case ObjectiveAlive:
		return NewAliveObjective(), nil
	case ObjectiveQualified:
		return NewQualifiedObjective(), nil
	case ObjectiveLeastLoad:
		return NewLeastLoadObjective(cfg.LoadBalancePickOptions), nil
	case ObjectiveLeastPing:
		return NewLeastPingObjective(cfg.LoadBalancePickOptions), nil
	default:
		return nil, E.New("unknown objective: ", cfg.Objective)
	}
}

// Scorer is an optional interface for objectives that rank nodes by a score.
type Scorer interface {
	// Score returns the cost of using the node, lower is better.
	Score(node *Node) healthcheck.RTT
}

// Keeper is an optional interface for objectives that can tell whether a node that
// just left the selection should still serve the connections it already has.
type Keeper interface {
	// Keep reports whether node, which is no longer selected, is close enough to
	// best to keep its existing connections. See LeastObjective.Keep.
	Keep(node *Node, best *Node) bool
}
