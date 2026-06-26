package option

// ProviderSelectorOptions is the options for selector outbounds with providers support
type ProviderSelectorOptions struct {
	ProviderGroupCommonOption
	Default                   string `json:"default,omitempty"`
	InterruptExistConnections bool   `json:"interrupt_exist_connections,omitempty"`
}

// ProviderURLTestOptions is the options for urltest outbounds with providers support
type ProviderURLTestOptions struct {
	ProviderGroupCommonOption
	Checker   string `json:"checker,omitempty"`
	Tolerance uint16 `json:"tolerance,omitempty"`
}

// ChainOptions is the chain of outbounds
type ChainOptions struct {
	Outbounds []string `json:"outbounds"`
}

// ProviderGroupCommonOption is the common options for group outbounds with providers support
type ProviderGroupCommonOption struct {
	Outbounds    []string `json:"outbounds"`
	Providers    []string `json:"providers"`
	AllProviders bool     `json:"all_providers,omitempty"`
	Exclude      string   `json:"exclude,omitempty"`
	Include      string   `json:"include,omitempty"`
}
