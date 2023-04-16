package balancer

import (
	"sort"
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
func (s *RoundRobinStrategy) Pick(_, filtered []*Node, _ *adapter.InboundContext) *Node {
	if len(filtered) == 0 {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Index > filtered[j].Index
	})
	for _, node := range filtered {
		if node.Index > s.index {
			s.index = node.Index
			return node
		}
	}
	s.index = filtered[0].Index
	return filtered[0]
}
