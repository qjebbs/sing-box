package selection

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
)

func TestNodeStatus(t *testing.T) {
	t.Parallel()
	var maxRTT healthcheck.RTT = healthcheck.Second
	testCases := []struct {
		name   string
		status Status
		stats  healthcheck.Stats
	}{
		{
			"nil RTTStorage", StatusUnknown, healthcheck.Stats{
				All: 0, Fail: 0, Latest: 0, Average: 0, Median: 0,
			},
		},
		{
			"untested", StatusUnknown, healthcheck.Stats{
				All: 0, Fail: 0, Latest: 0, Average: 0, Median: 0,
			},
		},
		{
			"clean", StatusQualified, healthcheck.Stats{
				All: 10, Fail: 0, FailureWeight: 0, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"below one failure", StatusQualified, healthcheck.Stats{
				All: 10, Fail: 1, FailureWeight: 0.99, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			// a fresh failure weighs one, and the gate is below one
			"one failure", StatusAlive, healthcheck.Stats{
				All: 10, Fail: 1, FailureWeight: 1, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			// a failure on the latest check is a failure like any other: the delay
			// describes the checks that worked, it does not excuse the one that did not
			"latest_fail", StatusAlive, healthcheck.Stats{
				All: 10, Fail: 1, FailureWeight: 1, Latest: healthcheck.Failed, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"a burst", StatusAlive, healthcheck.Stats{
				All: 10, Fail: 2, FailureWeight: 2, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			// nothing ever worked: there is no delay to rank and nothing to fall back on
			"never succeeded", StatusDead, healthcheck.Stats{
				All: 10, Fail: 10, FailureWeight: 10, Latest: healthcheck.Failed,
			},
		},
		{
			"over max_rtt", StatusAlive, healthcheck.Stats{
				All: 10, Fail: 0, FailureWeight: 0, Latest: healthcheck.Second, Average: 2 * healthcheck.Second, Median: 2 * healthcheck.Second,
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := calcStatus(&tc.stats, maxRTT)
			if got != tc.status {
				t.Errorf("want: %s, got: %s", tc.status, got)
			}
		})
	}
}

func TestNodeString(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		status Status
		stats  healthcheck.Stats
		want   string
	}{
		// a node whose checks all failed has no delay to show: the statistics describe
		// the checks that succeeded, and none did
		{
			name:  "all failed",
			stats: healthcheck.Stats{All: 1, Fail: 1},
			want:  "#0 x [nil] LAST=None MED=None",
		},
		// the floor outlives the window, but it is a statistic of the idle objective and
		// is not part of the line every objective shares
		{
			name:  "all failed with remembered floor",
			stats: healthcheck.Stats{All: 1, Fail: 1, Floor: 60},
			want:  "#0 x [nil] LAST=None MED=None",
		},
		// one successful check among failures still has a delay and a median
		{
			name:  "some failed",
			stats: healthcheck.Stats{All: 2, Fail: 1, Jitter: 40, MeanDeviation: 35, Median: 100, Latest: 120},
			want:  "#0 x [nil] LAST=120ms MED=100ms",
		},
		// the spreads are not printed here: each objective prints the ones it reads, see
		// TestNodeLinesStats
		{
			name:  "clear",
			stats: healthcheck.Stats{All: 10, Jitter: 57, MeanDeviation: 39, Median: 164, Latest: 155},
			want:  "#0 x [nil] LAST=155ms MED=164ms",
		},
		// the average is not printed, it mixes the delays and the timeouts
		{
			name:  "no average",
			stats: healthcheck.Stats{All: 10, Fail: 1, Jitter: 4330, MeanDeviation: 1830, Median: 526, Average: 2050, Latest: 402},
			want:  "#0 x [nil] LAST=402ms MED=526ms",
		},
		// a node that was never checked has nothing to show
		{
			name:   "never checked",
			status: StatusUnknown,
			stats:  healthcheck.Stats{},
			want:   "#0 ? [nil] LAST=None MED=None",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			node := &Node{Status: testCase.status, Stats: testCase.stats}
			if got := node.String(); got != testCase.want {
				t.Fatalf("want %q, got %q", testCase.want, got)
			}
		})
	}
}

func TestNodeStringScaled(t *testing.T) {
	t.Parallel()
	node := &Node{
		RTTSacale: 2,
		Stats:     healthcheck.Stats{All: 10, Jitter: 57, MeanDeviation: 39, Median: 164, Latest: 155},
	}
	want := "LAST=155ms(310ms) MED=164ms(328ms)"
	if got := node.String(); !strings.Contains(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	// the missing values stay missing, with or without a scale
	node = &Node{
		RTTSacale: 2,
		Stats:     healthcheck.Stats{All: 1, Fail: 1, Median: 15000},
	}
	want = "LAST=None MED=None"
	if got := node.String(); !strings.Contains(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
}
