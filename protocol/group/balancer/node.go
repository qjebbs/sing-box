package balancer

import (
	"fmt"
	"math"
	"math/rand"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/service/healthcheck"
)

// Status is the status of a node
type Status int

const (
	// StatusDead is the status of a node that is dead
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
	if n.RTTSacale <= 0 || n.RTTSacale == 1 {
		return fmt.Sprintf(
			"#%d %s [%s] STD=%s AVG=%s Latest=%s FAIL=%d/%d",
			n.Index, n.Status, tag,
			n.Deviation, n.Average, n.Latest,
			n.Fail, n.All,
		)
	}
	return fmt.Sprintf(
		"#%d %s [%s] STD=%s(%s) AVG=%s(%s) Latest=%s FAIL=%d/%d",
		n.Index, n.Status, tag,

		n.Deviation, applyFactorToRTT(n.Deviation, n.RTTSacale),
		n.Average, applyFactorToRTT(n.Average, n.RTTSacale),
		n.Latest,

		n.Fail, n.All,
	)
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
	if s.Latest == healthcheck.Failed {
		return StatusDead
	}
	if s.Fail > 0 && float32(s.Fail)/float32(s.All) > maxFailRate {
		return StatusAlive
	}
	// don't apply RTT scale to maxRTT, because it's a threshold, not a score
	if maxRTT > 0 && s.Average > maxRTT {
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

func calcFactor(tag string, biases []pickBias) float32 {
	factor := float32(1)
	for _, bias := range biases {
		if bias.RTTScale <= 0 || bias.RTTScale == 1 || !matchTag(tag, bias) {
			continue
		}
		factor *= bias.RTTScale
	}
	return factor
}

func matchTag(tag string, condition pickBias) bool {
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
