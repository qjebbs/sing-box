package balancer_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/balancer"
)

func BenchmarkAlive32(b *testing.B) {
	benchmarkObjective(b, benchmarkAliveObjective, 32)
}

func BenchmarkAlive128(b *testing.B) {
	benchmarkObjective(b, benchmarkAliveObjective, 128)
}

func BenchmarkLeastLoad32(b *testing.B) {
	benchmarkObjective(b, benchmarkLeastLoadObjective, 32)
}

func BenchmarkLeastLoad128(b *testing.B) {
	benchmarkObjective(b, benchmarkLeastLoadObjective, 128)
}

func benchmarkObjective(b *testing.B, f balancer.Objective, count int) {
	outbounds, store := genStorage(count)
	alive := allNodes(outbounds, store)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Filter(alive)
	}
}
