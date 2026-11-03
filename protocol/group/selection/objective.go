package selection

import (
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
	// RTTTolerance is the band around the best node for the leastping objective, relative
	// to its score, e.g. 0.2 means 20%.
	RTTTolerance float32
	// JitterTolerance is the band around the best node for the leastload objective, in
	// absolute milliseconds, e.g. 10ms.
	JitterTolerance time.Duration
	// MinNodes is the fewest nodes the cluster is extended to, MaxNodes the most it is
	// limited to, 0 for no limit.
	MinNodes uint
	MaxNodes uint
}

// ValidateTolerances returns an error when a tolerance is set for an objective that does
// not use it. Each tolerance measures the quantity of one objective, so it means nothing
// for the other one, and a value that is silently ignored is a mistake rather than a
// preference. The objective must be named for the question to have an answer, so a
// tolerance cannot be set together with an objective left at its default.
func ValidateTolerances(objective string, rttTolerance float32, jitterTolerance time.Duration) error {
	if rttTolerance != 0 && objective != ObjectiveLeastPing {
		return toleranceMismatch("rtt_tolerance", ObjectiveLeastPing, objective)
	}
	if jitterTolerance != 0 && objective != ObjectiveLeastLoad {
		return toleranceMismatch("jitter_tolerance", ObjectiveLeastLoad, objective)
	}
	return nil
}

func toleranceMismatch(field string, expected string, objective string) error {
	if objective == "" {
		return E.New(field, " requires the ", expected, " objective, which must be set explicitly")
	}
	return E.New(field, " requires the ", expected, " objective, not ", objective)
}

// NewObjective returns the objective of the name, alive when the name is empty.
func NewObjective(name string, options ObjectiveOptions) (Objective, error) {
	pick := option.LoadBalancePickOptions{
		RTTTolerance:    options.RTTTolerance,
		JitterTolerance: badoption.Duration(options.JitterTolerance),
		MinNodes:        options.MinNodes,
		MaxNodes:        options.MaxNodes,
	}
	switch name {
	case "", ObjectiveAlive:
		return NewAliveObjective(), nil
	case ObjectiveQualified:
		return NewQualifiedObjective(), nil
	case ObjectiveLeastLoad:
		return NewLeastLoadObjective(pick), nil
	case ObjectiveLeastPing:
		return NewLeastPingObjective(pick), nil
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

// Keeper is an optional interface for objectives that can tell whether a node that
// just left the selection should still serve the connections it already has.
type Keeper interface {
	// Keep reports whether node, which is no longer selected, is close enough to
	// best to keep its existing connections. See LeastObjective.Keep.
	Keep(node *Node, best *Node) bool
}
