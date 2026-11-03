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
)

const (
	// DefaultTolerance is the default relative tolerance of quick, around the best node.
	DefaultTolerance float32 = 0.2
	// DefaultJitterTolerance is the default absolute tolerance of steady, around the
	// steadiest node.
	DefaultJitterTolerance = 10 * healthcheck.Millisecond
	// resolutionSigma is how many standard errors of the best score it takes to tell two
	// nodes apart: the difference of two estimates carries the error of both, √2 each,
	// and the comparison is taken at two standard errors.
	resolutionSigma = 2 * math.Sqrt2
	// hysteresisSigma is how many standard errors a node may exceed the selection
	// band by and still keep its existing connections. Two standard errors is the
	// usual 95% confidence, the excess is then not distinguishable from noise.
	hysteresisSigma = 2
)

// ScoreObjective is the objective of a group that ranks its nodes by a score: quick and
// steady are both one. They only differ in the base statistic, and whether the jitter is
// added to it.
type ScoreObjective struct {
	*QualifiedObjective
	name string
	// tolerance is the relative band of quick, jitterTolerance the absolute band of steady.
	// Which one is used follows the objective, see band.
	tolerance       float32
	jitterTolerance healthcheck.RTT
	minNodes        int
	maxNodes        int
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
	objective := newScoreObjective(options)
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
	objective := newScoreObjective(options)
	objective.name = ObjectiveQuick
	objective.baseFunc = medianRTT
	objective.jitterFunc = jitterRTT
	return objective
}

// newScoreObjective returns a new ScoreObjective with the common options.
func newScoreObjective(options option.LoadBalancePickOptions) *ScoreObjective {
	return &ScoreObjective{
		QualifiedObjective: NewQualifiedObjective(),
		tolerance:          float32Option(options.RTTTolerance, DefaultTolerance),
		jitterTolerance:    jitterToleranceOption(options.JitterTolerance),
		minNodes:           int(options.MinNodes),
		maxNodes:           int(options.MaxNodes),
	}
}

func float32Option(value float32, defaultValue float32) float32 {
	if value <= 0 {
		return defaultValue
	}
	return value
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
		return node.Stats.Cost(base, 0)
	}
	return node.Stats.Cost(base, o.jitterFunc(node))
}

// scoreOf returns the score as a float, for the noise model of Keep, which compares
// the scores rather than reporting them.
func (o *ScoreObjective) scoreOf(node *Node) float64 {
	return float64(o.Score(node))
}

// scoreStandardError returns the standard error of the score of the node, i.e. how much
// the score would move if the health checks were run again. It only depends on the number
// of samples and their spread.
func (o *ScoreObjective) scoreStandardError(node *Node) float64 {
	n := float64(node.Stats.Succeeded())
	if n < 2 {
		// a single sample says nothing about the accuracy of its own estimate
		return float64(o.baseFunc(node))
	}
	if o.jitterFunc == nil {
		// the score is the mean distance to the median, and the standard error of a mean
		// of n samples shrinks with √n
		return float64(o.baseFunc(node)) / math.Sqrt(n)
	}
	// the score is the median and its jitter: the median of n samples has a standard
	// error of 1.25σ/√n, the jitter of about σ/√(2(n-1))
	jitter := float64(node.ScaleRTT(node.Jitter))
	medianError := 1.25 * jitter / math.Sqrt(n)
	jitterError := jitter / math.Sqrt(2*(n-1))
	return math.Sqrt(medianError*medianError + jitterError*jitterError)
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

// band returns the width of the tolerance around the best node, the range in which
// another node counts as equally good.
//
// The two objectives measure different quantities, so their bands are in different
// units and neither can be a share of the other:
//
//   - quick compares delays, which are far from zero, so a share of the best one is
//     a meaningful band: 20% of 170ms is 34ms.
//   - steady compares a spread that approaches zero for a steady node, so a share of
//     it would be smaller than the measurement can resolve: 20% of 2ms is 0.4ms. The band
//     is absolute milliseconds instead, and a resolution margin is added to it, because
//     the configured value describes the true difference while the comparison only sees
//     the observed one.
func (o *ScoreObjective) band(best *Node) float64 {
	if o.jitterFunc != nil {
		return o.scoreOf(best) * float64(o.tolerance)
	}
	band := float64(o.jitterTolerance)
	if succeeded := float64(best.Stats.Succeeded()); succeeded >= 2 {
		// the standard error of the mean deviation, which is what the score is made of
		spread := float64(best.ScaleRTT(best.MeanDeviation))
		band += resolutionSigma * spread / math.Sqrt(succeeded)
	}
	return band
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

// jitterToleranceOption returns the absolute band of steady, the default when it is
// not set. A negative value would make the band narrower than the measurement.
func jitterToleranceOption(value badoption.Duration) healthcheck.RTT {
	if value <= 0 {
		return DefaultJitterTolerance
	}
	return healthcheck.RTT(value.Build().Milliseconds())
}
