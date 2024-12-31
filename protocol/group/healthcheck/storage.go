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
		history:  make([]History, cap, cap),
	}
}

// Put puts a new rtt to the HealthPingResult
func (s *Storage) Put(d RTT) {
	if s == nil {
		return
	}
	s.idx = s.offset(1)
	// strip monotonic clock from `now` to avoid inaccurate
	// time comparison after computer sleep
	// https://pkg.go.dev/time#hdr-Monotonic_Clocks
	s.history[s.idx].Time = time.Now().Round(0)
	s.history[s.idx].Delay = d
	// statistics is not valid any more
	s.stats = Stats{}
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
