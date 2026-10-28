package healthcheck

import (
	"time"
)

// Storage holds ping rtts for health Checker, it's not thread safe
type Storage struct {
	idx      int
	cap      int
	validity time.Duration
	history  []History

	// connections holds the outcomes of the connections the group carried, see
	// Stats.Connections. They are kept apart from the checks: a check is one probe on a
	// schedule, a connection is the traffic the group was asked to carry, and the two
	// say different things about a node.
	//
	// Both outcomes are kept, not only the failures: a count of failures alone grows
	// with the traffic of the node, so a busy node would be scored worse than an idle
	// one whatever its quality.
	connections []connectionOutcome

	stats Stats
}

// History is the rtt history
type History struct {
	Time  time.Time `json:"time"`
	Delay RTT       `json:"delay"`
}

// NewStorage returns a new rtt storage with specified capacity
func NewStorage(cap uint, validity time.Duration) *Storage {
	return &Storage{
		cap:      int(cap),
		validity: validity,
		history:  make([]History, cap),
	}
}

// Put puts a new rtt to the HealthPingResult
func (s *Storage) Put(d RTT) {
	if s == nil {
		return
	}
	s.idx = s.offset(1)
	s.Update(d)
}

// Update updates the last history
func (s *Storage) Update(d RTT) {
	if s == nil {
		return
	}
	// strip monotonic clock from `now` to avoid inaccurate
	// time comparison after computer sleep
	// https://pkg.go.dev/time#hdr-Monotonic_Clocks
	s.history[s.idx].Time = time.Now().Round(0)
	s.history[s.idx].Delay = d
	// statistics is not valid any more
	s.stats = Stats{}
}

// connectionOutcome is one connection the group carried, and whether it was
// established.
type connectionOutcome struct {
	time        time.Time
	established bool
}

// ReportConnection records the outcome of a connection the group carried with the node,
// it is counted until it leaves the validity window.
func (s *Storage) ReportConnection(now time.Time, established bool) {
	if s == nil {
		return
	}
	s.pruneConnections(now)
	s.connections = append(s.connections, connectionOutcome{time: now, established: established})
	if len(s.connections) > s.cap {
		// a longer run says no more about the node than a full sampling window of it
		s.connections = s.connections[len(s.connections)-s.cap:]
	}
	// statistics is not valid any more
	s.stats = Stats{}
}

// pruneConnections drops the connections that left the validity window.
func (s *Storage) pruneConnections(now time.Time) {
	if len(s.connections) == 0 {
		return
	}
	cutoff := now.Add(-s.validity)
	kept := s.connections[:0]
	for _, connection := range s.connections {
		if connection.time.After(cutoff) {
			kept = append(kept, connection)
		}
	}
	s.connections = kept
}

// ConnectionOutcomes returns how many connections were carried inside the validity
// window, and how many of them failed.
func (s *Storage) ConnectionOutcomes(now time.Time) (attempts int, failures int) {
	s.pruneConnections(now)
	for _, connection := range s.connections {
		if !connection.established {
			failures++
		}
	}
	return len(s.connections), failures
}

// Get gets the history at the offset to the latest history, ignores the validity
func (s *Storage) Get(offset int) *History {
	if s == nil {
		return nil
	}
	rtt := s.history[s.offset(offset)]
	if rtt.Time.IsZero() {
		return nil
	}
	return &rtt
}

// Latest gets the latest history, alias of Get(0)
func (s *Storage) Latest() *History {
	return s.Get(0)
}

// All returns all the history, ignores the validity
func (s *Storage) All() []*History {
	if s == nil {
		return nil
	}
	all := make([]*History, 0, s.cap)
	for i := 0; i < s.cap; i++ {
		rtt := s.history[s.offset(-i)]
		if rtt.Time.IsZero() {
			break
		}
		all = append(all, &rtt)
	}
	return all
}

func (s *Storage) offset(n int) int {
	idx := s.idx
	idx += n
	if idx >= s.cap {
		idx %= s.cap
	} else if idx < 0 {
		idx %= s.cap
		idx += s.cap
	}
	return idx
}
