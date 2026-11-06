package selection

import (
	"math"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

const (
	// DefaultIdleTolerance is the default tolerance of idle, around the node at its best.
	// Its score is a share of the node's own floor expressed on the scale below, so the
	// same 50ms means 5% of that floor: the two objectives can share one value because
	// both scores are milliseconds, one of them on a fixed 1000ms line.
	DefaultIdleTolerance = 50 * healthcheck.Millisecond
	// idleScale expresses the dimensionless share of idle in milliseconds, so that its
	// score is read, compared and bounded like the score of quick. A line of 1000ms is
	// the reference: 5% over its own floor is 50ms of score, the same as the tolerance of
	// quick, which keeps one configured number meaningful for both.
	idleScale = 1000.0
	// idleBadWeight converts the share of bad checks into the same scale as the share of
	// excess delay: a node that is twice as slow on every check adds half the scale. The
	// share of bad checks is at most one and the excess share is on the order of one for a
	// node in trouble, so this is a scale rather than a fitted weight: it sets how many bad
	// checks weigh as much as one percent of queueing, not which of two clearly different
	// nodes comes first.
	idleBadWeight = 0.5
)

// idlePressure is how much longer the node typically waits than it does at its best, as a
// share of that best: the depth of its queueing over its own floor, not over the floor of
// another node. It carries no unit, so a node of 150ms and a node of 900ms are compared on
// the same number.
func idlePressure(node *Node) float64 {
	floor := float64(node.Floor)
	if floor <= 0 || float64(node.Median) <= floor {
		return 0
	}
	return float64(node.Median)/floor - 1
}

// idleBadShare is how often the node fell short of what it can do: the frequency of its
// queueing, next to the depth.
//
// A failure counts as one bad check. It is the extreme of the same defect — the node did not
// answer at all — and it is a share either way, so it needs no price of its own. What it
// cannot enter is the depth: a failure has no delay, and giving it the timeout would let one
// of thirty checks outweigh every difference in queueing.
//
// The share is over the attempts rather than over the ones that worked, so a node that fails
// often cannot hide behind the checks that succeeded.
func idleBadShare(node *Node) float64 {
	bad := 0.0
	if node.All > 0 {
		bad = float64(node.Bad) / float64(node.All)
	}
	// The sum can exceed one: Bad is a share of the checks that worked and the failure
	// share is of the attempts, and the long-run estimate stands in for failures the
	// window does not hold. The score is a share, so it is capped at one.
	return math.Min(1, bad+node.FailureShare())
}

// idleScore is the score of idle: the depth and the frequency of the queueing, added on one
// scale, see idleScale and idleBadWeight. Why the two shares are the whole question, and
// why the failures are counted rather than priced, is in NewIdleObjective.
//
// A round with a single check has no shares to add: it is read by its own delay instead.
func idleScore(node *Node) healthcheck.RTT {
	if node.Stats.Succeeded() < 2 {
		// One check cannot tell queueing from distance: the floor is that check itself, so
		// both shares are zero and every node ties at the smallest score there is, leaving
		// the group to pick among them at random. Rank by the delay instead, on half of it,
		// which is the order quick reads the same window in: a node whose one check came out
		// low must not look like the steadiest node there is.
		score := float64(node.Median) / 2
		if score < 1 {
			score = 1
		}
		return healthcheck.RTT(score + 0.5)
	}
	score := idleScale * (idlePressure(node) + idleBadWeight*idleBadShare(node))
	if score > float64(healthcheck.Timeout()) {
		return healthcheck.Timeout()
	}
	if score <= 0 {
		// a node at its floor all the time, which must stay measurable
		return healthcheck.Millisecond
	}
	return healthcheck.RTT(score + 0.5)
}

// IdleTerms returns the two shares the idle score is made of, for a report that has to
// show what the score was built from rather than only its total.
func IdleTerms(node *Node) (pressure float32, badShare float32) {
	return float32(idlePressure(node)), float32(idleBadShare(node))
}

// idleNoise is the scale one round of the score can be off by, see band: the error of the
// depth term, which is the error of a median divided by the floor. The frequency term is
// left out on purpose, see NewIdleObjective.
func idleNoise(node *Node) healthcheck.RTT {
	floor := float64(node.Floor)
	if floor <= 0 {
		return healthcheck.Millisecond
	}
	depthError := float64(medianToError) * float64(node.ScaleRTT(node.Jitter)) / floor
	return healthcheck.RTT(idleScale*depthError + 0.5)
}

// idleError is the error of idleScore: the depth is a median divided by the floor, so its
// error is the error of a median over the floor; the frequency is a share, so its error is
// the error of a share. The two are combined by their variance and scaled with the score.
//
// The spread of the samples is used here rather than the jitter, the opposite of idleNoise:
// this is how wrong the score of a known node can be, and the band, which has to stay
// narrow enough to tell nodes apart, is the one read off the jitter.
func idleError(node *Node) float64 {
	floor := float64(node.Floor)
	if floor <= 0 {
		return float64(idleScore(node))
	}
	n := float64(node.Stats.Succeeded())
	depthError := float64(medianToError) * float64(node.ScaleRTT(node.Stats.Spread)) / floor / math.Sqrt(n)
	bad := idleBadShare(node)
	frequencyError := idleBadWeight * math.Sqrt(bad*(1-bad)/math.Max(float64(node.All), 1))
	return idleScale * math.Hypot(depthError, frequencyError)
}

// NewIdleObjective returns a new ScoreObjective. It ranks the nodes by how far and how
// often they fall short of what they can do, see healthcheck.Stats.Floor: the depth of the
// queueing as a share of the node's own fastest, plus the share of checks that came out
// twice as slow as that fastest.
//
// Both terms are shares, so the distance of the node is not part of the question: a node of
// 150ms that doubles every third check ranks behind a node of 900ms that stays near its
// own floor. Bounding the distance is what max_rtt is for.
//
// The failures are not priced in, because the score is not a time the node costs: it is a
// share, and can be close to zero. They are counted as bad checks instead, see Stats.Bad.
//
// The frequency does not widen the band, see idleNoise: a node that may be keeping up with
// the best is a node worth waiting for, but a node that may be falling behind is not a node
// worth trying. Uncertainty about a share cannot buy either one, and at ten checks it is
// large enough to be worth more than the share itself: the half-width would double on the
// rounds where the best node happens to catch one bad check.
func NewIdleObjective(options option.LoadBalancePickOptions) *ScoreObjective {
	objective := newScoreObjective(options, DefaultIdleTolerance)
	objective.name = ObjectiveIdle
	objective.scoreFunc = idleScore
	objective.noiseFunc = idleNoise
	objective.errorFunc = idleError
	return objective
}
