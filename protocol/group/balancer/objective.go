package balancer

import (
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

// Objective is the interface for balancer objectives
type Objective interface {
	// Name returns the objective the group was configured with.
	Name() string
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

// ObjectiveOptions configure the ranking and the cluster around the best node.
type ObjectiveOptions struct {
	// JitterWeight scales the standard deviation added to the score of the leastping
	// objective. It is not used by leastload, whose score is the deviation itself.
	JitterWeight float32
	// Tolerance is the band around the best node within which the nodes are equally
	// good, relative to its score, e.g. 0.2 means 20%.
	Tolerance float32
	// MinNodes is the fewest nodes the cluster is extended to, MaxNodes the most it is
	// limited to, 0 for no limit.
	MinNodes uint
	MaxNodes uint
}

// NewObjective returns the objective of the name, alive when the name is empty.
func NewObjective(name string, options ObjectiveOptions) (Objective, error) {
	pick := option.LoadBalancePickOptions{
		JitterWeight: options.JitterWeight,
		Tolerance:    options.Tolerance,
		MinNodes:     options.MinNodes,
		MaxNodes:     options.MaxNodes,
	}
	switch name {
	case "", ObjectiveAlive:
		return NewAliveObjective(), nil
	case ObjectiveQualified:
		return NewQualifiedObjective(), nil
	case ObjectiveLeastLoad:
		return NewLeastLoadObjective(pick), nil
	case ObjectiveLeastPing:
		return NewLeastPingObjective(pick), nil
	default:
		return nil, E.New("unknown objective: ", name)
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
