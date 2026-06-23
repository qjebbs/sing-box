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
	User         string             `json:"user,omitempty"`
	Auth         string             `json:"auth,omitempty"`
	Host         string             `json:"host,omitempty"`
	Port         uint16             `json:"port,omitempty"`
	Ports        HysteriaPortRanges `json:"ports,omitempty"`
	Obfs         string             `json:"obfs,omitempty"`
	ObfsPassword string             `json:"obfs_password,omitempty"`
	SNI          string             `json:"sni,omitempty"`
	Insecure     bool               `json:"insecure,omitempty"`
	PinSHA256    string             `json:"pin_sha256,omitempty"`

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
			link.User = uname
			link.Auth = pass
		} else {
			link.Auth = uname
		}
	}

	queries := u.Query()
	for key, values := range queries {
		switch key {
		case "obfs":
			link.Obfs = values[0]
		case "obfs-password":
			link.ObfsPassword = values[0]
		case "sni":
			link.SNI = values[0]
		case "insecure":
			link.Insecure = values[0] == "1"
		case "pinSHA256":
			link.PinSHA256 = values[0]
		case "mport", "mports", "ports":
			ports, err := ParsePortRanges(values[0])
			if err != nil {
				return nil, E.Cause(err, "invalid", key, "=", values[0])
			}
			link.Ports = ports
		}
	}
	return link, nil
}

// Outbound implements the Link interface
func (l *Hysteria2) Outbound() (*option.Outbound, error) {
	insecure := l.Insecure
	if l.PinSHA256 != "" {
		// return nil, E.New("pinSHA256 is not unsupported")

		// sing-box does not support pinSHA256 option.
		// once pinSHA256 is set, we assume the link requires insecure, or the pinSHA256 should not be provided.
		// the hy2 doc says it's recommended to set pinSHA256 when insecure is set.
		insecure = true
	}
	password := l.Auth
	if l.User != "" {
		password = fmt.Sprintf("%s:%s", l.User, l.Auth)
	}
	var obfs *option.Hysteria2Obfs
	if l.Obfs != "" || l.ObfsPassword != "" {
		obfs = &option.Hysteria2Obfs{
			Type:     l.Obfs,
			Password: l.ObfsPassword,
		}
	}
	return &option.Outbound{
		Type: C.TypeHysteria2,
		Tag:  l.Remarks,
		Options: &option.Hysteria2OutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     l.Host,
				ServerPort: l.Port,
			},
			ServerPorts: l.Ports.SingBoxPorts(),
			Password:    password,
			Obfs:        obfs,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: &option.OutboundTLSOptions{
					Enabled:    true,
					ServerName: l.SNI,
					Insecure:   insecure,
					// CertificatePublicKeySHA256: [][]byte{[]byte(l.PinSHA256)},
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
		uri.User = url.UserPassword(l.User, l.Auth)
	case l.Auth != "":
		uri.User = url.User(l.Auth)
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
	if len(l.Ports) > 0 {
		query.Set("mport", l.Ports.String())
	}
	if l.Insecure {
		query.Set("insecure", "1")
	}
	if l.PinSHA256 != "" {
		query.Set("pinSHA256", l.PinSHA256)
	}

	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
