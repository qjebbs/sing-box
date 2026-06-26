package balancer

import (
	"testing"

	"github.com/sagernet/sing-box/service/healthcheck"
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
				All: 0, Fail: 0, Latest: 0, Average: 0,
			},
		},
		{
			"untested", StatusUnknown, healthcheck.Stats{
				All: 0, Fail: 0, Latest: 0, Average: 0,
			},
		},
		{
			"@max_rtt", StatusQualified, healthcheck.Stats{
				All: 10, Fail: 0, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"@max_fail", StatusQualified, healthcheck.Stats{
				All: 10, Fail: 2, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"@max_fail_2", StatusQualified, healthcheck.Stats{
				All: 5, Fail: 1, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"latest_fail", StatusDead, healthcheck.Stats{
				All: 10, Fail: 1, Latest: healthcheck.Failed, Average: healthcheck.Second,
			},
		},
		{
			"over max_fail", StatusAlive, healthcheck.Stats{
				All: 5, Fail: 2, Latest: healthcheck.Second, Average: healthcheck.Second,
			},
		},
		{
			"over max_rtt", StatusAlive, healthcheck.Stats{
				All: 10, Fail: 0, Latest: healthcheck.Second, Average: 2 * healthcheck.Second,
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
