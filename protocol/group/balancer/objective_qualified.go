package balancer

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

// Rank implements Objective, the nodes of a better status come first.
func (o *QualifiedObjective) Rank(all []*Node) []*Node {
	return o.AliveObjective.Rank(all)
}

// Select implements Objective.
// NOTICE: be aware of the coding convention of this function
func (o *QualifiedObjective) Select(ranked []*Node) []*Node {
	count := 0
	for count < len(ranked) && ranked[count].Status == StatusQualified {
		count++
	}
	if count > 0 {
		return ranked[:count]
	}
	// fallback to alive nodes, AliveObjective will fallback to all nodes if no alive
	return o.AliveObjective.Select(ranked)
}
