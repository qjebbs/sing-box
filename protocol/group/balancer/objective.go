package balancer

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
