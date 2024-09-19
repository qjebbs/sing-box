package provider

import (
	"regexp"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

var _ adapter.Provider = (*Filtered)(nil)

// Filtered is a filtered outbounds provider.
type Filtered struct {
	sync.Mutex
	upstream adapter.Provider
	exclude  *regexp.Regexp
	include  *regexp.Regexp

	outbounds      []adapter.Outbound
	outboundsByTag map[string]adapter.Outbound
	updatedAt      time.Time
}

// NewFiltered creates a new filtered provider.
func NewFiltered(upstream adapter.Provider, exclude, include string) (*Filtered, error) {
	var (
		err                          error
		excludeRegexp, includeRegexp *regexp.Regexp
	)
	if exclude != "" {
		excludeRegexp, err = regexp.Compile(exclude)
		if err != nil {
			return nil, err
		}
	}
	if include != "" {
		includeRegexp, err = regexp.Compile(include)
		if err != nil {
			return nil, err
		}
	}
	return &Filtered{
		upstream: upstream,
		exclude:  excludeRegexp,
		include:  includeRegexp,
	}, nil
}

// Outbounds returns all the outbounds from the provider.
func (s *Filtered) Outbounds() []adapter.Outbound {
	s.Lock()
	defer s.Unlock()
	s.update()
	return s.outbounds
}

// Outbound returns the outbound from the provider.
func (s *Filtered) Outbound(tag string) (adapter.Outbound, bool) {
	s.Lock()
	defer s.Unlock()
	s.update()
	detour, ok := s.outboundsByTag[tag]
	return detour, ok
}

// Tag returns the tag of the provider.
func (s *Filtered) Tag() string {
	return s.upstream.Tag()
}

// Start starts the provider.
func (s *Filtered) Start() error {
	return s.upstream.Start()
}

// Close closes the service.
func (s *Filtered) Close() error {
	return s.upstream.Close()
}

// Update updates the provider.
func (s *Filtered) Update() error {
	s.Lock()
	defer s.Unlock()
	err := s.upstream.Update()
	if err != nil {
		return err
	}
	s.update()
	return nil
}

// UpdatedAt implements adapter.Provider
func (s *Filtered) UpdatedAt() time.Time {
	s.Lock()
	defer s.Unlock()
	return s.updatedAt
}

// Wait implements adapter.Provider
func (s *Filtered) Wait() {
	s.upstream.Wait()
}

func (s *Filtered) update() {
	if s.updatedAt.After(s.upstream.UpdatedAt()) {
		return
	}
	s.outbounds = nil
	s.outboundsByTag = make(map[string]adapter.Outbound)
	for _, outbound := range s.upstream.Outbounds() {
		if s.exclude != nil && s.exclude.MatchString(outbound.Tag()) {
			continue
		}
		if s.include != nil && !s.include.MatchString(outbound.Tag()) {
			continue
		}
		s.outbounds = append(s.outbounds, outbound)
		s.outboundsByTag[outbound.Tag()] = outbound
	}
	s.updatedAt = s.upstream.UpdatedAt()
}
