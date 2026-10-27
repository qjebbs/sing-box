package balancer_test

import (
	"context"
	"math/rand"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/block"
	"github.com/sagernet/sing-box/protocol/group/balancer"
	M "github.com/sagernet/sing/common/metadata"
)

var (
	benchmarkPickOptions            = option.LoadBalancePickOptions{}
	benchmarkAliveObjective         = balancer.NewAliveObjective()
	benchmarkLeastLoadObjective     = balancer.NewLeastLoadObjective(benchmarkPickOptions)
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
		node := balancer.NewNode(o, i, 1, s.Stats(o.Tag()), balancer.StatusUnknown)
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
	b, _ := block.New(context.Background(), nil, nil, strconv.Itoa(index), option.StubOptions{})
	return b
}

type stubKeeper struct {
	keep map[string]bool
}

func (k stubKeeper) Keep(node *balancer.Node, best *balancer.Node) bool {
	return k.keep[node.Tag()]
}

type tagOutbound string

func (tagOutbound) Network() []string      { return []string{"tcp"} }
func (o tagOutbound) Tag() string          { return string(o) }
func (tagOutbound) Dependencies() []string { return nil }
func (tagOutbound) Type() string           { return "stub" }
func (tagOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, nil
}
func (tagOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, nil
}

func taggedNodes(tags ...string) []*balancer.Node {
	nodes := make([]*balancer.Node, 0, len(tags))
	for i, tag := range tags {
		nodes = append(nodes, balancer.NewNode(tagOutbound(tag), i, 1, healthcheck.Stats{}, balancer.StatusQualified))
	}
	return nodes
}

func tagList(nodes []*balancer.Node) []string {
	tags := make([]string, 0, len(nodes))
	for _, node := range nodes {
		tags = append(tags, node.Tag())
	}
	return tags
}

func TestRetainNodes(t *testing.T) {
	t.Parallel()
	all := taggedNodes("a", "b", "c", "d")
	selected := all[:2]
	keeper := stubKeeper{keep: map[string]bool{"c": true}}
	testCases := []struct {
		name   string
		inUse  []string
		keeper balancer.Keeper
		want   []string
	}{
		{name: "nothing in use", inUse: nil, keeper: keeper, want: []string{"a", "b"}},
		{name: "already selected", inUse: []string{"a", "b"}, keeper: keeper, want: []string{"a", "b"}},
		{name: "kept", inUse: []string{"c"}, keeper: keeper, want: []string{"a", "b", "c"}},
		{name: "dropped", inUse: []string{"d"}, keeper: keeper, want: []string{"a", "b"}},
		{name: "unknown tag", inUse: []string{"zzz"}, keeper: keeper, want: []string{"a", "b"}},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := balancer.RetainNodes(all, selected, testCase.inUse, testCase.keeper)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

func TestRetainNodesNoSelection(t *testing.T) {
	t.Parallel()
	all := taggedNodes("a", "b")
	if got := balancer.RetainNodes(all, nil, []string{"a"}, stubKeeper{}); len(got) != 0 {
		t.Fatalf("want nothing, got %v", got)
	}
}

func TestRetainNodesUsesObjective(t *testing.T) {
	t.Parallel()
	// The real objective decides, not a stub: 5 minutes of statistics put the node
	// right outside the band, but its deviation is large enough that the excess is
	// noise, so its connections are kept.
	objective := balancer.NewLeastPingObjective(option.LoadBalancePickOptions{})
	all := []*balancer.Node{
		{
			Index: 0, Status: balancer.StatusQualified,
			Stats: healthcheck.Stats{All: 10, Median: 161, Deviation: 57},
		},
		{
			Index: 1, Status: balancer.StatusQualified,
			Stats: healthcheck.Stats{All: 10, Median: 178, Deviation: 126},
		},
	}
	all[0].Outbound, all[1].Outbound = tagOutbound("fast"), tagOutbound("jittery")
	selected := objective.Filter(all)
	if len(selected) != 1 || selected[0].Tag() != "fast" {
		t.Fatalf("want only the fast node selected, got %v", tagList(selected))
	}
	got := balancer.RetainNodes(all, selected, []string{"jittery"}, objective)
	if !slices.Equal(got, []string{"fast", "jittery"}) {
		t.Fatalf("want the jittery node kept, got %v", got)
	}
	// it is not selected, new connections must not use it
	if filtered := objective.Filter(all); len(filtered) != 1 {
		t.Fatalf("the selection must stay strict, got %v", tagList(filtered))
	}
}
