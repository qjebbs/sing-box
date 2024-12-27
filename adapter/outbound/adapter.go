package outbound

import (
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/provider"
	E "github.com/sagernet/sing/common/exceptions"
)

type Adapter struct {
	outboundType string
	outboundTag  string
	network      []string
	dependencies []string
}

func NewAdapter(outboundType string, outboundTag string, network []string, dependencies []string) Adapter {
	return Adapter{
		outboundType: outboundType,
		outboundTag:  outboundTag,
		network:      network,
		dependencies: dependencies,
	}
}

func NewAdapterWithDialerOptions(outboundType string, outboundTag string, network []string, dialOptions option.DialerOptions) Adapter {
	var dependencies []string
	if dialOptions.Detour != "" {
		dependencies = []string{dialOptions.Detour}
	}
	return NewAdapter(outboundType, outboundTag, network, dependencies)
}

func (a *Adapter) Type() string {
	return a.outboundType
}

func (a *Adapter) Tag() string {
	return a.outboundTag
}

func (a *Adapter) Network() []string {
	return a.network
}

func (a *Adapter) Dependencies() []string {
	return a.dependencies
}

func NewGroupAdapter(
	outboundType string, outboundTag string, network []string,
	router adapter.Router, options option.ProviderGroupCommonOption,
) GroupAdapter {
	adapter := GroupAdapter{
		Adapter: NewAdapter(outboundType, outboundTag, network, options.Outbounds),
		router:  router,
		options: options,
	}
	return adapter
}

type GroupAdapter struct {
	Adapter

	router         adapter.Router
	options        option.ProviderGroupCommonOption
	providers      []adapter.Provider
	providersByTag map[string]adapter.Provider
}

func (a *GroupAdapter) All() []string {
	tags := make([]string, 0)
	for _, p := range a.providers {
		for _, outbound := range p.Outbounds() {
			tags = append(tags, outbound.Tag())
		}
	}
	return tags
}

func (a *GroupAdapter) InitProviders(om adapter.OutboundManager, pm adapter.ProviderManager) error {
	if len(a.options.Outbounds)+len(a.options.Providers) == 0 {
		return E.New("missing outbound and provider tags")
	}
	outbounds := make([]adapter.Outbound, 0, len(a.options.Outbounds))
	for _, tag := range a.options.Outbounds {
		detour, ok := om.Outbound(tag)
		if !ok {
			return E.New("outbound not found: ", tag)
		}
		outbounds = append(outbounds, detour)
	}
	providersByTag := make(map[string]adapter.Provider)
	providers := make([]adapter.Provider, 0, len(a.options.Providers)+1)
	if len(outbounds) > 0 {
		providers = append(providers, provider.NewMemory(outbounds))
	}
	var err error
	for _, tag := range a.options.Providers {
		p, ok := pm.Provider(tag)
		if !ok {
			return E.New("provider not found: ", tag)
		}
		if a.options.Exclude != "" || a.options.Include != "" {
			p, err = provider.NewFiltered(p, a.options.Exclude, a.options.Include)
			if err != nil {
				return E.New("failed to create filtered provider: ", err)
			}
		}
		providers = append(providers, p)
		providersByTag[tag] = p
	}
	a.providers = providers
	a.providersByTag = providersByTag
	return nil
}

func (a *GroupAdapter) Outbound(tag string) (adapter.Outbound, bool) {
	for _, p := range a.providers {
		if outbound, ok := p.Outbound(tag); ok {
			return outbound, true
		}
	}
	return nil, false
}

func (a *GroupAdapter) Outbounds() []adapter.Outbound {
	var outbounds []adapter.Outbound
	for _, p := range a.providers {
		outbounds = append(outbounds, p.Outbounds()...)
	}
	return outbounds
}

func (a *GroupAdapter) Provider(tag string) (adapter.Provider, bool) {
	provider, ok := a.providersByTag[tag]
	return provider, ok
}

func (a *GroupAdapter) Providers() []adapter.Provider {
	return a.providers
}
