package balancer

import "sort"

var _ Objective = (*AliveObjective)(nil)

// AliveObjective is the alive nodes objective
type AliveObjective struct{}

// NewAliveObjective returns a new AliveObjective
func NewAliveObjective() *AliveObjective {
	return &AliveObjective{}
}

// Filter implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *AliveObjective) Filter(all []*Node) []*Node {
	alive := make([]*Node, 0, len(all))
	for _, node := range all {
		if node.Status != StatusDead {
			alive = append(alive, node)
		}
	}
	if len(alive) > 0 {
		return alive
	}
	// fallback to all nodes
	alive = make([]*Node, len(all))
	copy(alive, all)
	return all
}

// Sort implements Objective.
func (o *AliveObjective) Sort(all []*Node) {
	sort.Slice(all, func(i, j int) bool {
		return all[i].Status > all[j].Status
	})
}
