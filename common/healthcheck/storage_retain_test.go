package healthcheck

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

func newConfigurable(options *option.HealthCheckOptions) *HealthCheck {
	return &HealthCheck{
		options: options,
		Storage: NewStorages(StorageOptions{
			Sampling: options.Sampling,
			Validity: time.Duration(options.Sampling+1) * time.Duration(options.Interval),
			Interval: time.Duration(options.Interval),
		}),
	}
}

func TestConfigureHistoryChangesTheWindow(t *testing.T) {
	t.Parallel()
	// Changing the sampling is changing the window the ranking reads, and it has to happen
	// without a restart and without losing the checks already collected.
	check := newConfigurable(&option.HealthCheckOptions{
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	})
	for i := 0; i < 10; i++ {
		check.Storage.Put("node", RTT(100+i))
	}
	if got := check.Storage.Stats("node").All; got != 10 {
		t.Fatalf("want the window of ten checks, got %d", got)
	}

	applied := check.ConfigureHistory(6, 4)
	if applied.Sampling != 6 || applied.Retained != 10 {
		t.Fatalf("want six checks read out of ten kept, got %v", applied)
	}
	if applied.Validity != 7*2*time.Minute {
		t.Fatalf("the window kept its old bound: %v", applied.Validity)
	}
	if got := check.Storage.Stats("node").All; got != 6 {
		t.Fatalf("the statistics still read %d checks", got)
	}
	// the four checks behind the window are still there, so growing it back costs nothing
	if got := len(check.Storage.All("node")); got != 10 {
		t.Fatalf("the resize dropped collected checks: %d left", got)
	}
	if applied := check.ConfigureHistory(10, 0); applied.Sampling != 10 || applied.Retained != 10 {
		t.Fatalf("want the window back at ten, got %v", applied)
	}
	if got := check.Storage.Stats("node").All; got != 10 {
		t.Fatalf("want the ten checks back, got %d", got)
	}
}

func TestConfigureHistoryKeepsTheWindow(t *testing.T) {
	t.Parallel()
	// Asking for extras alone must keep the window in use rather than fall back to the one
	// in the configuration, which would undo a sampling that was just set.
	check := newConfigurable(&option.HealthCheckOptions{
		Sampling: 10,
		Interval: badoption.Duration(time.Minute),
	})
	check.ConfigureHistory(20, 0)
	applied := check.ConfigureHistory(0, 30)
	if applied.Sampling != 20 || applied.Retained != 50 {
		t.Fatalf("want twenty checks read and fifty kept, got %v", applied)
	}
	// and asking for neither leaves the history keeping the window alone
	applied = check.ConfigureHistory(0, 0)
	if applied.Sampling != 20 || applied.Retained != 20 {
		t.Fatalf("want the window alone, got %v", applied)
	}
}

func TestConfigureHistoryLeavesRoomForTheSpacing(t *testing.T) {
	t.Parallel()
	// The age bound of the history is a staleness guard, not a second size: the ring
	// decides how many checks are kept. It has to leave room for rounds that run longer
	// than the interval, or asking for N checks would quietly deliver fewer.
	check := newConfigurable(&option.HealthCheckOptions{
		Sampling: 10,
		Interval: badoption.Duration(2 * time.Minute),
	})
	applied := check.ConfigureHistory(0, 20)
	if applied.Sampling != 10 || applied.Retained != 30 {
		t.Fatalf("want ten checks read and thirty kept, got %v", applied)
	}
	span := time.Duration(applied.Retained) * 2 * time.Minute
	if applied.Retention < 2*span {
		t.Fatalf("the history would drop checks before its capacity: bound %v for a span of %v",
			applied.Retention, span)
	}
	// The window keeps its own, tighter bound: it describes the node now, and nothing
	// about the kept history changes that.
	if applied.Validity != 11*2*time.Minute {
		t.Fatalf("the window moved with the history: %v", applied.Validity)
	}
}
