package option

// ProviderSelectorOptions is the options for selector outbounds with providers support
type ProviderSelectorOptions struct {
	ProviderGroupCommonOption
	Default                   string `json:"default,omitempty" reference:"outbound"`
	InterruptExistConnections bool   `json:"interrupt_exist_connections,omitempty"`
}

// ProviderURLTestOptions is the options for urltest outbounds with providers support
type ProviderURLTestOptions struct {
	ProviderGroupCommonOption
	HealthCheck               *HealthCheckOptions `json:"health_check,omitempty"`
	Tolerance                 uint16              `json:"tolerance,omitempty"`
	InterruptExistConnections bool                `json:"interrupt_exist_connections,omitempty"`
}

// ChainOptions is the chain of outbounds
type ChainOptions struct {
	Outbounds []string `json:"outbounds" reference:"outbound"`
}

// ProviderGroupCommonOption is the common options for group outbounds with providers support
type ProviderGroupCommonOption struct {
	Outbounds    []string `json:"outbounds" reference:"outbound"`
	Providers    []string `json:"providers" reference:"provider"`
	AllProviders bool     `json:"all_providers,omitempty"`
	Exclude      string   `json:"exclude,omitempty"`
	Include      string   `json:"include,omitempty"`
}
