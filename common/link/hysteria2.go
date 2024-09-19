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

var _ Link = (*Hysteria2)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "Hysteria2",
		Scheme: []string{"hysteria2", "hy2"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseHysteria2(u)
		},
	}))
}

// Hysteria2 represents a parsed hysteria2 link
type Hysteria2 struct {
	User         string `json:"user,omitempty"`
	Auth         string `json:"auth,omitempty"`
	Host         string `json:"host,omitempty"`
	Port         uint16 `json:"port,omitempty"`
	Obfs         string `json:"obfs,omitempty"`
	ObfsPassword string `json:"obfs_password,omitempty"`
	SNI          string `json:"sni,omitempty"`
	Insecure     bool   `json:"insecure,omitempty"`
	PingSHA256   string `json:"pin_sha256,omitempty"`

	Remarks string `json:"remarks,omitempty"`
}

// ParseHysteria2 parses a hysteria2 link
//
// https://v2.hysteria.network/zh/docs/developers/URI-Scheme/
func ParseHysteria2(u *url.URL) (*Hysteria2, error) {
	if u.Scheme != "hysteria2" && u.Scheme != "hy2" {
		return nil, E.New("not a hysteria2 link")
	}
	port := uint16(443)
	if u.Port() != "" {
		p, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil {
			return nil, E.Cause(err, "invalid port")
		}
		port = uint16(p)
	}
	link := &Hysteria2{}
	link.Host = u.Hostname()
	link.Port = port
	link.Remarks = u.Fragment

	if uname := u.User.Username(); uname != "" {
		if pass, ok := u.User.Password(); ok {
			user, err := url.QueryUnescape(uname)
			if err != nil {
				return nil, err
			}
			password, err := url.QueryUnescape(pass)
			if err != nil {
				return nil, err
			}
			link.User = user
			link.Auth = password
		} else {
			auth, err := url.QueryUnescape(uname)
			if err != nil {
				return nil, E.Cause(err, "invalid auth")
			}
			link.Auth = auth
		}
	}

	queries := u.Query()
	for key, values := range queries {
		switch key {
		case "obfs":
			if values[0] != "salamander" {
				return nil, E.New("unsupported obfs: " + values[0])
			}
			link.Obfs = values[0]
		case "obfs-password":
			link.ObfsPassword = values[0]
		case "sni":
			link.SNI = values[0]
		case "insecure":
			link.Insecure = values[0] == "1"
		case "pinSHA256":
			if values[0] != "" {
				return nil, E.New("pinSHA256 is not unsupported")
			}
			link.PingSHA256 = values[0]
		}
	}
	return link, nil
}

// Outbound implements the Link interface
func (l *Hysteria2) Outbound() (*option.Outbound, error) {
	password := l.Auth
	if l.User != "" {
		password = fmt.Sprintf("%s:%s", l.User, l.Auth)
	}
	return &option.Outbound{
		Type: C.TypeHysteria2,
		Tag:  l.Remarks,
		Hysteria2Options: option.Hysteria2OutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     l.Host,
				ServerPort: l.Port,
			},
			Password: password,
			Obfs: &option.Hysteria2Obfs{
				Type:     l.Obfs,
				Password: l.ObfsPassword,
			},
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: &option.OutboundTLSOptions{
					Enabled:    true,
					ServerName: l.SNI,
					Insecure:   l.Insecure,
				},
			},
		},
	}, nil
}

// URL implements the Link interface
func (l *Hysteria2) URL() (string, error) {
	var uri url.URL
	uri.Scheme = "hysteria2"
	if l.Port == 0 || l.Port == 443 {
		uri.Host = l.Host
	} else {
		uri.Host = fmt.Sprintf("%s:%d", l.Host, l.Port)
	}
	uri.Fragment = l.Remarks
	switch {
	case l.User != "" && l.Auth != "":
		uri.User = url.UserPassword(url.QueryEscape(l.User), url.QueryEscape(l.Auth))
	case l.Auth != "":
		uri.User = url.User(url.QueryEscape(l.Auth))
	}

	query := uri.Query()
	if l.Obfs != "" {
		query.Set("obfs", l.Obfs)
	}
	if l.ObfsPassword != "" {
		query.Set("obfs-password", l.ObfsPassword)
	}
	if l.SNI != "" {
		query.Set("sni", l.SNI)
	}
	if l.Insecure {
		query.Set("insecure", "1")
	}
	if l.PingSHA256 != "" {
		query.Set("pinSHA256", l.PingSHA256)
	}

	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
