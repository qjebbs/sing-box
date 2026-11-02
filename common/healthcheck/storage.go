package healthcheck

import (
	"time"
)

// Storage holds ping rtts for health Checker, it's not thread safe
type Storage struct {
	validity time.Duration
	history  *ring[History]
	stats    Stats
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
