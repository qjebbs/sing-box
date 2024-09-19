package balancer

import (
	"fmt"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
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

	Index  int
	Status Status

	rand int
}

func (n *Node) String() string {
	if n == nil {
		return "nil"
	}
	tag := "nil"
	if n.Outbound != nil {
		tag = n.Outbound.Tag()
	}
	return fmt.Sprintf(
		"#%d %s [%s] STD=%s AVG=%s Latest=%s FAIL=%d/%d",
		n.Index, n.Status, tag,
		n.Deviation, n.Average, n.Latest, n.Fail, n.All,
	)
}

// CalcStatus calculates & updates the status of the node according to the healthcheck statistics
func (n *Node) CalcStatus(maxRTT healthcheck.RTT, maxFailRate float32) {
	n.Status = nodeStatus(&n.Stats, maxRTT, maxFailRate)
}

// nodeStatus tells if a node is alive or qualified according to the healthcheck statistics
func nodeStatus(s *healthcheck.Stats, maxRTT healthcheck.RTT, maxFailRate float32) Status {
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
	if maxRTT > 0 && s.Average > maxRTT {
		return StatusAlive
	}
	return StatusQualified
}
