package healthcheck

import (
	"time"

	"github.com/sagernet/sing-box/adapter"
)

// Storage holds ping rtts for health Checker, it's not thread safe
type Storage struct {
	options StorageOptions
	history *ring[History]
	// baseline is the fastest the node has been, kept per time of day. It outlives the
	// window: a window describes the node now, the baseline describes what it can be.
	baseline baseline
	// failures is how often the node has failed over the last few hours, in wall-clock
	// time. It outlives the window too: a window can be clean while the node is not
	// reliable, see failureRate.
	failures failureRate
	stats    Stats
	// suspended is set by an out of band check that failed, see Update, and cleared by
	// the first check that passes. It is not part of the window: the window is what the
	// interval measured, this is what the connections the node carried have shown.
	suspended bool
}

// History is the rtt history
type History struct {
	Time  time.Time `json:"time"`
	Delay RTT       `json:"delay"`
}

// failureStateTimeout is how long a stored failure estimate is worth restoring. An estimate
// is read as it stands, without being brought forward, so one that has been decaying
// unwatched for longer than this no longer describes the node: the window is the better
// evidence by then.
const failureStateTimeout = 3 * failureTimeConstant

// State returns what the storage keeps outside the window, and whether there is anything to
// keep. A floor older than a day, or a failure estimate that has gone stale, is left out:
// the record would not be usable on the next start either.
func (s *Storage) State(now time.Time) (adapter.HealthState, bool) {
	// The round writes the baseline before it reads it, see refreshStats, so a state taken
	// without refreshing the statistics can be a round behind.
	s.refreshStats(now)
	var state adapter.HealthState
	values, window, filled := s.baseline.export(now)
	if filled {
		state.Floors = values
		state.FloorsAt = window
	}
	if sum, weight, at := s.failures.estimate(); !at.IsZero() && now.Sub(at) < failureStateTimeout {
		state.FailureSum = sum
		state.FailureWeight = weight
		state.FailureAt = at
	}
	return state, filled || !state.FailureAt.IsZero()
}

// restoreState puts a stored state back. The slots go back into the windows they came from,
// so they expire when they would have without the restart, see baseline.restore. The failure
// estimate is restored as it was: the next check folds into it as if the process had never
// stopped.
//
// It reports whether the state held anything this run does not: a record read a second time,
// or one older than what the checks have already measured, is nothing.
func (s *Storage) restoreState(state adapter.HealthState, now time.Time) bool {
	restored := s.baseline.restore(state.Floors, state.FloorsAt)
	if !state.FailureAt.IsZero() && now.Sub(state.FailureAt) < failureStateTimeout {
		if s.failures.restore(state.FailureSum, state.FailureWeight, state.FailureAt) {
			restored = true
		}
	}
	if restored {
		// statistics is not valid any more
		s.stats = Stats{}
	}
	return restored
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

// Put puts a new rtt to the HealthPingResult, as the scheduled check does. A check that
// passed lifts the suspension, since the node answered the question the failed connections
// asked: a check that failed leaves it as it is, because that failure is a sample of the
// window rather than a verdict about the node now.
func (s *Storage) Put(d RTT) {
	if s == nil {
		return
	}
	at := now()
	s.history.Push(History{Time: at, Delay: d})
	s.failures.observe(at, d == Failed)
	if d != Failed {
		s.suspended = false
		// The baseline is filled here, from the check itself, and not from a round of them
		// read later: an idle node is probed once a window, so a round of it holds this one
		// check and nothing else, and it would set no floor at all. A slot keeps the fastest
		// check of its own window either way, so filling it a check at a time leaves the day
		// with the same minimum as filling it once a round.
		//
		// Writing it before the statistics are read keeps the floor at or below the fastest
		// check of the window: a minimum over real delays cannot make a score of zero.
		s.baseline.Put(at, d)
	}
	// statistics is not valid any more
	s.stats = Stats{}
}

// Update updates the last history, which is what a check the connections asked for does:
// it replaces the latest result instead of adding one, so the sampling stays as even as the
// interval made it. The result is the answer to the question the failed connections raised,
// so a failed one suspends the node until a check passes, see Stats.Suspended.
func (s *Storage) Update(d RTT) {
	if s == nil {
		return
	}
	at := now()
	s.history.Replace(History{Time: at, Delay: d})
	// a check the connections asked for is evidence about the node like any other: it is
	// the second half of what a failed connection already said
	s.failures.observe(at, d == Failed)
	if d != Failed {
		s.baseline.Put(at, d)
	}
	s.suspended = d == Failed
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
