package selection

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

// Status is the status of a node
type Status int

const (
	// StatusDead is the status of a node that never passed a check
	StatusDead Status = iota
	// StatusUnknown is the status of a node that is not tested yet
	StatusUnknown
	// StatusAlive is the status of a node that is alive
	StatusAlive
	// StatusQualified is the status of a node that is qualified
	StatusQualified
)

func (s Status) String() string {
	switch s {
	case StatusDead:
		return "x"
	case StatusUnknown:
		return "?"
	case StatusAlive:
		return "*"
	case StatusQualified:
		return "OK"
	default:
		return ""
	}
}

// Node is a banalcer Node with health check result
type Node struct {
	adapter.Outbound
	healthcheck.Stats

	Index     int
	RTTSacale float32
	Status    Status

	rand int
}

// NewNode creates a new Node with the given outbound and index.
func NewNode(
	outbound adapter.Outbound, index int, rttScale float32,
	stats healthcheck.Stats, status Status,
) *Node {
	if rttScale <= 0 {
		rttScale = 1
	}
	return &Node{
		Outbound:  outbound,
		Index:     index,
		RTTSacale: rttScale,
		Stats:     stats,
		Status:    status,

		rand: rand.Intn(math.MaxInt32),
	}
}

// String returns the head of a node line: the node itself and the measurements every
// objective is read with, whatever it ranks by.
//
// LAST and MED are those. Every objective falls back to a worse class of node when the
// better one is gone, and the latest delay is what says how the node is doing now, while
// the median is what the limit max_rtt is compared with. The average is not printed: a
// single spike moves it, so it says less about what the node usually costs.
//
// The failure record is the third measurement every objective is read with, but it is the
// raw observation, see nodeLine, so it ends the line instead of opening it. What an
// objective reads beyond these is appended by the objective itself, see Printer: the line
// is read at a glance, so a statistic no decision is made on is left out rather than
// crowded in.
func (n *Node) String() string {
	if n == nil {
		return "nil"
	}
	tag := "nil"
	if n.Outbound != nil {
		tag = n.Outbound.Tag()
	}
	return fmt.Sprintf(
		"#%d %s [%s] LAST=%s MED=%s",
		n.Index, n.Status, tag, n.latestString(), n.medianString(),
	)
}

// countString returns a count of checks against all of them, e.g. the failure record of
// the node.
func (n *Node) countString(count int) string {
	return fmt.Sprintf("%d/%d", count, n.All)
}

// nodeLine returns the line of a node: the measurements every objective is read with, the
// ones this objective reads on its own, the score when the objective has one, and the
// failures.
func nodeLine(objective Objective, node *Node) string {
	line := node.String()
	if printer, isPrinter := objective.(Printer); isPrinter {
		if stats := printer.PrintStats(node); stats != "" {
			line += " " + stats
		}
	}
	if scorer, isScorer := objective.(Scorer); isScorer {
		if score := scorer.Score(node); score != healthcheck.Failed {
			line += " SCORE=" + score.String()
		} else {
			// healthcheck.Failed means there is no score to show, printing 0ms would read
			// as the best possible result.
			line += " SCORE=None"
		}
	}
	// The failures are not part of any score: they are what max_fail is compared with, and
	// what a fallback class is read from when the better one is gone. They come last so
	// that the numbers a node is ranked by stay together.
	return line + " FAIL=" + node.countString(node.Fail)
}

// selectedMark is appended to the line of a node the selection returned.
const selectedMark = " SELECTED"

// NodeLines returns one line per node of ranked, in that order, with the nodes selected
// marked: the lines are a flat list, so the mark is what says which of them are in use.
// It is the text form of a ranking, and the only form the groups state it in.
func NodeLines(objective Objective, ranked, selected []*Node) []string {
	selectedTags := make(map[string]bool, len(selected))
	for _, node := range selected {
		selectedTags[node.Tag()] = true
	}
	lines := make([]string, 0, len(ranked))
	for _, node := range ranked {
		line := nodeLine(objective, node)
		if selectedTags[node.Tag()] {
			line += selectedMark
		}
		lines = append(lines, line)
	}
	return lines
}

// jitterString returns the jitter to show, the statistic quick adds to the median and the
// one both score objectives size their band by. A node that never worked has no stability
// to speak of: the zero of a missing value reads as the best possible result.
func (n *Node) jitterString() string {
	if n.Stats.Succeeded() == 0 {
		return "None"
	}
	return n.formatRTT(n.Jitter)
}

// medianString returns the median to show, there is none without a successful check.
func (n *Node) medianString() string {
	if n.Stats.Succeeded() == 0 {
		return "None"
	}
	return n.formatRTT(n.Median)
}

// floorString is the fastest the node has been, the reference idle measures against, and
// the only measurement of a node that outlives its window.
func (n *Node) floorString() string {
	if n.Stats.Floor == 0 {
		return "None"
	}
	return n.formatRTT(n.Floor)
}

// latestString returns the latest delay to show. A failed check, and a check that
// never happened, have a delay of healthcheck.Failed, which is not a delay of zero.
func (n *Node) latestString() string {
	if n.Latest == healthcheck.Failed {
		return "None"
	}
	return n.formatRTT(n.Latest)
}

// formatRTT returns the delay with the scale factor applied, when there is one.
func (n *Node) formatRTT(rtt healthcheck.RTT) string {
	if n.RTTSacale <= 0 || n.RTTSacale == 1 {
		return rtt.String()
	}
	return fmt.Sprintf("%s(%s)", rtt, applyFactorToRTT(rtt, n.RTTSacale))
}

// ScaleRTT returns the RTT after applying the scale factor of this node.
func (n *Node) ScaleRTT(rtt healthcheck.RTT) healthcheck.RTT {
	return applyFactorToRTT(rtt, n.RTTSacale)
}

// failureNearLimit is FAIL_NEAR a node may carry and stay qualified: one full failure. A
// failed check weighs one, so a node leaves on the check that fails and is back on the next
// one that succeeds, unless it fails again and the weights add up.
const failureNearLimit = 1.0

// calcStatus tells if a node is alive or qualified according to the healthcheck statistics.
// A recent failure keeps the node alive until its weight decays below one full failure, see
// failureNearLimit.
func calcStatus(s *healthcheck.Stats, maxRTT healthcheck.RTT) Status {
	if s.All == 0 {
		// untetsted
		return StatusUnknown
	}
	if s.Succeeded() == 0 {
		// nothing ever worked, there is no delay to rank and nothing to fall back on
		return StatusDead
	}
	if s.Suspended {
		// The node was checked because the connections it carried were failing, and that
		// check failed too. It worked before, so it is alive rather than dead, but it does
		// not qualify until a check passes: the window is the reads of the interval, and
		// the checks that are in it say nothing about the node now.
		return StatusAlive
	}
	if s.FailureNear >= failureNearLimit {
		return StatusAlive
	}
	// don't apply RTT scale to maxRTT, because it's a threshold, not a score
	// median is used so a single spike doesn't disqualify an otherwise good node
	if maxRTT > 0 && s.Median > maxRTT {
		return StatusAlive
	}
	return StatusQualified
}

func applyFactorToRTT(rtt healthcheck.RTT, scale float32) healthcheck.RTT {
	if scale <= 0 || scale == 1 {
		return rtt
	}
	return healthcheck.RTT(float32(rtt) * scale)
}

// NodeOptions describe how the health of a node becomes its status.
type NodeOptions struct {
	// MaxRTT is the highest median delay a node may have and stay qualified, 0 for no
	// limit.
	MaxRTT healthcheck.RTT
	// Biases scale the score of the nodes they match.
	Biases []option.LoadBalancePickBias
}

// NodeBuilder builds the nodes of a group with the status their health gives them.
//
// It holds nothing that moves: the window the statistics are read over can be changed while
// a ranking is being built, so it is passed with each node rather than kept here, and the
// builder stays safe to read from anywhere.
type NodeBuilder struct {
	maxRTT healthcheck.RTT
	biases []bias
}

type bias struct {
	option.LoadBalancePickBias
	Regexp *regexp.Regexp
}

// NewNodeBuilder returns the builder of the nodes of a group.
func NewNodeBuilder(options NodeOptions) (*NodeBuilder, error) {
	builder := &NodeBuilder{
		maxRTT: options.MaxRTT,
	}
	for _, optionBias := range options.Biases {
		var re *regexp.Regexp
		if optionBias.Regexp != "" {
			var err error
			re, err = regexp.Compile(optionBias.Regexp)
			if err != nil {
				return nil, err
			}
		}
		builder.biases = append(builder.biases, bias{
			LoadBalancePickBias: optionBias,
			Regexp:              re,
		})
	}
	return builder, nil
}

// NewNode returns the node of the outbound with the health it reports.
func (b *NodeBuilder) NewNode(outbound adapter.Outbound, index int, stats healthcheck.Stats) *Node {
	scale := calcFactor(outbound.Tag(), b.biases)
	status := calcStatus(&stats, b.maxRTT)
	return NewNode(outbound, index, scale, stats, status)
}

func calcFactor(tag string, biases []bias) float32 {
	factor := float32(1)
	for _, bias := range biases {
		if bias.RTTScale <= 0 || bias.RTTScale == 1 || !matchTag(tag, bias) {
			continue
		}
		factor *= bias.RTTScale
	}
	return factor
}

func matchTag(tag string, condition bias) bool {
	if condition.Contains != "" {
		return strings.Contains(tag, condition.Contains)
	}
	if condition.Prefix != "" {
		return strings.HasPrefix(tag, condition.Prefix)
	}
	if condition.Suffix != "" {
		return strings.HasSuffix(tag, condition.Suffix)
	}
	if condition.Regexp != nil {
		return condition.Regexp.MatchString(tag)
	}
	return false
}
