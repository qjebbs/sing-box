package healthcheck

import (
	"math"
	"time"
)

// failurePrior is the evidence the estimate starts with, in checks: ten clean ones. A cold
// start is a node nobody has seen fail, and the first check must not outweigh the ten that
// follow it — a node whose very first check fails during a network blip would otherwise be
// reported as failing every time for an hour. The prior is weighted like any other evidence
// and fades with it, so once the history is long enough it makes no difference at all.
const failurePrior = 10

// failureTimeConstant is how far back the long-run failure rate looks. It is a duration
// rather than a number of checks on purpose: the same number of checks covers a different
// stretch of time at 1m and at 5m, and this estimate is meant to describe the node over
// time rather than over a count.
//
// It is long enough to remember a node that fails in bursts — the failure a ten-check
// window has already forgotten — and short enough to forget an outage that ended: a node
// that failed for the better part of an hour is clean again about that much later.
const failureTimeConstant = 45 * time.Minute

// failureRate is how often the node has failed over the last few failureTimeConstant, as a
// continuous-time exponential estimate. It outlives the window: a window can be clean while
// the node is not reliable, and the checks it holds cannot tell a node that fails in bursts
// from one that failed once and recovered.
//
// It is weighted by wall-clock time rather than by check: the weight of a check is
// exp(-age/τ), which is the only weighting under which two stretches of time can be added.
// A check folds in as
//
//	decay = exp(-Δt / τ)
//	sum    = decay*sum + outcome
//	weight = decay*weight + 1
//	rate   = sum / weight
//
// so a run of checks a few seconds apart moves the estimate by the time it covers, not by
// the number of checks in it. The rate of a node that has been failing for a minute is one
// minute of a τ-long memory, however many probes that minute held.
//
// It is not thread safe, like the storage that holds it.
type failureRate struct {
	// sum is the weighted number of failed checks, and weight the weighted number of all
	// checks, including the prior; their ratio is the estimate. Keeping the two apart is
	// what lets a cold start carry the prior rather than start from one check.
	sum    float64
	weight float64
	// at is when the estimate was last brought forward. A zero at means nothing has been
	// observed yet and weight is still the prior alone.
	at time.Time
}

// observe folds one check in.
func (f *failureRate) observe(at time.Time, failed bool) {
	outcome := 0.0
	if failed {
		outcome = 1
	}
	if f.at.IsZero() {
		f.weight = failurePrior
		f.at = at
	}
	elapsed := at.Sub(f.at)
	if elapsed < 0 {
		// the clock stepped backwards: nothing has decayed, and this check is the newer
		elapsed = 0
	}
	decay := math.Exp(-elapsed.Seconds() / failureTimeConstant.Seconds())
	f.sum = decay*f.sum + outcome
	f.weight = decay*f.weight + 1
	f.at = at
}

// rate returns the estimate.
func (f *failureRate) rate() float64 {
	if f.weight <= 0 {
		return 0
	}
	return f.sum / f.weight
}

// ReplayFailureRate returns the estimate the storage would hold after the checks, which are
// newest first like the history they come from. It is how a window cut out of that history
// is given the same prior the group had when the window was current.
func ReplayFailureRate(checks []*History) float64 {
	var rate failureRate
	for i := len(checks) - 1; i >= 0; i-- {
		rate.observe(checks[i].Time, checks[i].Delay == Failed)
	}
	return rate.rate()
}
