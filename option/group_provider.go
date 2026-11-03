package option

import "github.com/sagernet/sing/common/json/badoption"

// ProviderSelectorOptions is the options for selector outbounds with providers support
type ProviderSelectorOptions struct {
	ProviderGroupCommonOption
	Default                   string `json:"default,omitempty" reference:"outbound"`
	InterruptExistConnections bool   `json:"interrupt_exist_connections,omitempty"`
}

// ProviderURLTestOptions is the options for urltest outbounds with providers support
type ProviderURLTestOptions struct {
	ProviderGroupCommonOption
	HealthCheck *HealthCheckOptions `json:"health_check,omitempty"`
	// objective ranks the nodes, it is quick or steady. Default is quick, the median delay
	// plus the jitter and the failures.
	Objective string `json:"objective,omitempty"`
	// tolerance is the absolute band of the best node: the node in use keeps its place
	// while its score is at most this far above the best one, and the same band decides
	// whether a node that was just replaced keeps the connections it already carries.
	// Default is 10ms for steady and 50ms for quick.
	Tolerance                 badoption.Duration `json:"tolerance,omitempty"`
	InterruptExistConnections bool               `json:"interrupt_exist_connections,omitempty"`
}

// RelayOptions is the chain of outbounds, which the connection passes through in the order
// they are listed.
type RelayOptions struct {
	Outbounds []string `json:"outbounds" reference:"outbound"`
}

// FallbackOutboundOptions is the options for the fallback outbound
type FallbackOutboundOptions struct {
	ProviderGroupCommonOption
	// HealthCheck is optional here, unlike the load balancing groups: when it is not
	// set, no health checker is attached at all, no check round is ever started and
	// the outbounds are simply tried in the declared order.
	HealthCheck *HealthCheckOptions `json:"health_check,omitempty"`
}

// ProviderGroupCommonOption is the common options for group outbounds with providers support
type ProviderGroupCommonOption struct {
	Outbounds    []string `json:"outbounds" reference:"outbound"`
	Providers    []string `json:"providers" reference:"provider"`
	AllProviders bool     `json:"all_providers,omitempty"`
	Exclude      string   `json:"exclude,omitempty"`
	Include      string   `json:"include,omitempty"`
}
