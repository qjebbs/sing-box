package dialer

import (
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	dns "github.com/sagernet/sing-dns"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

func New(router adapter.Router, options option.DialerOptions) (N.Dialer, error) {
	return new(router, options, "")
}

func NewChainRedirectable(router adapter.Router, tag string, options option.DialerOptions) (N.Dialer, error) {
	return new(router, options, tag)
}

func new(router adapter.Router, options option.DialerOptions, redirectableTag string) (N.Dialer, error) {
	detourable := true
	if options.IsWireGuardListener {
		detourable = false
	}
	if router == nil {
		return NewDefault(nil, options)
	}
	var (
		dialer N.Dialer
		err    error
	)
	if options.Detour == "" {
		dialer, err = NewDefault(router, options)
		if err != nil {
			return nil, err
		}
		if redirectableTag != "" {
			dialer = NewChainRedirectDialer(redirectableTag, detourable, dialer, dialer)
		}
	} else if !detourable {
		return nil, E.New("[", redirectableTag, "] ", "detour is not supported")
	} else {
		dialer = NewDetour(router, options.Detour)
		if redirectableTag != "" {
			defDialer, err := NewDefault(router, options)
			if err != nil {
				return nil, err
			}
			dialer = NewChainRedirectDialer(redirectableTag, true, dialer, defDialer)
		}
	}
	if options.Detour == "" {
		dialer = NewResolveDialer(
			router,
			dialer,
			options.Detour == "" && !options.TCPFastOpen,
			dns.DomainStrategy(options.DomainStrategy),
			time.Duration(options.FallbackDelay))
	}
	return dialer, nil
}
