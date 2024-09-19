package balancer_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/balancer"
	"github.com/sagernet/sing-box/common/healthcheck"
)

func TestNodeStatus(t *testing.T) {
	t.Parallel()
	var maxRTT healthcheck.RTT = healthcheck.Second
	var maxFailRate float32 = 0.2
	testCases := []struct {
		name   string
		status balancer.Status
		stats  healthcheck.Stats
	}{
		{
			"nil RTTStorage", balancer.StatusUnknown, healthcheck.Stats{
				All: 0, Fail: 0, Latest: 0, Average: 0,
			},
		},
		{
			"untested", balancer.StatusUnknown, healthcheck.Stats{
				All: 0, Fail: 0, Latest: 0, Average: 0,
			},
		},
		{
			"@max_rtt", balancer.StatusQualified, healthcheck.Stats{
				All: 10, Fail: 0, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"@max_fail", balancer.StatusQualified, healthcheck.Stats{
				All: 10, Fail: 2, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"@max_fail_2", balancer.StatusQualified, healthcheck.Stats{
				All: 5, Fail: 1, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"latest_fail", balancer.StatusDead, healthcheck.Stats{
				All: 10, Fail: 1, Latest: healthcheck.Failed, Average: healthcheck.Second,
			},
		},
		{
			"over max_fail", balancer.StatusAlive, healthcheck.Stats{
				All: 5, Fail: 2, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"over max_rtt", balancer.StatusAlive, healthcheck.Stats{
				All: 10, Fail: 0, Latest: healthcheck.Second, Average: 2 * healthcheck.Second,
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := &balancer.Node{Stats: tc.stats}
			node.CalcStatus(maxRTT, maxFailRate)
			if node.Status != tc.status {
				t.Errorf("want: %s, got: %s", tc.status, node.Status)
			}
		})
	}
}
