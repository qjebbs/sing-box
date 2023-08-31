package dialer

import (
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	dns "github.com/sagernet/sing-dns"
	"github.com/sagernet/sing/common"
	N "github.com/sagernet/sing/common/network"
)

func MustNew(router adapter.Router, options option.DialerOptions) N.Dialer {
	return common.Must1(New(router, options))
}

func New(router adapter.Router, options option.DialerOptions) (N.Dialer, error) {
	var (
		dialer N.Dialer
		err    error
	)
	if options.Detour == "" {
		dialer, err = NewDefault(router, options)
		if err != nil {
			return nil, err
		}
	} else {
		dialer = NewDetour(router, options.Detour)
	}
	domainStrategy := dns.DomainStrategy(options.DomainStrategy)
	if domainStrategy != dns.DomainStrategyAsIS || options.Detour == "" {
		dialer = NewResolveDialer(router, dialer, domainStrategy, time.Duration(options.FallbackDelay))
	}
	return dialer, nil
}

func MustNewRedirectable(router adapter.Router, tag string, options option.DialerOptions) N.Dialer {
	return common.Must1(NewRedirectable(router, tag, options))
}

func NewRedirectable(router adapter.Router, tag string, options option.DialerOptions) (N.Dialer, error) {
	var (
		dialer N.Dialer
		err    error
	)
	defDialer, err := NewDefault(router, options)
	if err != nil {
		return nil, err
	}
	if options.Detour == "" {
		dialer = NewChainRedirectDialer(tag, defDialer, defDialer)
	} else {
		dialer = NewDetour(router, options.Detour)
		dialer = NewChainRedirectDialer(tag, dialer, defDialer)
	}
	domainStrategy := dns.DomainStrategy(options.DomainStrategy)
	if domainStrategy != dns.DomainStrategyAsIS || options.Detour == "" {
		dialer = NewResolveDialer(router, dialer, domainStrategy, time.Duration(options.FallbackDelay))
	}
	return dialer, nil
}
