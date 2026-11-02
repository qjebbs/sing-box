package selection

import (
	"errors"
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

func (n *Node) String() string {
	if n == nil {
		return "nil"
	}
	tag := "nil"
	if n.Outbound != nil {
		tag = n.Outbound.Tag()
	}
	// The average is not printed: the median is the typical delay, and the average is
	// moved by a single spike, so it says less about what the node usually costs.
	return fmt.Sprintf(
		"#%d %s [%s] STD=%s MED=%s Latest=%s FAIL=%d/%d",
		n.Index, n.Status, tag,
		n.deviationString(), n.medianString(), n.latestString(),
		n.Fail, n.All,
	)
}

// nodeLine returns the log line of a node: its statistics and, when the objective scores
// the nodes, its score.
func nodeLine(objective Objective, node *Node) string {
	line := node.String()
	scorer, isScorer := objective.(Scorer)
	if !isScorer {
		return line
	}
	if score := scorer.Score(node); score != healthcheck.Failed {
		return line + " SCORE=" + score.String()
	}
	// healthcheck.Failed means there is no score to show, printing 0ms would read as the
	// best possible result.
	return line + " SCORE=None"
}

// deviationString returns the deviation to show. A node that never worked has no
// stability to speak of: the zero of a missing value reads as the best possible
// result.
func (n *Node) deviationString() string {
	if n.Stats.Succeeded() == 0 {
		return "None"
	}
	return n.formatRTT(n.Deviation)
}

// medianString returns the median to show, there is none without a successful check.
func (n *Node) medianString() string {
	if n.Stats.Succeeded() == 0 {
		return "None"
	}
	return n.formatRTT(n.Median)
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

// calcStatus tells if a node is alive or qualified according to the healthcheck statistics
func calcStatus(s *healthcheck.Stats, maxRTT healthcheck.RTT, maxFailRate float32) Status {
	if s.All == 0 {
		// untetsted
		return StatusUnknown
	}
	if s.Succeeded() == 0 {
		// nothing ever worked, there is no delay to rank and nothing to fall back on
		return StatusDead
	}
	if s.Fail > 0 && float32(s.Fail)/float32(s.All) > maxFailRate {
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
	// MaxFail is how many checks of the sampling window may fail before a node is only
	// alive, 0 for any failure.
	MaxFail uint
	// Biases scale the score of the nodes they match.
	Biases []option.LoadBalancePickBias
}

// NodeBuilder builds the nodes of a group with the status their health gives them.
type NodeBuilder struct {
	maxRTT      healthcheck.RTT
	maxFailRate float32
	biases      []bias
}

type bias struct {
	option.LoadBalancePickBias
	Regexp *regexp.Regexp
}

// NewNodeBuilder returns the builder of the nodes of a group. sampling is the number of
// checks a node keeps, which the share of failures is relative to.
func NewNodeBuilder(sampling int, options NodeOptions) (*NodeBuilder, error) {
	if sampling <= 0 {
		return nil, errors.New("sampling must be greater than 0")
	}
	builder := &NodeBuilder{
		maxRTT:      options.MaxRTT,
		maxFailRate: float32(options.MaxFail) / float32(sampling),
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
	status := calcStatus(&stats, b.maxRTT, b.maxFailRate)
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
