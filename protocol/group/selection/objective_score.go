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
	// DefaultQuickTolerance is the default tolerance of quick, around the fastest node.
	// Its score is a delay of hundreds of milliseconds, so a difference worth switching
	// for is a share of a second: 50ms of a 500ms node.
	DefaultQuickTolerance = 50 * healthcheck.Millisecond
	// DefaultSteadyTolerance is the default tolerance of steady, around the steadiest
	// node. Its score approaches zero for a steady node, so the band is much narrower.
	DefaultSteadyTolerance = 10 * healthcheck.Millisecond
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
// steady are both one. They only differ in the statistic the score is made of, which is
// also what their default tolerance is sized for.
type ScoreObjective struct {
	*QualifiedObjective
	name string
	// tolerance is the band around the best node, in absolute milliseconds, see band.
	tolerance healthcheck.RTT
	minNodes  int
	maxNodes  int
	// baseFunc is the primary statistic of the objective, it is the first term of
	// the score. It is a delay of the checks that succeeded, see healthcheck.Stats.
	baseFunc func(node *Node) healthcheck.RTT
	// jitterFunc is added to the score as a penalty, it is nil when the base already is a
	// spread, since adding another one would rank the same thing twice.
	jitterFunc func(node *Node) healthcheck.RTT
}

// Name implements Objective.
func (o *ScoreObjective) Name() string {
	return o.name
}

func jitterRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.Jitter)
}

func meanDeviationRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.MeanDeviation)
}

func medianRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.Median)
}

// NewSteadyObjective returns a new ScoreObjective. It ranks the nodes by how much
// they stray from their own median, whatever that median is: a node of 500ms that stays
// there is steadier than a node of 100ms that swings, and the distance of the node is not
// part of the question. Bounding the distance is what max_rtt is for.
func NewSteadyObjective(options option.LoadBalancePickOptions) *ScoreObjective {
	objective := newScoreObjective(options, DefaultSteadyTolerance)
	objective.name = ObjectiveSteady
	objective.baseFunc = meanDeviationRTT
	return objective
}

// NewQuickObjective returns a new ScoreObjective. It ranks the nodes by what they
// typically cost: the median delay of the checks, plus the jitter, since a node that is
// near but unstable delays the traffic as a node that is simply slower does. The spread is
// the typical one, so a check that timed out once does not decide it, see
// healthcheck.Stats.MeanDeviation for the one that counts it.
func NewQuickObjective(options option.LoadBalancePickOptions) *ScoreObjective {
	objective := newScoreObjective(options, DefaultQuickTolerance)
	objective.name = ObjectiveQuick
	objective.baseFunc = medianRTT
	objective.jitterFunc = jitterRTT
	return objective
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
//	quick  = MED + JIT       what a node typically costs
//	steady = MeanDeviation   how much it strays from its own typical delay
//
// The jitter term is absent when the objective has no jitterFunc, which is the case for
// steady: its base is a spread already. See healthcheck.Stats.Cost for what an attempt
// costs on average, including the failures.
func (o *ScoreObjective) Score(node *Node) healthcheck.RTT {
	base := o.baseFunc(node)
	if o.jitterFunc == nil {
		// steady: the base is the spread itself
		return node.Stats.Cost(base, 0)
	}
	// quick: the typical delay plus the jitter it typically adds
	return node.Stats.Cost(base, o.jitterFunc(node))
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
// three times, and of a steady score by half again.
func (o *ScoreObjective) scoreStandardError(node *Node) float64 {
	n := float64(node.Stats.Succeeded())
	if n < 2 {
		// a single sample says nothing about the accuracy of its own estimate
		return float64(o.baseFunc(node))
	}
	if o.jitterFunc == nil {
		// steady: the score is the average distance to the median, and the standard error
		// of an average is the spread of the values it averages over √n
		return float64(node.ScaleRTT(node.Stats.DeviationSpread)) / math.Sqrt(n)
	}
	// quick: the score is the median plus the jitter, two estimates of the same samples,
	// each with its own error, combined by their variance
	medianStandardError := medianToError * float64(node.ScaleRTT(node.Stats.Spread)) / math.Sqrt(n)
	jitterStandardError := jitterToError * float64(node.ScaleRTT(node.Stats.DeviationSpread)) / math.Sqrt(n)
	return math.Sqrt(medianStandardError*medianStandardError + jitterStandardError*jitterStandardError)
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
// one: every node whose value is within tolerance, relative to the best value, is
// kept, so a node that is clearly worse is never selected just to fill a count.
// minNodes extends the selection to that count when the cluster is too small,
// maxNodes limits it when set.
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

// SortByLeast sorts nodes by least value from rttFunc and more.
func SortByLeast(nodes []*Node, rttFunc func(*Node) healthcheck.RTT) {
	sort.Slice(nodes, func(i, j int) bool {
		left := nodes[i]
		right := nodes[j]
		if left.Status != right.Status {
			return left.Status > right.Status
		}
		leftRTT, rightRTT := rttFunc(left), rttFunc(right)
		if leftRTT != rightRTT {
			// 0, 100
			if leftRTT == healthcheck.Failed {
				return false
			}
			// 100, 0
			if rightRTT == healthcheck.Failed {
				return true
			}
			// 100, 200
			return leftRTT < rightRTT
		}
		if left.Fail != right.Fail {
			return left.Fail < right.Fail
		}
		if left.All != right.All {
			return left.All < right.All
		}
		// order by random to avoid always selecting
		// the same nodes when all nodes are equal
		return left.rand > right.rand
	})
}

// band returns the width of the tolerance around the best node, the range in which another
// node counts as equally good: the tolerance of the objective plus a resolution margin.
//
// The margin is always added, because the tolerance describes the true difference while the
// comparison only sees observed values. Its scale is the jitter of the best node, not the
// standard error of its score: the margin decides who counts as equally good, and a margin
// that followed the full error of the score would be widened by a single slow check, letting
// one probe decide that a clearly worse node is equally good. The jitter is a median, so it
// describes the node while the margin stays put.
//
// The price is that the margin is narrower than the error of the score: measured on live
// windows, the standard error of a quick score is about 2.8 times the jitter over √n. The
// band is therefore a floor on the resolution, not a 95% one, and the tolerance is sized
// knowing that.
func (o *ScoreObjective) band(best *Node) float64 {
	band := float64(o.tolerance)
	if succeeded := float64(best.Stats.Succeeded()); succeeded >= 2 {
		// the typical spread of the node, the scale the reading of its score can be off by
		spread := float64(best.ScaleRTT(best.Jitter))
		band += resolutionSigma * spread / math.Sqrt(succeeded)
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
