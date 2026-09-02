package option

import (
	"context"

	"github.com/sagernet/sing-box/schema"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badjson"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
)

type ProviderOptionsRegistry interface {
	OptionTypes() []string
	CreateOptions(providerType string) (any, bool)
}
type _Provider struct {
	Type    string `json:"type"`
	Tag     string `json:"tag"`
	Options any    `json:"-"`
}

type Provider _Provider

func (h *Provider) MarshalJSONContext(ctx context.Context) ([]byte, error) {
	return badjson.MarshallObjectsContext(ctx, (*_Provider)(h), h.Options)
}

func (h *Provider) UnmarshalJSONContext(ctx context.Context, content []byte) error {
	err := json.UnmarshalContext(ctx, content, (*_Provider)(h))
	if err != nil {
		return err
	}
	registry := service.FromContext[ProviderOptionsRegistry](ctx)
	if registry == nil {
		return E.New("missing provider options registry in context")
	}
	options, loaded := registry.CreateOptions(h.Type)
	if !loaded {
		return E.New("unknown provider type: ", h.Type)
	}
	err = badjson.UnmarshallExcludedContext(ctx, content, (*_Provider)(h), options)
	if err != nil {
		return err
	}
	h.Options = options
	return nil
}

func (h Provider) DescribeSchema(builder schema.Builder) (*schema.Node, error) {
	return builder.Define("Provider", func() (*schema.Node, error) {
		registry := service.FromContext[ProviderOptionsRegistry](builder.Context())
		if registry == nil {
			return nil, E.New("missing provider options registry in context")
		}
		return registryUnion(builder, registry, nil, true)
	})
}

type RemoteProviderOptions struct {
	URL              string             `json:"url"`
	Interval         badoption.Duration `json:"interval,omitempty"`
	CacheFile        string             `json:"cache_file,omitempty"`
	HTTPClient       *HTTPClientOptions `json:"http_client,omitempty"`
	DisableUserAgent bool               `json:"disable_user_agent,omitempty"`
	// Deprecated: use http_client instead
	DownloadDetour string `json:"download_detour,omitempty" reference:"outbound" schema:"omit"`

	Exclude string `json:"exclude,omitempty"`
	Include string `json:"include,omitempty"`

	DedupHost     bool `json:"dedup_host,omitempty"`
	DedupHostPort bool `json:"dedup_host_port,omitempty"`

	OutboundsDefault ProviderOutboundsOptions `json:"outbounds_default,omitempty"`
}

// ProviderOutboundsOptions is the default options for outbounds of a provider.
type ProviderOutboundsOptions struct {
	DialerOptions
}
