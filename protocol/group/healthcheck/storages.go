package healthcheck

import (
	"sync"
	"time"
)

// Storages is the storages for different tags (nodes)
type Storages struct {
	sync.RWMutex

	cap      uint
	validity time.Duration

	storages map[string]*Storage
}

// NewStorages returns a new Storages
func NewStorages(cap uint, validity time.Duration) *Storages {
	return &Storages{
		cap:      cap,
		validity: validity,
		storages: make(map[string]*Storage),
	}
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

// Put gets all histories for the tag
func (s *Storages) Put(tag string, delay RTT) {
	s.Lock()
	defer s.Unlock()
	store, ok := s.storages[tag]
	if !ok {
		store = NewStorage(s.cap, s.validity)
		s.storages[tag] = store
	}
	store.Put(delay)
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
