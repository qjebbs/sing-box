package balancer

import "github.com/sagernet/sing-box/common/healthcheck"

// Objective is the interface for balancer objectives
type Objective interface {
	// Filter filters nodes from all.
	//
	// Conventions：
	//  1. keeps the slice `all` unchanged, because the number and order matters for consistent hash strategy.
	//  2. takes care of the fallback logic, it never returns an empty slice if `all` is not empty.
	Filter(all []*Node) []*Node
	// Sort sorts nodes according to the objective, better nodes are in front.
	Sort([]*Node)
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
