package adapter

import (
	"context"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// Provider is the interface of proxy provider
type Provider interface {
	Type() string
	Tag() string
	Update() error
	UpdatedAt() time.Time
	Wait()
	Outbounds() []Outbound
	Outbound(tag string) (Outbound, bool)
}

// ProviderInfoer is the interface of provider with info
type ProviderInfoer interface {
	Provider
	Info() *ProviderInfo
}

// ProviderRegistry is the interface of provider registry
type ProviderRegistry interface {
	option.ProviderOptionsRegistry
	CreateProvider(ctx context.Context, router Router, logFactory log.Factory, tag string, providerType string, options any) (Provider, error)
}

// ProviderManager is the interface of provider manager
type ProviderManager interface {
	Lifecycle
	Providers() []Provider
	Provider(tag string) (Provider, bool)
	Remove(tag string) error
	Create(ctx context.Context, router Router, logFactory log.Factory, tag string, providerType string, options any) error
}

// ProviderInfo is the info of provider
type ProviderInfo struct {
	Download int `json:"Download"`
	Upload   int `json:"Upload"`
	Total    int `json:"Total"`
	Expire   int `json:"Expire"`
}
