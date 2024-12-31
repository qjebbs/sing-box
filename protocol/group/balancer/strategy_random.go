package balancer

import (
	"math/rand"

	"github.com/sagernet/sing-box/adapter"
)

var _ Strategy = (*RandomStrategy)(nil)

// RandomStrategy is the random strategy
type RandomStrategy struct{}

// NewRandomStrategy returns a new RandomStrategy
func NewRandomStrategy() *RandomStrategy {
	return &RandomStrategy{}
}

// Pick implements Strategy
func (s *RandomStrategy) Pick(_, filtered []*Node, _ *adapter.InboundContext) *Node {
	count := len(filtered)
	if count == 0 {
		return nil
	}
	return filtered[rand.Intn(count)]
}
