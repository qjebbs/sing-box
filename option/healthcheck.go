package option

import "github.com/sagernet/sing/common/json/badoption"

// HealthCheckOptions is the settings for health check
type HealthCheckOptions struct {
	Interval    badoption.Duration `json:"interval"`
	Sampling    uint               `json:"sampling"`
	Destination string             `json:"destination"`
	DetourOf    []string           `json:"detour_of,omitempty"`
}
