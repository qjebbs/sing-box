package healthcheck_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/healthcheck"
)

func TestDuration(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		value healthcheck.RTT
		want  string
	}{
		{healthcheck.Failed, "0ms"},
		{healthcheck.RTT(1), "1ms"},
		{healthcheck.RTT(1000), "1000ms"},
		{healthcheck.RTT(1101), "1.10s"},
	}
	for _, tc := range testCases {
		if got := tc.value.String(); got != tc.want {
			t.Errorf("Duration.String() = %v, want %v", got, tc.want)
		}
	}
}
