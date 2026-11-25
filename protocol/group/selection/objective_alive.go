package selection

import "sort"

var _ Objective = (*AliveObjective)(nil)

// AliveObjective is the alive nodes objective
//
// It is not a Printer: the statistics it is read with are the ones every objective shares,
// see Node.String. The status it ranks by is decided by max_rtt and the recent failure
// weight, which are compared with the median and the failures of the node, and the latest
// delay is what says how the node is doing now.
type AliveObjective struct{}

// NewAliveObjective returns a new AliveObjective
func NewAliveObjective() *AliveObjective {
	return &AliveObjective{}
}

// Name implements Objective.
func (o *AliveObjective) Name() string {
	return ObjectiveAlive
}

// Rank implements Objective, the nodes of a better status come first.
func (o *AliveObjective) Rank(all []*Node) []*Node {
	ranked := make([]*Node, len(all))
	copy(ranked, all)
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Status > ranked[j].Status
	})
	return ranked
}

// Select implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *AliveObjective) Select(ranked []*Node) []*Node {
	count := 0
	for count < len(ranked) && ranked[count].Status != StatusDead {
		count++
	}
	if count == 0 {
		// every node is dead, they are still worth a try
		return ranked
	}
	return ranked[:count]
}
