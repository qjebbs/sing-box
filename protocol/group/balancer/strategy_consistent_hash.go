package balancer

import (
	"hash/crc32"

	"github.com/sagernet/sing-box/adapter"

	"golang.org/x/net/publicsuffix"
)

var _ Strategy = (*ConsistentHashStrategy)(nil)

// ConsistentHashStrategy is the random strategy
type ConsistentHashStrategy struct{}

// NewConsistentHashStrategy returns a new ConsistentHashStrategy
func NewConsistentHashStrategy() *ConsistentHashStrategy {
	return &ConsistentHashStrategy{}
}

// Pick implements Strategy
func (s *ConsistentHashStrategy) Pick(all, filtered []*Node, metadata *adapter.InboundContext) *Node {
	// Consistent Hashing: Algorithmic Tradeoffs
	// https://dgryski.medium.com/consistent-hashing-algorithmic-tradeoffs-ef6b8e2fcae8
	//
	// Jump Hash requires a stable number and order of nodes.
	// so we pick from all nodes instead of filtered nodes.
	if len(all) == 0 {
		return nil
	}
	// suppose half of the nodes are dead, the probability of select an alive
	// node in 7 retries is 1-0.5^7 = 0.9921875
	maxRetry := 7
	buckets := len(all)
	key := uint64(crc32.ChecksumIEEE([]byte(getKey(metadata))))
	for i := 0; i < maxRetry; i, key = i+1, key+1 {
		idx := jumpHash(key, buckets)
		if all[idx].Status != StatusDead {
			return all[idx]
		}
	}
	return nil
}

func getKey(metadata *adapter.InboundContext) string {
	if metadata.Domain != "" {
		if etld, err := publicsuffix.EffectiveTLDPlusOne(metadata.Domain); err == nil {
			return etld
		}
	}
	return metadata.Destination.String()
}

// Hash consistently chooses a hash bucket number in the range [0, numBuckets) for the given key. numBuckets must be >= 1.
//
// https://github.com/dgryski/go-jump/blob/master/jump.go
func jumpHash(key uint64, numBuckets int) int32 {
	var b int64 = -1
	var j int64

	for j < int64(numBuckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}

	return int32(b)
}
