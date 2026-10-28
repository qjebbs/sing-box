package balancer

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
)

func TestNodeStatus(t *testing.T) {
	t.Parallel()
	var maxRTT healthcheck.RTT = healthcheck.Second
	var maxFailRate float32 = 0.2
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
			"@max_rtt", StatusQualified, healthcheck.Stats{
				All: 10, Fail: 0, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"@max_fail", StatusQualified, healthcheck.Stats{
				All: 10, Fail: 2, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"@max_fail_2", StatusQualified, healthcheck.Stats{
				All: 5, Fail: 1, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"latest_fail", StatusDead, healthcheck.Stats{
				All: 10, Fail: 1, Latest: healthcheck.Failed, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"over max_fail", StatusAlive, healthcheck.Stats{
				All: 5, Fail: 2, Latest: healthcheck.Second, Average: healthcheck.Second, Median: healthcheck.Second,
			},
		},
		{
			"over max_rtt", StatusAlive, healthcheck.Stats{
				All: 10, Fail: 0, Latest: healthcheck.Second, Average: 2 * healthcheck.Second, Median: 2 * healthcheck.Second,
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := calcStatus(&tc.stats, maxRTT, maxFailRate)
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
			want:  "#0 x [nil] STD=None MED=None Latest=None FAIL=1/1",
		},
		// one successful check among failures still has a deviation
		{
			name:  "some failed",
			stats: healthcheck.Stats{All: 2, Fail: 1, Deviation: 40, Median: 100, Latest: 120},
			want:  "#0 x [nil] STD=40ms MED=100ms Latest=120ms FAIL=1/2",
		},
		{
			name:  "clear",
			stats: healthcheck.Stats{All: 10, Deviation: 57, Median: 164, Latest: 155},
			want:  "#0 x [nil] STD=57ms MED=164ms Latest=155ms FAIL=0/10",
		},
		// the average is not printed, it mixes the delays and the timeouts
		{
			name:  "no average",
			stats: healthcheck.Stats{All: 10, Fail: 1, Deviation: 4330, Median: 526, Average: 2050, Latest: 402},
			want:  "#0 x [nil] STD=4.33s MED=526ms Latest=402ms FAIL=1/10",
		},
		// a node that was never checked has nothing to show
		{
			name:   "never checked",
			status: StatusUnknown,
			stats:  healthcheck.Stats{},
			want:   "#0 ? [nil] STD=None MED=None Latest=None FAIL=0/0",
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
		Stats:     healthcheck.Stats{All: 10, Deviation: 57, Median: 164, Latest: 155},
	}
	want := "STD=57ms(114ms) MED=164ms(328ms) Latest=155ms(310ms)"
	if got := node.String(); !strings.Contains(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	// the missing values stay missing, with or without a scale
	node = &Node{
		RTTSacale: 2,
		Stats:     healthcheck.Stats{All: 1, Fail: 1, Median: 15000},
	}
	want = "STD=None MED=None Latest=None"
	if got := node.String(); !strings.Contains(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
}
