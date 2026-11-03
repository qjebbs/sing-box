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
	// objective ranks the nodes, it is leastping or leastload. Default is leastping,
	// the median delay plus the failures.
	Objective string `json:"objective,omitempty"`
	// rtt_tolerance is the relative score band of the best node within which the node in
	// use keeps its place, for the leastping objective, e.g. 0.2 keeps it while it is at
	// most 20% worse than the best one. The same band decides whether a node that was
	// just replaced keeps the connections it already carries. Requires the objective to
	// be set to leastping.
	RTTTolerance float32 `json:"rtt_tolerance,omitempty"`
	// jitter_tolerance is the absolute band of the best node, for the leastload
	// objective, e.g. 10ms keeps the node in use while its jitter is at most 10ms above
	// the best one. Requires the objective to be set to leastload.
	JitterTolerance           badoption.Duration `json:"jitter_tolerance,omitempty"`
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
