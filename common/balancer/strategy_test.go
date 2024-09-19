package balancer_test

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/balancer"
)

func BenchmarkRandom32(b *testing.B) {
	benchmarkStrategy(b, benchmarkRandomStrategy, 32)
}

func BenchmarkRandom128(b *testing.B) {
	benchmarkStrategy(b, benchmarkRandomStrategy, 128)
}

func BenchmarkRoundRobin32(b *testing.B) {
	benchmarkStrategy(b, benchmarkRoundRobinStrategy, 32)
}

func BenchmarkRoundRobin128(b *testing.B) {
	benchmarkStrategy(b, benchmarkRoundRobinStrategy, 128)
}

func BenchmarkConsistentHash32(b *testing.B) {
	benchmarkStrategy(b, benchmarkConsistentHashStrategy, 32)
}

func BenchmarkConsistentHash128(b *testing.B) {
	benchmarkStrategy(b, benchmarkConsistentHashStrategy, 128)
}

func benchmarkStrategy(b *testing.B, s balancer.Strategy, count int) {
	ctx := &adapter.InboundContext{
		Domain: "example.com",
	}
	outbounds, store := genStorage(count)
	all := allNodes(outbounds, store)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Pick(all, all, ctx)
	}
}
