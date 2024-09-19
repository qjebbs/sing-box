package balancer

import "github.com/sagernet/sing-box/adapter"

// Strategy is the interface for balancer strategies
type Strategy interface {
	// Pick picks a node from the given nodes
	//
	//  - The `all` nodes are stable in both number and order between pickings.
	//  - The `filtered` nodes are filtered and sorted by the objective.
	Pick(all, filtered []*Node, metadata *adapter.InboundContext) *Node
}
