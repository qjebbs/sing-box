package healthcheck

import (
	"time"
)

// Storage holds ping rtts for health Checker, it's not thread safe
type Storage struct {
	validity time.Duration
	history  *ring[History]

	// connections holds the outcomes of the connections the group carried, see
	// Stats.Connections. They are kept apart from the checks: a check is one probe on a
	// schedule, a connection is the traffic the group was asked to carry, and the two
	// say different things about a node.
	//
	// Both outcomes are kept, not only the failures: a count of failures alone grows
	// with the traffic of the node, so a busy node would be scored worse than an idle
	// one whatever its quality.
	//
	// The ring holds as many connections as the checks do. The two describe the same
	// window of the same node, and are mixed by their counts, so a longer run of one
	// would outweigh the other instead of corroborating it.
	//
	// It is allocated by the first connection reported, so a node whose traffic never
	// reaches its group doesn't pay for it.
	connections *ring[connectionOutcome]

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
		validity: validity,
		history:  newRing[History](int(cap)),
	}
}

// now returns the current time without the monotonic clock, so the history survives a
// computer sleep: https://pkg.go.dev/time#hdr-Monotonic_Clocks
func now() time.Time {
	return time.Now().Round(0)
}

// Put puts a new rtt to the HealthPingResult
func (s *Storage) Put(d RTT) {
	if s == nil {
		return
	}
	s.history.Push(History{Time: now(), Delay: d})
	// statistics is not valid any more
	s.stats = Stats{}
}

// Update updates the last history
func (s *Storage) Update(d RTT) {
	if s == nil {
		return
	}
	s.history.Replace(History{Time: now(), Delay: d})
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
func (s *Storage) ReportConnection(at time.Time, established bool) {
	if s == nil {
		return
	}
	if s.connections == nil {
		s.connections = newRing[connectionOutcome](s.history.Capacity())
	}
	s.connections.Push(connectionOutcome{time: at, established: established})
	// statistics is not valid any more
	s.stats = Stats{}
}

// ConnectionOutcomes returns how many connections were carried inside the validity
// window, and how many of them failed.
//
// The oldest connections leave the window first, so the walk stops at the first expired
// one. When more than the capacity were carried inside the window, the count describes
// the most recent ones.
func (s *Storage) ConnectionOutcomes(at time.Time) (attempts int, failures int) {
	if s.connections == nil {
		return 0, 0
	}
	cutoff := at.Add(-s.validity)
	for i := 0; i < s.connections.Length(); i++ {
		connection, _ := s.connections.At(i)
		if !connection.time.After(cutoff) {
			break
		}
		attempts++
		if !connection.established {
			failures++
		}
	}
	return attempts, failures
}

// Get gets the history at the offset to the latest history, ignores the validity.
// The offset is counted backwards: 0 is the latest, -1 the one before it.
func (s *Storage) Get(offset int) *History {
	if s == nil {
		return nil
	}
	rtt, loaded := s.history.At(-offset)
	if !loaded || rtt.Time.IsZero() {
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
	all := make([]*History, 0, s.history.Length())
	for i := 0; i < s.history.Length(); i++ {
		rtt, _ := s.history.At(i)
		if rtt.Time.IsZero() {
			break
		}
		all = append(all, &rtt)
	}
	return all
}
