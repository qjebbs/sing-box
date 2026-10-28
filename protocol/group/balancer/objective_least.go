package balancer

import (
	"math"
	"sort"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

var (
	_ Objective = (*LeastObjective)(nil)
	_ Keeper    = (*LeastObjective)(nil)
)

const (
	// DefaultTolerance is the default relative tolerance around the best node.
	DefaultTolerance float32 = 0.2
	// DefaultJitterWeight is the default weight of the standard deviation in the score.
	DefaultJitterWeight float32 = 1
	// toleranceFloor is the absolute floor of the tolerance, so tiny values, such as
	// the deviation of an always stable node, are not split by noise.
	toleranceFloor = 10 * healthcheck.Millisecond
	// hysteresisSigma is how many standard errors a node may exceed the selection
	// band by and still keep its existing connections. Two standard errors is the
	// usual 95% confidence, the excess is then not distinguishable from noise.
	hysteresisSigma = 2
)

// LeastObjective is the least load / least ping balancing objective. Both rank the
// nodes by Score, they only differ in the base statistic and whether the jitter is
// added to it.
type LeastObjective struct {
	*QualifiedObjective
	tolerance float32
	minNodes  int
	maxNodes  int
	// baseFunc is the primary statistic of the objective, it is the first term of
	// the score.
	baseFunc func(node *Node) healthcheck.RTT
	// jitterFunc is added to the score as a penalty, it is nil when baseFunc is the
	// jitter itself, since adding it again would only scale every score.
	jitterFunc   func(node *Node) healthcheck.RTT
	jitterWeight float32
}

func deviationRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.Deviation)
}

func medianRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.Median)
}

// NewLeastLoadObjective returns a new LeastObjective.
func NewLeastLoadObjective(options option.LoadBalancePickOptions) *LeastObjective {
	objective := newLeastObjective(options)
	// the standard deviation is the jitter itself, so jitter_weight is not used,
	// adding the deviation again would only scale every score by the same factor
	// objective.jitterWeight = 0
	objective.baseFunc = deviationRTT
	return objective
}

// NewLeastPingObjective returns a new LeastObjective.
func NewLeastPingObjective(options option.LoadBalancePickOptions) *LeastObjective {
	objective := newLeastObjective(options)
	objective.baseFunc = medianRTT
	objective.jitterFunc = deviationRTT
	objective.jitterWeight = float32Option(options.JitterWeight, DefaultJitterWeight)
	return objective
}

// newLeastObjective returns a new LeastObjective with the common options.
func newLeastObjective(options option.LoadBalancePickOptions) *LeastObjective {
	return &LeastObjective{
		QualifiedObjective: NewQualifiedObjective(),
		tolerance:          float32Option(options.Tolerance, DefaultTolerance),
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

// Score returns the cost of using the node, lower is better. It starts from the
// base value of the objective and adds the jitter, since an unstable node stalls
// connections:
//
//	score = base + jitterWeight * jitter
//
// The jitter term is absent when the objective has no jitterFunc, which is the
// case for leastload: its base is the deviation itself.
//
// Failures are not scored here, they are part of the base already: the statistics
// count a failed check with the delay of the timeout it waited for, see
// healthcheck.Storage.FailureDelay.
func (o *LeastObjective) Score(node *Node) healthcheck.RTT {
	return rttFromFloat(o.scoreOf(node))
}

func (o *LeastObjective) scoreOf(node *Node) float64 {
	score := float64(o.baseFunc(node))
	if o.jitterFunc != nil && o.jitterWeight > 0 {
		score += float64(o.jitterWeight) * float64(o.jitterFunc(node))
	}
	return score
}

// scoreStandardError returns the standard error of the score of the node, i.e. how
// much the score would move if the health checks were run again. It only depends on
// the number of samples and their deviation: the median of n samples has a standard
// error of 1.25σ/√n, and the deviation itself of σ/√(2(n-1)).
func (o *LeastObjective) scoreStandardError(node *Node) float64 {
	deviation := float64(node.ScaleRTT(node.Deviation))
	if node.All < 2 {
		// a single sample says nothing about the accuracy of its own estimate
		return deviation
	}
	n := float64(node.All)
	if o.jitterFunc == nil {
		// the score is the deviation
		return deviation / math.Sqrt(2*(n-1))
	}
	medianError := 1.25 * deviation / math.Sqrt(n)
	jitterError := float64(o.jitterWeight) * deviation / math.Sqrt(2*(n-1))
	return math.Sqrt(medianError*medianError + jitterError*jitterError)
}

// Keep implements Keeper. A node that left the selection is still kept while the
// excess over the selection band is within the noise of the two estimates, so the
// existing connections are not closed by a measurement that could go either way.
//
// The allowance is capped at the band itself: hysteresis is there to ride out noise,
// not to keep a node that is clearly worse, so a node never survives more than twice
// the tolerance.
func (o *LeastObjective) Keep(node *Node, best *Node) bool {
	if node.Status != best.Status {
		return false
	}
	bestScore := o.scoreOf(best)
	band := bestScore * float64(o.tolerance)
	if band < float64(toleranceFloor) {
		band = float64(toleranceFloor)
	}
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
func (o *LeastObjective) Rank(all []*Node) []*Node {
	ranked := o.QualifiedObjective.Rank(all)
	SortByLeast(ranked, o.Score)
	return ranked
}

// Select implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *LeastObjective) Select(ranked []*Node) []*Node {
	// nodes are either qualified, alive or all nodes
	nodes := o.QualifiedObjective.Select(ranked)
	if len(nodes) == 0 {
		return nil
	}
	return SelectCluster(nodes, o.tolerance, o.minNodes, o.maxNodes, o.Score)
}

// SelectCluster selects the nodes that are statistically close enough to the best
// one: every node whose value is within tolerance, relative to the best value, is
// kept, so a node that is clearly worse is never selected just to fill a count.
// minNodes extends the selection to that count when the cluster is too small,
// maxNodes limits it when set.
func SelectCluster(
	nodes []*Node, tolerance float32, minNodes int, maxNodes int,
	rttFunc func(node *Node) healthcheck.RTT,
) []*Node {
	if len(nodes) == 0 {
		return nil
	}
	best := rttFunc(nodes[0])
	toleranceRange := float32(best) * tolerance
	if toleranceRange < float32(toleranceFloor) {
		toleranceRange = float32(toleranceFloor)
	}
	limit := rttFromFloat(float64(best) + float64(toleranceRange))
	count := 1
	for count < len(nodes) {
		// Nodes are sorted by status first, never mix a fallback status in.
		if nodes[count].Status != nodes[0].Status {
			break
		}
		if rttFunc(nodes[count]) > limit {
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
