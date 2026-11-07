package healthcheck

import (
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

// StorageOptions is how much of the history one node keeps.
//
// The sampling and the history are two sizes, not one: the statistics read Sampling checks,
// the history keeps Retained of them. The extra checks are never read by the ranking, only by
// the report, which uses them to measure the ranking against the checks that follow it.
type StorageOptions struct {
	// Sampling is how many checks the statistics read: the sampling the node is ranked over.
	Sampling uint
	// Validity is how old those checks may be.
	Validity time.Duration
	// Retained is how many checks the history keeps, at least Sampling.
	Retained uint
	// Retention is how old the kept checks may be, at least Validity. It is a staleness
	// bound, not the width of the history: the checks are the width, and this only says
	// when the whole of them stops describing the node.
	Retention time.Duration
	// Interval is the time between two checks, which the ages above are sized from and
	// the history is as wide as.
	Interval time.Duration
}

func (o StorageOptions) withDefaults() StorageOptions {
	if o.Sampling == 0 {
		o.Sampling = 1
	}
	if o.Retained < o.Sampling {
		o.Retained = o.Sampling
	}
	if o.Retention < o.Validity {
		o.Retention = o.Validity
	}
	return o
}

// Storages is the storages for different tags (nodes)
type Storages struct {
	sync.RWMutex

	options  StorageOptions
	storages map[string]*Storage
	// changed reports whether anything was measured since the last save, so a checker whose
	// groups are all idle does not rewrite what is already stored.
	changed bool
}

// NewStorages returns a new Storages
func NewStorages(options StorageOptions) *Storages {
	return &Storages{
		options:  options.withDefaults(),
		storages: make(map[string]*Storage),
	}
}

// Sampling returns how many checks the statistics of each node read, the sampling the group
// is ranked over. The history may keep more of them, see StorageOptions.
func (s *Storages) Sampling() int {
	return int(s.options.Sampling)
}

// retentionMultiple is how many times the width of the history a check may be kept. The
// ring is what decides how many checks are kept, so the age is here to catch a pause rather
// than to police the spacing: a round that runs longer than the interval must not quietly
// shorten the history that was asked for.
const retentionMultiple = 2

// Resize sets how many checks the statistics read and how many the history keeps behind
// them, keeping the checks already stored, and returns what was applied.
//
// A sampling of zero keeps the one in use, an extra of zero keeps the sampling alone. Reading
// and applying the sizes is one step, so two resizes cannot interleave into a history
// neither of them asked for.
func (s *Storages) Resize(sampling uint, extra uint, interval time.Duration) StorageOptions {
	s.Lock()
	defer s.Unlock()
	if sampling == 0 {
		sampling = s.options.Sampling
	}
	retained := sampling + extra
	s.options = StorageOptions{
		Sampling:  sampling,
		Validity:  time.Duration(sampling+1) * interval,
		Retained:  retained,
		Retention: time.Duration(retained+1) * interval * retentionMultiple,
		Interval:  interval,
	}.withDefaults()
	for _, store := range s.storages {
		store.retain(s.options)
	}
	return s.options
}

// Latest gets the latest history for the tag
func (s *Storages) Latest(tag string) *History {
	s.RLock()
	defer s.RUnlock()
	return s.storages[tag].Latest()
}

// All gets all histories for the tag
func (s *Storages) All(tag string) []*History {
	s.RLock()
	defer s.RUnlock()
	return s.storages[tag].All()
}

// Stats gets the statistics of all histories for the tag
func (s *Storages) Stats(tag string) Stats {
	s.Lock()
	defer s.Unlock()
	return s.storages[tag].Stats()
}

// Put appends a scheduled check for the tag
func (s *Storages) Put(tag string, delay RTT) {
	s.Lock()
	defer s.Unlock()
	store := s.storageOf(tag)
	store.Put(delay)
	s.changed = true
}

// Update commits an out of band check for the tag, replacing its latest result and
// suspending the node when the check failed
func (s *Storages) Update(tag string, delay RTT) {
	s.Lock()
	defer s.Unlock()
	store := s.storageOf(tag)
	store.Update(delay)
	s.changed = true
}

// storageOf returns the storage of the tag, creating it when the node is checked for the
// first time.
func (s *Storages) storageOf(tag string) *Storage {
	store, loaded := s.storages[tag]
	if loaded {
		return store
	}
	store = NewStorage(s.options)
	s.storages[tag] = store
	return store
}

// Restore puts a stored state back into the storage of its tag, creating that storage when the
// node has not been checked yet. It is called before the first round measures anything, with
// the nodes the providers still have, so the record of a node that is gone never enters memory
// and nothing has to drop it afterwards.
func (s *Storages) Restore(tag string, state adapter.HealthState, now time.Time) {
	s.Lock()
	defer s.Unlock()
	s.storageOf(tag).restoreState(state, now)
}

// States returns the states to store, one per node that has something to keep. A state that
// has expired is left out, see Storage.State.
func (s *Storages) States(now time.Time) map[string]adapter.HealthState {
	s.Lock()
	defer s.Unlock()
	states := make(map[string]adapter.HealthState, len(s.storages))
	for tag, store := range s.storages {
		if state, ok := store.State(now); ok {
			states[tag] = state
		}
	}
	return states
}

// TakeChanged reports whether anything was measured since the last call, and forgets it, so
// a period that wrote the states does not write them again while nothing moves.
func (s *Storages) TakeChanged() bool {
	s.Lock()
	defer s.Unlock()
	changed := s.changed
	s.changed = false
	return changed
}

// MarkChanged records that the states still have to be written, which a failed write does.
func (s *Storages) MarkChanged() {
	s.Lock()
	defer s.Unlock()
	s.changed = true
}

// Delete remove the histories storage for the tag
func (s *Storages) Delete(tag string) {
	s.Lock()
	defer s.Unlock()
	delete(s.storages, tag)
}

// List returns the storage list
func (s *Storages) List() []string {
	s.RLock()
	defer s.RUnlock()
	list := make([]string, 0, len(s.storages))
	for tag := range s.storages {
		list = append(list, tag)
	}
	return list
}
