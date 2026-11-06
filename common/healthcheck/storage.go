package healthcheck

import (
	"time"
)

// Storage holds ping rtts for health Checker, it's not thread safe
type Storage struct {
	options StorageOptions
	history *ring[History]
	// baseline is the fastest the node has been, kept per time of day. It outlives the
	// window: a window describes the node now, the baseline describes what it can be.
	baseline baseline
	stats    Stats
}

// History is the rtt history
type History struct {
	Time  time.Time `json:"time"`
	Delay RTT       `json:"delay"`
}

// NewStorage returns a new rtt storage with the given sizes
func NewStorage(options StorageOptions) *Storage {
	options = options.withDefaults()
	return &Storage{
		options: options,
		history: newRing[History](int(options.Retained)),
	}
}

// retain resizes the history, keeping the most recent checks it already holds. Only the
// kept size may change without the statistics moving: a different sampling or a different
// age is a different measurement, so the cache is dropped for it.
func (s *Storage) retain(options StorageOptions) {
	if options.Sampling != s.options.Sampling || options.Validity != s.options.Validity {
		s.stats = Stats{}
	}
	if options.Retained != s.options.Retained {
		kept := min(s.history.Length(), int(options.Retained))
		history := newRing[History](int(options.Retained))
		for i := kept - 1; i >= 0; i-- {
			check, _ := s.history.At(i)
			history.Push(check)
		}
		s.history = history
	}
	s.options = options
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

// All returns the history that is still kept, most recent first: the window the statistics
// read, and the checks kept behind it when the history is kept longer.
func (s *Storage) All() []*History {
	if s == nil {
		return nil
	}
	current := now()
	all := make([]*History, 0, s.history.Length())
	for i := 0; i < s.history.Length(); i++ {
		rtt, _ := s.history.At(i)
		if rtt.Time.IsZero() || current.Sub(rtt.Time) > s.options.Retention {
			break
		}
		all = append(all, &rtt)
	}
	return all
}
