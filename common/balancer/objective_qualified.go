package balancer

import "sort"

var _ Objective = (*QualifiedObjective)(nil)

// QualifiedObjective is the qualified balancing objective
type QualifiedObjective struct {
	*AliveObjective
}

// NewQualifiedObjective returns a new QualifiedObjective
func NewQualifiedObjective() *QualifiedObjective {
	return &QualifiedObjective{
		AliveObjective: NewAliveObjective(),
	}
}

// Filter implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *QualifiedObjective) Filter(all []*Node) []*Node {
	qulifaied := make([]*Node, 0, len(all))
	for _, node := range all {
		if node.Status == StatusQualified {
			qulifaied = append(qulifaied, node)
		}
	}
	if len(qulifaied) > 0 {
		return qulifaied
	}
	// fallback to alive nodes, AliveObjective will fallback to all nodes if no alive
	return o.AliveObjective.Filter(all)
}

// Sort implements Objective.
func (o *QualifiedObjective) Sort(all []*Node) {
	sort.Slice(all, func(i, j int) bool {
		return all[i].Status > all[j].Status
	})
}
