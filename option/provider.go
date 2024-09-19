package option

type Provider struct {
	Tag              string   `json:"tag"`
	URL              string   `json:"url"`
	Interval         Duration `json:"interval,omitempty"`
	CacheFile        string   `json:"cache_file,omitempty"`
	DownloadDetour   string   `json:"download_detour,omitempty"`
	DisableUserAgent bool     `json:"disable_user_agent,omitempty"`

	Exclude string `json:"exclude,omitempty"`
	Include string `json:"include,omitempty"`

	DialerOptions
}
