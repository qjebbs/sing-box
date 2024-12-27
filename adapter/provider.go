package adapter

import (
	"context"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

type Provider interface {
	Service
	Tag() string
	Update() error
	UpdatedAt() time.Time
	Wait()
	Outbounds() []Outbound
	Outbound(tag string) (Outbound, bool)
}
type ProviderInfoer interface {
	Provider
	Info() *ProviderInfo
}

type ProviderRegistry interface {
	option.ProviderOptionsRegistry
	CreateProvider(ctx context.Context, router Router, logFactory log.Factory, tag string, providerType string, options any) (Provider, error)
}

type ProviderManager interface {
	Lifecycle
	Providers() []Provider
	Provider(tag string) (Provider, bool)
	// Remove(tag string) error
	// Create(ctx context.Context, router Router, logger log.ContextLogger, tag string, options any) error
}

// ProviderInfo is the info of provider
type ProviderInfo struct {
	Download int `json:"Download"`
	Upload   int `json:"Upload"`
	Total    int `json:"Total"`
	Expire   int `json:"Expire"`
}
