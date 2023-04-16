package provider

import (
	"time"

	"github.com/sagernet/sing-box/adapter"
)

var _ adapter.Provider = (*Memory)(nil)

// Memory is a in memory outbounds provider.
type Memory struct {
	outbounds      []adapter.Outbound
	outboundsByTag map[string]adapter.Outbound
}

// NewMemory creates a new memory provider.
func NewMemory(outbounds []adapter.Outbound) *Memory {
	tags := make(map[string]adapter.Outbound)
	for _, outbound := range outbounds {
		tags[outbound.Tag()] = outbound
	}
	return &Memory{
		outbounds:      outbounds,
		outboundsByTag: tags,
	}
}

// Outbounds returns all the outbounds from the provider.
func (s *Memory) Outbounds() []adapter.Outbound {
	return s.outbounds
}

// Outbound returns the outbound from the provider.
func (s *Memory) Outbound(tag string) (adapter.Outbound, bool) {
	detour, ok := s.outboundsByTag[tag]
	return detour, ok
}

// Tag returns the tag of the provider.
func (s *Memory) Tag() string {
	return ""
}

// Start starts the provider.
func (s *Memory) Start() error {
	return nil
}

// Close closes the service.
func (s *Memory) Close() error {
	return nil
}

// Update closes the service.
func (s *Memory) Update() error {
	return nil
}

// UpdatedAt implements adapter.Provider
func (s *Memory) UpdatedAt() time.Time {
	return time.Now()
}

// Wait implements adapter.Provider
func (s *Memory) Wait() {}
