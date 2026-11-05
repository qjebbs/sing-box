package selection

import (
	"math"
	"sort"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

var (
	_ Objective = (*ScoreObjective)(nil)
	_ Bander    = (*ScoreObjective)(nil)
	_ Keeper    = (*ScoreObjective)(nil)
	_ Clusterer = (*ScoreObjective)(nil)
)

const (
	// resolutionSigma is how many standard errors of the best score it takes to tell two
	// nodes apart: the difference of two estimates carries the error of both, √2 each,
	// and the comparison is taken at two standard errors.
	resolutionSigma = 2 * math.Sqrt2
	// medianToError is the standard error of a median in units of σ/√n. The asymptotic
	// variance of a median is 1/(4n·f(m)²), which is (1.2533σ)²/n for a normal sample: a
	// median uses only the rank of the samples, so it is a quarter less accurate than the
	// mean of the same samples.
	medianToError = 1.2533
	// jitterToError is the same for the jitter. It is a median of the deviations, scaled by
	// 1.4826 to a σ estimate, so its standard error is 1.4826 × 1.2533 times the standard
	// error of one deviation.
	jitterToError = 1.4826 * medianToError
	// hysteresisSigma is how many standard errors a node may exceed the selection
	// band by and still keep its existing connections. Two standard errors is the
	// usual 95% confidence, the excess is then not distinguishable from noise.
	hysteresisSigma = 2
)

// ScoreObjective is the objective of a group that ranks its nodes by a score: quick and
// idle are both one. They differ in the score itself, in the scale one check moves it by,
// which is what sizes their band, and in how wrong that score may be, which is what
// hysteresis allows.
type ScoreObjective struct {
	*QualifiedObjective
	name string
	// tolerance is the band around the best node, in absolute milliseconds, see band.
	tolerance healthcheck.RTT
	minNodes  int
	maxNodes  int
	// scoreFunc is the score of the node, the one number the objective ranks by. It is
	// also the base its band is sized around, see noiseFunc.
	scoreFunc func(node *Node) healthcheck.RTT
	// noiseFunc is the scale of one round of scoreFunc: how much a single check moves it.
	// The band is sized by it, so it is a typical deviation rather than the deviation
	// summed over the window, see band.
	noiseFunc func(node *Node) healthcheck.RTT
	// errorFunc is the standard error of scoreFunc, i.e. how wrong the score of a known
	// node may be, see scoreStandardError and Keep.
	errorFunc func(node *Node) float64
}

// Name implements Objective.
func (o *ScoreObjective) Name() string {
	return o.name
}

func meanDeviationRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.MeanDeviation)
}

// measurement returns the statistic of a node that needs more than one check to exist,
// absent when there is only one: a single check has no variation to report, and the value
// the implementation stores for it is a placeholder, not a measurement.
func measurement(node *Node, statistic func(*Node) healthcheck.RTT) healthcheck.RTT {
	if node.Succeeded() < 2 {
		return healthcheck.Failed
	}
	return statistic(node)
}

// spreadRTT is how far the checks are from their average: the scale the standard error of
// the score is measured in, not the one its band is sized by, see band.
func spreadRTT(node *Node) healthcheck.RTT {
	return measurement(node, func(node *Node) healthcheck.RTT { return node.ScaleRTT(node.Spread) })
}

// deviationSpreadRTT is how far the distances to the median are from their average: an
// uneven variation is a node that changed within the window rather than one that moves.
func deviationSpreadRTT(node *Node) healthcheck.RTT {
	return measurement(node, func(node *Node) healthcheck.RTT { return node.ScaleRTT(node.DeviationSpread) })
}

// newScoreObjective returns a new ScoreObjective with the common options, and the default
// tolerance of the objective for the value it leaves unset.
func newScoreObjective(options option.LoadBalancePickOptions, defaultTolerance healthcheck.RTT) *ScoreObjective {
	return &ScoreObjective{
		QualifiedObjective: NewQualifiedObjective(),
		tolerance:          toleranceOption(options.Tolerance, defaultTolerance),
		minNodes:           int(options.MinNodes),
		maxNodes:           int(options.MaxNodes),
	}
}

func toleranceOption(value badoption.Duration, defaultValue healthcheck.RTT) healthcheck.RTT {
	if value <= 0 {
		return defaultValue
	}
	return healthcheck.RTT(value.Build().Milliseconds())
}

// Score returns the cost of using the node, lower is better:
//
//	quick = MED + JIT                    what a node typically costs, failures priced in
//	idle  = 1000 × (pres + 0.5 × bad)    how far and how often it falls short of its floor
//
// See healthcheck.Stats.Cost for how a time is mixed with the failures, which is what
// quick does.
func (o *ScoreObjective) Score(node *Node) healthcheck.RTT {
	if node.Stats.All == 0 {
		// nothing was measured, there is no score to report
		return healthcheck.Failed
	}
	if node.Stats.Succeeded() == 0 {
		// nothing worked, the timeout is all that is known about the node
		return healthcheck.Timeout()
	}
	return o.scoreFunc(node)
}

// scoreOf returns the score as a float, for the noise model of Keep, which compares
// the scores rather than reporting them.
func (o *ScoreObjective) scoreOf(node *Node) float64 {
	return float64(o.Score(node))
}

// scoreStandardError returns the standard error of the score of the node, i.e. how much
// the score would move if the health checks were run again. It only depends on the number
// of samples and how far apart they are.
//
// It is computed from the spreads of the samples, not from the robust statistics the score
// is made of. A standard error is a spread over √n, and the spread it needs is the one a
// sum of squares is taken over; reading it off the jitter or the mean deviation instead
// assumes normal samples and a robust centre as accurate as σ, and no real window is. On
// live windows that assumption understated the standard error of a quick score by about
// three times, and of an idle score by half again.
//
// idle keeps the spread here even though its band is sized by the jitter: the band is a
// decision threshold that has to stay narrow enough to tell the nodes apart, this is how
// wrong the score of a known node may be. The two are then far apart, so the allowance of
// Keep is capped at the band and a node is kept within twice it, see Keep.
func (o *ScoreObjective) scoreStandardError(node *Node) float64 {
	if node.Stats.Succeeded() < 2 {
		// a single sample says nothing about the accuracy of its own estimate
		return float64(o.Score(node))
	}
	return o.errorFunc(node)
}

// Keep implements Keeper. A node that left the selection is still kept while the
// excess over the selection band is within the noise of the two estimates, so the
// existing connections are not closed by a measurement that could go either way.
//
// The allowance is capped at the band itself: hysteresis is there to ride out noise,
// not to keep a node that is clearly worse, so a node never survives more than twice
// the tolerance.
func (o *ScoreObjective) Keep(node *Node, best *Node) bool {
	if node.Status != best.Status {
		return false
	}
	bestScore := o.scoreOf(best)
	band := o.band(best)
	nodeError := o.scoreStandardError(node)
	bestError := o.scoreStandardError(best)
	noise := hysteresisSigma * math.Sqrt(nodeError*nodeError+bestError*bestError)
	if noise > band {
		noise = band
	}
	return o.scoreOf(node) <= bestScore+band+noise
}

func rttFromFloat(value float64) healthcheck.RTT {
	if value <= 0 {
		return healthcheck.Failed
	}
	if value >= math.MaxUint16 {
		return healthcheck.RTT(math.MaxUint16)
	}
	// round instead of truncate, so the penalty never loses a millisecond
	return healthcheck.RTT(value + 0.5)
}

// Rank implements Objective, the nodes of a better status come first, then the
// nodes are ordered by score.
func (o *ScoreObjective) Rank(all []*Node) []*Node {
	ranked := o.QualifiedObjective.Rank(all)
	SortByLeast(ranked, o.Score)
	return ranked
}

// Select implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *ScoreObjective) Select(ranked []*Node) []*Node {
	// nodes are either qualified, alive or all nodes
	nodes := o.QualifiedObjective.Select(ranked)
	if len(nodes) == 0 {
		return nil
	}
	return SelectCluster(nodes, o.minNodes, o.maxNodes, o.Score, o.Limit)
}

// SelectCluster selects the nodes that are statistically close enough to the best
// one: every node whose value is no higher than the limit of the best node is kept, so
// a node that is clearly worse is never selected just to fill a count. minNodes extends
// the selection to that count when the cluster is too small, maxNodes limits it when set.
func SelectCluster(
	nodes []*Node, minNodes int, maxNodes int,
	rttFunc func(node *Node) healthcheck.RTT,
	limit func(best *Node) healthcheck.RTT,
) []*Node {
	if len(nodes) == 0 {
		return nil
	}
	highest := limit(nodes[0])
	count := 1
	for count < len(nodes) {
		// Nodes are sorted by status first, never mix a fallback status in.
		if nodes[count].Status != nodes[0].Status {
			break
		}
		if rttFunc(nodes[count]) > highest {
			break
		}
		count++
	}
	if count < minNodes {
		count = min(minNodes, len(nodes))
	}
	if maxNodes > 0 && count > maxNodes {
		count = maxNodes
	}
	return nodes[:count]
}

// SortByLeast sorts the nodes by the least value from rttFunc, and by the rest of what is
// known about them when two values are equal.
//
// The equal case is not a detail of sorting: a group in a quiet period has most of its
// nodes at their own best, so equal values are the rule, and the order among them decides
// which node is read as the best one — the band of the group is sized by that node. The
// statistics therefore come next, each of them something that makes a node a better
// reference than another: the typical variation first, because the band is sized by it,
// then the spread the standard error is made of, then the variation the node cost in
// total, then how far it is above its own floor, then how evenly that variation spreads
// over the checks.
func SortByLeast(nodes []*Node, rttFunc func(*Node) healthcheck.RTT) {
	sort.Slice(nodes, func(i, j int) bool {
		left := nodes[i]
		right := nodes[j]
		if left.Status != right.Status {
			return left.Status > right.Status
		}
		if less, decided := lessRTT(rttFunc(left), rttFunc(right)); decided {
			return less
		}
		// A statistic that needs two checks to exist is absent with one, so a node of a
		// single check is decided on at the end of the list rather than on a zero its
		// measurement never produced.
		for _, statistic := range tieBreakers {
			if less, decided := lessRTT(statistic(left), statistic(right)); decided {
				return less
			}
		}
		if left.Fail != right.Fail {
			return left.Fail < right.Fail
		}
		if leftSucceeded, rightSucceeded := left.Succeeded(), right.Succeeded(); leftSucceeded != rightSucceeded {
			// the same estimate from more checks is the better founded one
			return leftSucceeded > rightSucceeded
		}
		// order by random to avoid always selecting
		// the same nodes when all nodes are equal
		return left.rand > right.rand
	})
}

// tieBreakers are the statistics that order two nodes of equal value, in the order they
// are read: the smaller value wins for each of them.
var tieBreakers = []func(*Node) healthcheck.RTT{
	// the typical variation, which the band is sized by: an unpredictable node as the
	// reference widens the band for every candidate
	func(node *Node) healthcheck.RTT { return measurement(node, jitterRTT) },
	// the spread, which the standard error of the score is made of
	spreadRTT,
	// the variation the node cost in total
	func(node *Node) healthcheck.RTT { return measurement(node, meanDeviationRTT) },
	// how far the node is above its own floor, which it can lose on its own
	func(node *Node) healthcheck.RTT {
		return measurement(node, func(node *Node) healthcheck.RTT {
			return node.ScaleRTT(healthcheck.RTT(idleScale * idlePressure(node)))
		})
	},
	// how evenly the variation is spread over the checks
	deviationSpreadRTT,
}

// lessRTT orders two delays and reports whether the order is decided: equal values leave
// it to the next statistic. A statistic of a node with fewer than two successful checks is
// not a measurement, and is compared as absent rather than as zero.
func lessRTT(left, right healthcheck.RTT) (less bool, decided bool) {
	if left == right {
		return false, false
	}
	if left == healthcheck.Failed {
		return false, true
	}
	if right == healthcheck.Failed {
		return true, true
	}
	return left < right, true
}

// band returns the width of the tolerance around the best node, the range in which another
// node counts as equally good: the tolerance of the objective plus a resolution margin.
//
// The margin is always added, because the tolerance describes the true difference while the
// comparison only sees observed values. Its scale is a typical deviation of the best node,
// not the standard error of its score: the margin decides who counts as equally good, and a
// margin that followed the full error of the score would be widened by a single slow check,
// letting one probe decide that a clearly worse node is equally good. A typical deviation is
// a median, so it describes the node while the margin stays put, see noiseFunc.
//
// The price is that the margin is narrower than the error of the score. On live windows the
// standard error of a quick score was about three times the jitter over √n, and of an idle
// score far more, since one slow check moves the spread of a node and not its jitter. A band
// sized by the spread then counts most of the qualified nodes as equally good and the group
// has no ranking left. The band is therefore a floor on the resolution, not a 95% one, and
// the tolerance is sized knowing that.
func (o *ScoreObjective) band(best *Node) float64 {
	band := float64(o.tolerance)
	samples := float64(best.Stats.Succeeded())
	if samples >= 2 {
		// the scale the reading of the score can be off by, see noiseFunc
		noise := float64(o.noiseFunc(best))
		band += resolutionSigma * noise / math.Sqrt(samples)
	}
	return band
}

// Tolerance implements Tolerancer. It returns the configured width of the band, the part
// of it that describes a difference worth switching for: the rest is the resolution
// margin, see band.
func (o *ScoreObjective) Tolerance() healthcheck.RTT {
	return o.tolerance
}

// MinNodes implements Clusterer.
func (o *ScoreObjective) MinNodes() int {
	return o.minNodes
}

// MaxNodes implements Clusterer.
func (o *ScoreObjective) MaxNodes() int {
	return o.maxNodes
}

// Limit implements Bander. It returns the highest score that still counts as equally
// good as best.
func (o *ScoreObjective) Limit(best *Node) healthcheck.RTT {
	score := o.Score(best)
	if score == healthcheck.Failed {
		return healthcheck.Failed
	}
	return rttFromFloat(float64(score) + o.band(best))
}
