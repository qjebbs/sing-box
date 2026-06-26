package healthcheck

import (
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	boxConstant "github.com/sagernet/sing-box/constant"
)

var _ adapter.Provider = (*mergedProvider)(nil)

// mergedProvider aggregates providers from multiple namespaces.
// It is passed to HealthCheck once at construction and never replaced;
// Outbounds() dynamically reads from all registered providers with
// outbound-level deduplication by tag.
type mergedProvider struct {
	mu        sync.RWMutex
	providers map[string][]adapter.Provider
}

func newMergedProvider() *mergedProvider {
	return &mergedProvider{
		providers: make(map[string][]adapter.Provider),
	}
}

// Set replaces the full provider list for the given namespace.
func (m *mergedProvider) Set(namespace string, providers []adapter.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(providers) == 0 {
		delete(m.providers, namespace)
		return
	}
	m.providers[namespace] = providers
}

func (m *mergedProvider) Tag() string          { return "" }
func (m *mergedProvider) Type() string         { return boxConstant.TypeHealthChecker }
func (m *mergedProvider) Update() error        { return nil }
func (m *mergedProvider) UpdatedAt() time.Time { return time.Time{} }
func (m *mergedProvider) Wait() {
	m.mu.RLock()
	providers := make([]adapter.Provider, 0)
	for _, ps := range m.providers {
		for _, p := range ps {
			providers = append(providers, p)
		}
	}
	m.mu.RUnlock()
	for _, p := range providers {
		p.Wait()
	}
}

func (m *mergedProvider) NamespacedWait(namespace string) {
	m.mu.RLock()
	providers, ok := m.providers[namespace]
	m.mu.RUnlock()
	if !ok {
		return
	}
	for _, p := range providers {
		p.Wait()
	}
}

func (m *mergedProvider) Outbound(tag string) (adapter.Outbound, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, providers := range m.providers {
		for _, p := range providers {
			if o, ok := p.Outbound(tag); ok {
				return o, ok
			}
		}
	}
	return nil, false
}

func (m *mergedProvider) NamespacedOutbound(namespace, tag string) (adapter.Outbound, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	providers, ok := m.providers[namespace]
	if !ok {
		return nil, false
	}
	for _, p := range providers {
		if o, ok := p.Outbound(tag); ok {
			return o, ok
		}
	}
	return nil, false
}

func (m *mergedProvider) Outbounds() []adapter.Outbound {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]struct{})
	var outbounds []adapter.Outbound
	for _, providers := range m.providers {
		for _, p := range providers {
			for _, o := range p.Outbounds() {
				tag := o.Tag()
				if _, ok := seen[tag]; ok {
					continue
				}
				seen[tag] = struct{}{}
				outbounds = append(outbounds, o)
			}
		}
	}
	return outbounds
}

func (m *mergedProvider) NamespacedOutbounds(namespace string) []adapter.Outbound {
	m.mu.RLock()
	defer m.mu.RUnlock()
	providers, ok := m.providers[namespace]
	if !ok {
		return nil
	}
	seen := make(map[string]struct{})
	var outbounds []adapter.Outbound
	for _, p := range providers {
		for _, o := range p.Outbounds() {
			tag := o.Tag()
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			outbounds = append(outbounds, o)
		}
	}
	return outbounds
}
