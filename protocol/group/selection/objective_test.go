package selection_test

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/selection"
)

func BenchmarkAlive32(b *testing.B) {
	benchmarkObjective(b, benchmarkAliveObjective, 32)
}

func BenchmarkAlive128(b *testing.B) {
	benchmarkObjective(b, benchmarkAliveObjective, 128)
}

func BenchmarkLeastLoad32(b *testing.B) {
	benchmarkObjective(b, benchmarkIdleObjective, 32)
}

func BenchmarkLeastLoad128(b *testing.B) {
	benchmarkObjective(b, benchmarkIdleObjective, 128)
}

func benchmarkObjective(b *testing.B, f selection.Objective, count int) {
	outbounds, store := genStorage(count)
	alive := allNodes(outbounds, store)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selection.Filter(f, alive)
	}
}

func TestNodeLinesStats(t *testing.T) {
	t.Parallel()
	// The statistics every objective is read with are the same, and each objective adds
	// the ones it ranks by: what it does not read, it does not print.
	stats := healthcheck.Stats{
		All: 9, Latest: 332, Median: 336, Floor: 184, Jitter: 6, MeanDeviation: 10, Bad: 1,
	}
	nodes := func() []*selection.Node {
		node := &selection.Node{Status: selection.StatusQualified, Stats: stats}
		node.Outbound = tagOutbound("probe")
		return []*selection.Node{node}
	}
	testCases := []struct {
		name      string
		objective selection.Objective
		want      string
	}{
		{
			name:      "alive",
			objective: selection.NewAliveObjective(),
			want:      "#0 OK [probe] LAST=332ms MED=336ms FAIL=0/9",
		},
		{
			// qualified is decided by max_rtt and max_fail, which are compared with the
			// median and the failures the line already states
			name:      "qualified",
			objective: selection.NewQualifiedObjective(),
			want:      "#0 OK [probe] LAST=332ms MED=336ms FAIL=0/9",
		},
		{
			// quick is the median plus the jitter: the jitter is the only statistic it
			// reads that the shared part does not state
			name:      "quick",
			objective: selection.NewQuickObjective(option.LoadBalancePickOptions{}),
			want:      "#0 OK [probe] LAST=332ms MED=336ms JIT=6ms SCORE=342ms FAIL=0/9",
		},
		{
			// idle measures the queueing against the floor, counts the checks that fell
			// short of it, and sizes its band by the jitter
			name:      "idle",
			objective: selection.NewIdleObjective(option.LoadBalancePickOptions{}),
			want:      "#0 OK [probe] LAST=332ms MED=336ms FLOOR=184ms JIT=6ms BAD=11% SCORE=882ms FAIL=0/9",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			lines := selection.NodeLines(testCase.objective, nodes(), nil)
			if len(lines) != 1 {
				t.Fatalf("want one line, got %d", len(lines))
			}
			if lines[0] != testCase.want {
				t.Fatalf("want %q, got %q", testCase.want, lines[0])
			}
		})
	}
}

func TestNodeLinesShowsTheFrequencyIdleCounts(t *testing.T) {
	t.Parallel()
	// idle counts every check that fell short of the floor, and the share it prints is the
	// one its score reads: the failed checks are the extreme of the same defect, and the
	// node's own record stands in where the window has nothing to say.
	testCases := []struct {
		name   string
		status selection.Status
		stats  healthcheck.Stats
		want   string
	}{
		{
			// nothing worked: the whole window fell short
			name:   "never answered",
			status: selection.StatusDead,
			stats:  healthcheck.Stats{All: 9, Fail: 9, Floor: 184},
			want:   "[probe] LAST=None MED=None FLOOR=184ms JIT=None BAD=100% SCORE=15.00s FAIL=9/9 SELECTED",
		},
		{
			// the window is clean, but the node fails in bursts: the long-run rate is part
			// of the share, so it is not lost between two rounds
			name:   "clean window, unreliable node",
			status: selection.StatusQualified,
			stats:  healthcheck.Stats{All: 9, Latest: 332, Median: 336, Floor: 184, Jitter: 6, FailureRate: 0.25},
			want:   "LAST=332ms MED=336ms FLOOR=184ms JIT=6ms BAD=25% SCORE=",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &selection.Node{Status: testCase.status, Stats: testCase.stats}
			node.Outbound = tagOutbound("probe")
			lines := selection.NodeLines(selection.NewIdleObjective(option.LoadBalancePickOptions{}), []*selection.Node{node}, []*selection.Node{node})
			if len(lines) != 1 {
				t.Fatalf("want one line, got %d", len(lines))
			}
			if !strings.Contains(lines[0], testCase.want) {
				t.Fatalf("want a line containing %q, got %q", testCase.want, lines[0])
			}
		})
	}
}
