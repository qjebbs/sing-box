// Package selection ranks the nodes of a group and chooses the ones that serve its
// connections. It knows nothing about providers, health checks or connections: it reads
// the statistics of a node and returns the nodes a connection may use.
package selection

import (
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common"
)

// Logger is the log a selection describes the nodes of its group to.
type Logger interface {
	Info(args ...any)
}

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
	// LogNodes writes the statistics of the nodes to the log of the group. all is every
	// node of the group, selected the ones Select returned, so a selection lays out what
	// it picks among the way that reads best for it.
	LogNodes(logger Logger, all, selected []*Node)
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

// LogNodes lists the nodes the group picks among first, the rest after them: a balancing
// group takes every connection from the former, so the latter only matter as the reason
// a node is not one of them.
func (s *Balancing) LogNodes(logger Logger, all, selected []*Node) {
	ranked := s.objective.Rank(all)
	var best *Node
	if len(ranked) > 0 {
		best = ranked[0]
	}
	logger.Info(s.String(), ", ", len(selected), " of ", len(all), " nodes available",
		clusterDescription(s.objective, best))
	selectedTags := make(map[string]bool, len(selected))
	for _, node := range selected {
		selectedTags[node.Tag()] = true
	}
	available := make([]*Node, 0, len(selected))
	unavailable := make([]*Node, 0, len(ranked)-len(selected))
	for _, node := range ranked {
		if selectedTags[node.Tag()] {
			available = append(available, node)
		} else {
			unavailable = append(unavailable, node)
		}
	}
	logger.Info("=== nodes available ===")
	for _, node := range available {
		logger.Info(nodeLine(s.objective, node))
	}
	if len(unavailable) > 0 {
		logger.Info("=== nodes unavailable ===")
		for _, node := range unavailable {
			logger.Info(nodeLine(s.objective, node))
		}
	}
}

func (s *Balancing) String() string {
	return s.objective.Name() + "/" + s.strategy.Name()
}

// Single is the selection of a group that uses one node at a time, like the url test
// group. It keeps the node it chose while its score is within the tolerance of the best
// one: a change takes every new connection with it, so a difference that is not worth
// it keeps the node where it is.
//
// The tolerance is the one of the objective it was created with: the two objectives
// measure different quantities, so their bands are in different units.
type Single struct {
	objective Objective
}

func NewSingle(objective Objective) *Single {
	return &Single{objective: objective}
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
	bander, isBander := s.objective.(Bander)
	if !isScorer || !isBander {
		return []*Node{best}, best.Tag()
	}
	if previous != "" && previous != best.Tag() {
		limit := bander.Limit(best)
		for _, node := range ranked {
			if node.Tag() != previous {
				continue
			}
			if node.Status == best.Status && scorer.Score(node) <= limit {
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

// LogNodes lists every node in the order of the ranking and marks the one in use. There
// is no group to tell apart from another: every connection goes to that node, and the
// order says how far the others are from it, which is what the tolerance decides.
func (s *Single) LogNodes(logger Logger, all, selected []*Node) {
	ranked := s.objective.Rank(all)
	var best *Node
	if len(ranked) > 0 {
		best = ranked[0]
	}
	logger.Info(s.String(), ", ", len(all), " nodes", clusterDescription(s.objective, best))
	var inUse *Node
	if len(selected) > 0 {
		inUse = selected[0]
	}
	for _, node := range ranked {
		line := nodeLine(s.objective, node)
		if inUse != nil && node.Tag() == inUse.Tag() {
			line += " SELECTED"
		}
		logger.Info(line)
	}
}

func (s *Single) String() string {
	return s.objective.Name() + "/single"
}

// RetainNodes returns the tags of selected, plus the tags of inUse that the keeper
// still accepts, in the order of inUse.
//
// best is the node the others are compared to, the best one of the ranking and not the
// one the selection returned: a single node selection may have kept a node that is
// itself within the tolerance, and measuring against it would allow a kept node up to
// twice the tolerance away from the best.
func RetainNodes(all, selected []*Node, best *Node, inUse []string, keeper Keeper) []string {
	retained := Tags(selected)
	if len(selected) == 0 || len(inUse) == 0 {
		return retained
	}
	if best == nil {
		return retained
	}
	selectedTags := make(map[string]bool, len(retained))
	for _, tag := range retained {
		selectedTags[tag] = true
	}
	nodesByTag := make(map[string]*Node, len(all))
	for _, node := range all {
		nodesByTag[node.Tag()] = node
	}
	for _, tag := range inUse {
		if selectedTags[tag] {
			continue
		}
		node, loaded := nodesByTag[tag]
		if !loaded || !keeper.Keep(node, best) {
			continue
		}
		retained = append(retained, tag)
	}
	return retained
}

// Tags returns the tags of the nodes, without duplicates.
func Tags(nodes []*Node) []string {
	return common.Uniq(common.Map(nodes, func(node *Node) string {
		return node.Tag()
	}))
}
