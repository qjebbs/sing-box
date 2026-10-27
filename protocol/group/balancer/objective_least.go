package balancer

import (
	"sort"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

var _ Objective = (*LeastObjective)(nil)

const (
	// DefaultTolerance is the default relative tolerance around the best node.
	DefaultTolerance float32 = 0.2
	// toleranceFloor is the absolute floor of the tolerance, so tiny values, such as
	// the deviation of an always stable node, are not split by noise.
	toleranceFloor = 10 * healthcheck.Millisecond
)

// LeastObjective is the least load / least ping balancing objective
type LeastObjective struct {
	*QualifiedObjective
	tolerance float32
	minNodes  int
	maxNodes  int
	rttFunc   func(node *Node) healthcheck.RTT
}

// NewLeastLoadObjective returns a new LeastObjective.
func NewLeastLoadObjective(options option.LoadBalancePickOptions) *LeastObjective {
	return NewLeastObjective(options, func(node *Node) healthcheck.RTT {
		return node.ScaleRTT(node.Deviation)
	})
}

// NewLeastPingObjective returns a new LeastObjective.
func NewLeastPingObjective(options option.LoadBalancePickOptions) *LeastObjective {
	return NewLeastObjective(options, func(node *Node) healthcheck.RTT {
		return node.ScaleRTT(node.Median)
	})
}

// NewLeastObjective returns a new LeastObjective
func NewLeastObjective(options option.LoadBalancePickOptions, rttFunc func(node *Node) healthcheck.RTT) *LeastObjective {
	tolerance := options.Tolerance
	if tolerance <= 0 {
		tolerance = DefaultTolerance
	}
	return &LeastObjective{
		QualifiedObjective: NewQualifiedObjective(),
		tolerance:          tolerance,
		minNodes:           int(options.MinNodes),
		maxNodes:           int(options.MaxNodes),
		rttFunc:            rttFunc,
	}
}

// Filter implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *LeastObjective) Filter(all []*Node) []*Node {
	// nodes are either qualified, alive or all nodes
	nodes := o.QualifiedObjective.Filter(all)
	o.Sort(nodes)
	return SelectCluster(nodes, o.tolerance, o.minNodes, o.maxNodes, o.rttFunc)
}

// Sort implements Objective.
func (o *LeastObjective) Sort(all []*Node) {
	SortByLeast(all, o.rttFunc)
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
	toleranceRange := healthcheck.RTT(float32(best) * tolerance)
	if toleranceRange < toleranceFloor {
		toleranceRange = toleranceFloor
	}
	limit := best + toleranceRange
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
