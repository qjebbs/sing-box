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

// longFailureTimeConstant is τ_L of the docs: how far back the long-run failure rate looks.
// It is a duration rather than a number of checks on purpose: the same number of checks
// covers a different stretch of time at 1m and at 5m, and this estimate is meant to describe
// the node over time rather than over a count.
//
// It is long enough to remember a node that fails in bursts — the failure a ten-check
// window has already forgotten — and short enough to forget an outage that ended: a node
// that failed for the better part of an hour is clean again about that much later.
const longFailureTimeConstant = 45 * time.Minute

// nearFailureTimeConstant is τ_N of the docs: how far back the near failure weight looks. It
// is the short half of the failure memory: a failure stops counting as a whole one after
// about this long, and a burst adds up, so a long outage keeps the node out longer than a
// single failure. It is a wall-clock duration like longFailureTimeConstant, not a number of
// checks: the same five minutes cover a different count at 1m and at 5m, and the gate is
// meant to describe how recently the node failed rather than how often it was checked.
const nearFailureTimeConstant = 5 * time.Minute

// decayedWeight is wᵢ(τ) of the docs: the weight of a check after age has passed,
// exp(-age/τ). It is the kernel both failure horizons share — FAIL_NEAR/FAILR_NEAR and
// FAILR_LONG differ in τ and in which checks they are taken over, not in how a check's age
// counts. A clock that stepped backwards has not aged the check, so its weight stays one.
func decayedWeight(age time.Duration, tau time.Duration) float64 {
	if age < 0 {
		age = 0
	}
	return math.Exp(-age.Seconds() / tau.Seconds())
}

// nearFailureWeightAt returns wᵢ(τ_N) of the docs: the weight of a check at now,
// exp(-(now-at)/τ_N).
func nearFailureWeightAt(at time.Time, now time.Time) float64 {
	return decayedWeight(now.Sub(at), nearFailureTimeConstant)
}

// longRunFailure is the state of FAILR_LONG = F(τ_L) of the docs: how often the node has
// failed over the last few longFailureTimeConstant, as a continuous-time exponential
// estimate. It outlives the window: a window can be clean while the node is not reliable,
// and the checks it holds cannot tell a node that fails in bursts from one that failed once
// and recovered.
//
// It is weighted by wall-clock time rather than by check: the weight of a check is
// exp(-age/τ), which is the only weighting under which two stretches of time can be added.
// A check folds in as
//
//	decay = exp(-Δt / τ_L)
//	S'    = decay*S + f
//	W'    = decay*W + 1
//	FAILR_LONG = S / W
//
// so a run of checks a few seconds apart moves the estimate by the time it covers, not by
// the number of checks in it. The rate of a node that has been failing for a minute is one
// minute of a τ_L-long memory, however many probes that minute held.
//
// It is not thread safe, like the storage that holds it.
type longRunFailure struct {
	// sum is S(τ_L), the weighted number of failed checks, and weight W(τ_L), the weighted
	// number of all checks, including the prior; their ratio is the estimate. Keeping the
	// two apart is what lets a cold start carry the prior rather than start from one check.
	sum    float64
	weight float64
	// at is when the estimate was last brought forward. A zero at means nothing has been
	// observed yet and weight is still the prior alone.
	at time.Time
}

// observe folds one check in.
func (f *longRunFailure) observe(at time.Time, failed bool) {
	outcome := 0.0
	if failed {
		outcome = 1
	}
	if f.at.IsZero() {
		f.weight = failurePrior
		f.at = at
	}
	decay := decayedWeight(at.Sub(f.at), longFailureTimeConstant)
	f.sum = decay*f.sum + outcome
	f.weight = decay*f.weight + 1
	f.at = at
}

// rate returns the estimate: FAILR_LONG = F(τ_L) = S / W.
func (f *longRunFailure) rate() float64 {
	if f.weight <= 0 {
		return 0
	}
	return f.sum / f.weight
}

// estimate returns S(τ_L), W(τ_L) and the time they were brought forward to, which is the
// whole of the state: the next observe folds a check into them by decaying and adding.
// Nothing has been observed while at is zero, which is the state of a fresh estimate.
func (f *longRunFailure) estimate() (sum float64, weight float64, at time.Time) {
	return f.sum, f.weight, f.at
}

// restore sets the estimate back to a stored one. It reports false when the state is not one
// observe could have produced: a weight without a time, a sum larger than the weight, or a
// time in the future.
//
// An estimate this run has already brought forward is newer than any record: the record would
// undo the checks it has folded in since, so it is left alone. Reading the same record twice is
// then nothing.
func (f *longRunFailure) restore(sum float64, weight float64, at time.Time) bool {
	if at.IsZero() || weight <= 0 || sum < 0 || sum > weight || time.Since(at) < -time.Minute {
		return false
	}
	if !f.at.IsZero() && !at.After(f.at) {
		return false
	}
	f.sum, f.weight, f.at = sum, weight, at
	return true
}

// ReplayFailureRateLong returns FAILR_LONG the storage would hold after the checks, which are
// newest first like the history they come from. It is how a window cut out of that history
// is given the same prior the group had when the window was current.
func ReplayFailureRateLong(checks []*History) float64 {
	var rate longRunFailure
	for i := len(checks) - 1; i >= 0; i-- {
		rate.observe(checks[i].Time, checks[i].Delay == Failed)
	}
	return rate.rate()
}
