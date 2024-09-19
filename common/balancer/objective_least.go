package balancer

import (
	"sort"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

var _ Objective = (*LeastObjective)(nil)

// LeastObjective is the least load / least ping balancing objective
type LeastObjective struct {
	*QualifiedObjective
	expected  int
	baselines []healthcheck.RTT
	rttFunc   func(node *Node) healthcheck.RTT
}

// NewLeastObjective returns a new LeastObjective
func NewLeastObjective(sampling uint, options option.LoadBalancePickOptions, rttFunc func(node *Node) healthcheck.RTT) *LeastObjective {
	return &LeastObjective{
		QualifiedObjective: NewQualifiedObjective(),
		expected:           int(options.Expected),
		baselines:          healthcheck.RTTsOf(options.Baselines),
		rttFunc:            rttFunc,
	}
}

// Filter implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *LeastObjective) Filter(all []*Node) []*Node {
	// nodes are either qualified, alive or all nodes
	nodes := o.QualifiedObjective.Filter(all)
	o.Sort(nodes)
	// LeastNodes will always select at least one node
	return LeastNodes(
		nodes,
		o.expected, o.baselines,
		o.rttFunc,
	)
}

// Sort implements Objective.
func (o *LeastObjective) Sort(all []*Node) {
	SortByLeast(all, o.rttFunc)
}

// LeastNodes filters ordered nodes according to `baselines` and `expected`.
//  1. baseline: nil, expected: 0: selects top one node.
//  2. baseline: nil, expected > 0: selects `expected` number of nodes.
//  3. baselines: [...], expected > 0: select `expected` number of nodes, and also those near them according to baselines.
//  4. baselines: [...], expected <= 0: go through all baselines until find selects, if not, select the top one.
func LeastNodes(
	nodes []*Node, expected int, baselines []healthcheck.RTT,
	rttFunc func(node *Node) healthcheck.RTT,
) []*Node {
	if len(nodes) == 0 {
		return nil
	}
	availableCount := len(nodes)
	if expected > availableCount {
		return nodes
	}
	if expected <= 0 {
		expected = 1
	}
	if len(baselines) == 0 {
		return nodes[:expected]
	}

	count := 0
	// go through all base line until find expected selects
	for i := 0; i < len(baselines); i++ {
		baseline := baselines[i]
		curStatus := nodes[count].Status
		for j := count; j < availableCount; j++ {
			if nodes[j].Status != curStatus {
				// if status changed, reset baseline index
				i = -1
				break
			}
			if rttFunc(nodes[j]) >= baseline {
				break
			}
			count = j + 1
		}
		// don't continue if find expected selects
		if count >= expected {
			break
		}
	}
	if count < expected {
		count = expected
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
