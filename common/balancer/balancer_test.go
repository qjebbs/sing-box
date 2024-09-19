package balancer_test

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/balancer"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/outbound"
)

var (
	benchmarkPickOptions = option.LoadBalancePickOptions{
		Expected: 9999,
	}
	benchmarkAliveObjective     = balancer.NewAliveObjective()
	benchmarkLeastLoadObjective = balancer.NewLeastObjective(10, benchmarkPickOptions, func(node *balancer.Node) healthcheck.RTT {
		return node.Deviation
	})
	benchmarkRandomStrategy         = balancer.NewRandomStrategy()
	benchmarkRoundRobinStrategy     = balancer.NewRoundRobinStrategy()
	benchmarkConsistentHashStrategy = balancer.NewConsistentHashStrategy()
)

func BenchmarkAliveRandom32(b *testing.B) {
	benchmarkBalancer(b, benchmarkAliveObjective, benchmarkRandomStrategy, 32)
}

func BenchmarkAliveRandom128(b *testing.B) {
	benchmarkBalancer(b, benchmarkAliveObjective, benchmarkRandomStrategy, 128)
}

func BenchmarkLeastloadRandom32(b *testing.B) {
	benchmarkBalancer(b, benchmarkLeastLoadObjective, benchmarkRandomStrategy, 32)
}

func BenchmarkLeastloadRandom128(b *testing.B) {
	benchmarkBalancer(b, benchmarkLeastLoadObjective, benchmarkRandomStrategy, 128)
}

func BenchmarkAliveRoundRobin32(b *testing.B) {
	benchmarkBalancer(b, benchmarkAliveObjective, benchmarkRoundRobinStrategy, 32)
}

func BenchmarkAliveRoundRobin128(b *testing.B) {
	benchmarkBalancer(b, benchmarkAliveObjective, benchmarkRoundRobinStrategy, 128)
}

func BenchmarkLeastloadRoundRobin32(b *testing.B) {
	benchmarkBalancer(b, benchmarkLeastLoadObjective, benchmarkRoundRobinStrategy, 32)
}

func BenchmarkLeastloadRoundRobin128(b *testing.B) {
	benchmarkBalancer(b, benchmarkLeastLoadObjective, benchmarkRoundRobinStrategy, 128)
}

func BenchmarkAliveConsistentHash32(b *testing.B) {
	benchmarkBalancer(b, benchmarkAliveObjective, benchmarkConsistentHashStrategy, 32)
}

func BenchmarkAliveConsistentHash128(b *testing.B) {
	benchmarkBalancer(b, benchmarkAliveObjective, benchmarkConsistentHashStrategy, 128)
}

func BenchmarkLeastloadConsistentHash32(b *testing.B) {
	benchmarkBalancer(b, benchmarkLeastLoadObjective, benchmarkConsistentHashStrategy, 32)
}

func BenchmarkLeastloadConsistentHash128(b *testing.B) {
	benchmarkBalancer(b, benchmarkLeastLoadObjective, benchmarkConsistentHashStrategy, 128)
}

func benchmarkBalancer(b *testing.B, f balancer.Objective, s balancer.Strategy, count int) {
	ctx := &adapter.InboundContext{
		Domain: "example.com",
	}
	outbounds, store := genStorage(count)
	// for _, n := range f.Objective(allNodes(outbounds, store)) {
	// 	b.Logf("node #%d: %v", n.Index, n.Stats)
	// }
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all := allNodes(outbounds, store)
		filtered := f.Filter(all)
		s.Pick(all, filtered, ctx)
	}
}

func BenchmarkGetAllNodes32(b *testing.B) {
	benchmarkGetAllNodes(b, 32)
}

func BenchmarkGetAllNodes128(b *testing.B) {
	benchmarkGetAllNodes(b, 128)
}

func benchmarkGetAllNodes(b *testing.B, count int) {
	outbounds, store := genStorage(count)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		allNodes(outbounds, store)
	}
}

func allNodes(outbounds []adapter.Outbound, s *healthcheck.Storages) []*balancer.Node {
	all := make([]*balancer.Node, 0, len(outbounds))
	for i, o := range outbounds {
		stat := s.Stats(o.Tag())
		node := &balancer.Node{
			Index:    i,
			Outbound: o,
			Stats:    stat,
		}
		node.CalcStatus(healthcheck.Second, 0)
		all = append(all, node)
	}
	return all
}

func genStorage(count int) ([]adapter.Outbound, *healthcheck.Storages) {
	store := healthcheck.NewStorages(uint(count), time.Hour)
	outbounds := make([]adapter.Outbound, 0, count)
	for i := 0; i < count; i++ {
		outbounds = append(outbounds, genNode(store, i))
	}
	return outbounds, store
}

func genNode(store *healthcheck.Storages, index int) adapter.Outbound {
	const sampling = 10
	tag := strconv.Itoa(index)
	for i := 0; i < sampling; i++ {
		sample := healthcheck.RTT(rand.NormFloat64()*100) + 200
		store.Put(tag, sample)
	}
	return outbound.NewBlock(nil, strconv.Itoa(index))
}
