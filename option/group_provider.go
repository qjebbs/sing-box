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
	URL       string   `json:"url,omitempty"`
	Interval  Duration `json:"interval,omitempty"`
	Tolerance uint16   `json:"tolerance,omitempty"`
}

// ProviderGroupCommonOption is the common options for group outbounds with providers support
type ProviderGroupCommonOption struct {
	Outbounds []string `json:"outbounds"`
	Providers []string `json:"providers"`
	Exclude   string   `json:"exclude,omitempty"`
	Include   string   `json:"include,omitempty"`
}
