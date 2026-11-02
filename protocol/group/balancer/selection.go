package balancer

import (
	"github.com/sagernet/sing-box/adapter"
)

// Selection is how a group turns the health of its nodes into the node that serves a
// connection. It is the only thing a group has to decide for itself, the balancer owns
// the nodes, their ranking and the cache.
type Selection interface {
	// Objective returns the ranking the selection is built on.
	Objective() Objective
	// Select returns the nodes new connections may use, and the node to keep for the
	// next ranking, empty when there is none to keep.
	//
	// It is called when the ranking is rebuilt, under the lock of the balancer, so an
	// implementation may keep the choice it made. previous is the node kept by the
	// previous ranking of the same network, empty when there was none.
	Select(ranked []*Node, previous string) (selected []*Node, chosen string)
	// Pick returns the node that serves the connection, from the nodes Select returned.
	// It is called for every connection, so it must be cheap.
	Pick(all, selected []*Node, metadata *adapter.InboundContext) *Node
	// String describes the selection, for the log.
	String() string
}

// Balancing is the selection of a load balance group: the objective selects a cluster of
// nodes, and the strategy picks one of them for each connection.
type Balancing struct {
	objective Objective
	strategy  Strategy
}

func NewBalancing(objective Objective, strategy Strategy) *Balancing {
	return &Balancing{objective: objective, strategy: strategy}
}

func (s *Balancing) Objective() Objective {
	return s.objective
}

func (s *Balancing) Select(ranked []*Node, _ string) ([]*Node, string) {
	return s.objective.Select(ranked), ""
}

func (s *Balancing) Pick(all, selected []*Node, metadata *adapter.InboundContext) *Node {
	return s.strategy.Pick(all, selected, metadata)
}

func (s *Balancing) String() string {
	return s.objective.Name() + "/" + s.strategy.Name()
}

// Single is the selection of a group that uses one node at a time, like the url test
// group. It keeps the node it chose while its score is within the tolerance of the best
// one: a change takes every new connection with it, so a difference that is not worth
// it keeps the node where it is.
type Single struct {
	objective Objective
	tolerance float32
}

func NewSingle(objective Objective, tolerance float32) *Single {
	return &Single{
		objective: objective,
		tolerance: float32Option(tolerance, DefaultTolerance),
	}
}

func (s *Single) Objective() Objective {
	return s.objective
}

func (s *Single) Select(ranked []*Node, previous string) ([]*Node, string) {
	selected := s.objective.Select(ranked)
	if len(selected) == 0 {
		return nil, ""
	}
	best := selected[0]
	scorer, isScorer := s.objective.(Scorer)
	if !isScorer {
		return []*Node{best}, best.Tag()
	}
	if previous != "" && previous != best.Tag() {
		for _, node := range ranked {
			if node.Tag() != previous {
				continue
			}
			if node.Status == best.Status && scorer.Score(node) <= clusterLimit(scorer.Score(best), s.tolerance) {
				return []*Node{node}, previous
			}
			break
		}
	}
	return []*Node{best}, best.Tag()
}

func (s *Single) Pick(_, selected []*Node, _ *adapter.InboundContext) *Node {
	if len(selected) == 0 {
		return nil
	}
	// Select returns one node for this selection.
	return selected[0]
}

func (s *Single) String() string {
	return s.objective.Name() + "/single"
}
