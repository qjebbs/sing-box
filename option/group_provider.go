package option

import (
	"github.com/sagernet/sing/common/json/badoption"
)

// ProviderSelectorOptions is the options for selector outbounds with providers support
type ProviderSelectorOptions struct {
	ProviderGroupCommonOption
	Default                   string `json:"default,omitempty"`
	InterruptExistConnections bool   `json:"interrupt_exist_connections,omitempty"`
}

// ProviderURLTestOptions is the options for urltest outbounds with providers support
type ProviderURLTestOptions struct {
	ProviderGroupCommonOption
	URL       string             `json:"url,omitempty"`
	Interval  badoption.Duration `json:"interval,omitempty"`
	Tolerance uint16             `json:"tolerance,omitempty"`
}

// ChainOptions is the chain of outbounds
type ChainOptions struct {
	Outbounds []string `json:"outbounds"`
}

// ProviderGroupCommonOption is the common options for group outbounds with providers support
type ProviderGroupCommonOption struct {
	Outbounds []string `json:"outbounds"`
	Providers []string `json:"providers"`
	Exclude   string   `json:"exclude,omitempty"`
	Include   string   `json:"include,omitempty"`
}
