package selection

import (
	"strconv"
	"time"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
)

// Objective is the interface for balancer objectives
type Objective interface {
	// Name returns the objective the group was configured with.
	Name() string
	// Rank orders the nodes from the most to the least preferred, and returns them
	// as a new slice, so the number and order of `all` are kept, which matters for
	// the consistent hash strategy.
	//
	// The status is part of the order: the nodes of a better class come first, so
	// Select can take a head of the result.
	Rank(all []*Node) []*Node
	// Select returns the nodes for new connections, a head of ranked in practice.
	//
	// Conventions：
	//  1. takes care of the fallback logic, it never returns an empty slice if
	//     ranked is not empty.
	//  2. only returns nodes of a single class, a selection never mixes a fallback
	//     status in.
	Select(ranked []*Node) []*Node
}

// Filter returns the nodes for new connections in preference order.
func Filter(objective Objective, all []*Node) []*Node {
	return objective.Select(objective.Rank(all))
}

// ObjectiveOptions configure the ranking and the cluster around the best node.
type ObjectiveOptions struct {
	// Tolerance is the band around the best node, in absolute milliseconds: a node whose
	// score is at most this far above the best one counts as equally good. 0 for the
	// default of the objective, 10ms for idle and 50ms for quick.
	Tolerance time.Duration
	// MinNodes is the fewest nodes the cluster is extended to, MaxNodes the most it is
	// limited to, 0 for no limit.
	MinNodes uint
	MaxNodes uint
}

// NewObjective returns the objective of the name, alive when the name is empty.
func NewObjective(name string, options ObjectiveOptions) (Objective, error) {
	pick := option.LoadBalancePickOptions{
		Tolerance: badoption.Duration(options.Tolerance),
		MinNodes:  options.MinNodes,
		MaxNodes:  options.MaxNodes,
	}
	switch name {
	case "", ObjectiveAlive:
		return NewAliveObjective(), nil
	case ObjectiveQualified:
		return NewQualifiedObjective(), nil
	case ObjectiveIdle:
		return NewIdleObjective(pick), nil
	case ObjectiveQuick:
		return NewQuickObjective(pick), nil
	default:
		return nil, E.New("unknown objective: ", name)
	}
}

// Scorer is an optional interface for objectives that rank nodes by a score.
type Scorer interface {
	// Score returns the cost of using the node, lower is better.
	Score(node *Node) healthcheck.RTT
}

// Bander is an optional interface for objectives whose band around the best node is
// not a fixed share of its score.
type Bander interface {
	// Limit returns the highest score that still counts as equally good as best.
	Limit(best *Node) healthcheck.RTT
}

// Clusterer is an optional interface for objectives that take a cluster around the best
// node, so a report can name the part of the decision that comes from the configuration
// rather than from the measurements.
type Clusterer interface {
	// Tolerance is the configured width of the band around the best node, the part of it
	// that describes a difference worth switching for: the rest is the margin the
	// measurement needs.
	Tolerance() healthcheck.RTT
	// MinNodes is the fewest nodes the cluster is extended to, MaxNodes the most it is
	// limited to, 0 for no limit.
	MinNodes() int
	MaxNodes() int
}

// clusterDescription returns the part of a log line that names the cluster an objective
// takes around the best node, empty for an objective that takes none. The band is the
// tolerance plus the margin the measurement adds to it, which only exists once there is a
// best node to measure.
func clusterDescription(objective Objective, best *Node) string {
	clusterer, isClusterer := objective.(Clusterer)
	if !isClusterer {
		return ""
	}
	description := ", tolerance " + clusterer.Tolerance().String()
	if band, loaded := BandOf(objective, best); loaded {
		description += ", band " + band.String()
	}
	if minNodes := clusterer.MinNodes(); minNodes > 0 {
		description += ", min nodes " + strconv.Itoa(minNodes)
	}
	if maxNodes := clusterer.MaxNodes(); maxNodes > 0 {
		description += ", max nodes " + strconv.Itoa(maxNodes)
	}
	return description
}

// BandOf returns the width of the band around the best node, the tolerance plus the margin
// the measurement adds to it, and whether the objective has one.
func BandOf(objective Objective, best *Node) (healthcheck.RTT, bool) {
	bander, isBander := objective.(Bander)
	scorer, isScorer := objective.(Scorer)
	if !isBander || !isScorer || best == nil {
		return healthcheck.Failed, false
	}
	score := scorer.Score(best)
	limit := bander.Limit(best)
	if score == healthcheck.Failed || limit == healthcheck.Failed || limit < score {
		return healthcheck.Failed, false
	}
	return limit - score, true
}

// Keeper is an optional interface for objectives that can tell whether a node that
// just left the selection should still serve the connections it already has.
type Keeper interface {
	// Keep reports whether node, which is no longer selected, is close enough to
	// best to keep its existing connections. See ScoreObjective.Keep.
	Keep(node *Node, best *Node) bool
}
