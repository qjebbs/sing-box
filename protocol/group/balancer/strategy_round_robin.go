package balancer

import (
	"sync"

	"github.com/sagernet/sing-box/adapter"
)

var _ Strategy = (*RoundRobinStrategy)(nil)

// RoundRobinStrategy is the round robin strategy
type RoundRobinStrategy struct {
	sync.Mutex
	index int
}

// NewRoundRobinStrategy returns a new RoundRobinStrategy
func NewRoundRobinStrategy() *RoundRobinStrategy {
	return &RoundRobinStrategy{index: -1}
}

// Pick implements Strategy
// Pick implements Strategy. The filtered nodes may be shared with the cached ranking of
// the balancer, so it must not be reordered in place: the node after the last one is
// found in a single pass instead of sorting.
func (s *RoundRobinStrategy) Pick(_, filtered []*Node, _ *adapter.InboundContext) *Node {
	if len(filtered) == 0 {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	var next, first *Node
	for _, node := range filtered {
		if first == nil || node.Index < first.Index {
			first = node
		}
		if node.Index > s.index && (next == nil || node.Index < next.Index) {
			next = node
		}
	}
	if next == nil {
		next = first
	}
	s.index = next.Index
	return next
}
