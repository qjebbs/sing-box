package link

import (
	"fmt"
	"net/url"
	"strconv"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ Link = (*AnyTLS)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "AnyTLS",
		Scheme: []string{"anytls"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseAnyTLS(u)
		},
	}))
}

// AnyTLS represents a parsed anytls link
type AnyTLS struct {
	Auth     string `json:"auth,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     uint16 `json:"port,omitempty"`
	SNI      string `json:"sni,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`

	Remarks string `json:"remarks,omitempty"`
}

// ParseAnyTLS parses a anytls link
//
// https://github.com/anytls/anytls-go/blob/main/docs/uri_scheme.md
func ParseAnyTLS(u *url.URL) (*AnyTLS, error) {
	if u.Scheme != "anytls" {
		return nil, E.New("not a anytls link")
	}
	port := uint16(443)
	if u.Port() != "" {
		p, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil {
			return nil, E.Cause(err, "invalid port")
		}
		port = uint16(p)
	}
	link := &AnyTLS{}
	link.Host = u.Hostname()
	link.Port = port
	link.Remarks = u.Fragment

	if uname := u.User.Username(); uname != "" {
		if pass, ok := u.User.Password(); ok {
			link.Auth = pass
		} else {
			link.Auth = uname
		}
	}

	queries := u.Query()
	for key, values := range queries {
		switch key {
		case "sni":
			link.SNI = values[0]
		case "insecure":
			link.Insecure = values[0] == "1"
		}
	}
	return link, nil
}

// Outbound implements the Link interface
func (l *AnyTLS) Outbound(opt option.ProviderOutboundsOptions) (*option.Outbound, error) {
	password := l.Auth
	o := &option.AnyTLSOutboundOptions{
		DialerOptions: opt.DialerOptions,
		ServerOptions: option.ServerOptions{
			Server:     l.Host,
			ServerPort: l.Port,
		},
		Password: password,
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: &option.OutboundTLSOptions{
				Enabled:    true,
				ServerName: l.SNI,
				Insecure:   l.Insecure,
			},
		},
	}
	return &option.Outbound{
		Type:    C.TypeAnyTLS,
		Tag:     l.Remarks,
		Options: o,
	}, nil
}

// URL implements the Link interface
func (l *AnyTLS) URL() (string, error) {
	var uri url.URL
	uri.Scheme = "anytls"
	if l.Port == 0 || l.Port == 443 {
		uri.Host = l.Host
	} else {
		uri.Host = fmt.Sprintf("%s:%d", l.Host, l.Port)
	}
	uri.Fragment = l.Remarks
	uri.User = url.User(l.Auth)

	query := uri.Query()
	if l.SNI != "" {
		query.Set("sni", l.SNI)
	}
	if l.Insecure {
		query.Set("insecure", "1")
	}

	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
