package option

type ClashAPIOptions struct {
	ExternalController       string `json:"external_controller,omitempty"`
	ExternalUI               string `json:"external_ui,omitempty"`
	ExternalUIDownloadURL    string `json:"external_ui_download_url,omitempty"`
	ExternalUIDownloadDetour string `json:"external_ui_download_detour,omitempty"`
	Secret                   string `json:"secret,omitempty"`
	DefaultMode              string `json:"default_mode,omitempty"`
	StoreMode                bool   `json:"store_mode,omitempty"`
	StoreSelected            bool   `json:"store_selected,omitempty"`
	StoreFakeIP              bool   `json:"store_fakeip,omitempty"`
	CacheFile                string `json:"cache_file,omitempty"`
	CacheID                  string `json:"cache_id,omitempty"`

	ModeList []string `json:"-"`
}

type Provider struct {
	Tag            string   `json:"tag"`
	URL            string   `json:"url"`
	Interval       Duration `json:"interval,omitempty"`
	CacheFile      string   `json:"cache_file,omitempty"`
	DownloadDetour string   `json:"download_detour,omitempty"`

	Exclude string `json:"exclude,omitempty"`
	Include string `json:"include,omitempty"`

	DialerOptions
}

type SelectorOutboundOptions struct {
	GroupCommonOption
	Default string `json:"default,omitempty"`
}

type URLTestOutboundOptions struct {
	GroupCommonOption
	URL       string   `json:"url,omitempty"`
	Interval  Duration `json:"interval,omitempty"`
	Tolerance uint16   `json:"tolerance,omitempty"`
}

// LoadBalanceOutboundOptions is the options for balancer outbound
type LoadBalanceOutboundOptions struct {
	GroupCommonOption
	Check HealthCheckOptions     `json:"check,omitempty"`
	Pick  LoadBalancePickOptions `json:"pick,omitempty"`
}

type GroupCommonOption struct {
	Outbounds []string `json:"outbounds"`
	Providers []string `json:"providers"`
}

// LoadBalancePickOptions is the options for balancer outbound picking
type LoadBalancePickOptions struct {
	// load balance objective
	Objective string `json:"objective,omitempty"`
	// pick strategy
	Strategy string `json:"strategy,omitempty"`
	// max acceptable failures
	MaxFail uint `json:"max_fail,omitempty"`
	// max acceptable rtt. defalut 0
	MaxRTT Duration `json:"max_rtt,omitempty"`
	// expected nodes count to select
	Expected uint `json:"expected,omitempty"`
	// ping rtt baselines
	Baselines []Duration `json:"baselines,omitempty"`
}

// HealthCheckOptions is the settings for health check
type HealthCheckOptions struct {
	Interval     Duration `json:"interval"`
	Sampling     uint     `json:"sampling"`
	Destination  string   `json:"destination"`
	Connectivity string   `json:"connectivity"`
	DetourOf     []string `json:"detour_of,omitempty"`
}
