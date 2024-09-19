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

var _ Link = (*Hysteria)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "Hysteria",
		Scheme: []string{"hysteria"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseHysteria(u)
		},
	}))
}

// Hysteria represents a parsed hysteria link
type Hysteria struct {
	Host      string `json:"host,omitempty"`
	Port      uint16 `json:"port,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Auth      string `json:"auth,omitempty"`
	Peer      string `json:"peer,omitempty"`
	Insecure  bool   `json:"insecure,omitempty"`
	ALPN      string `json:"alpn,omitempty"`
	UpMpbs    uint64 `json:"up_mpbs,omitempty"`
	DownMpbs  uint64 `json:"down_mpbs,omitempty"`
	Obfs      string `json:"obfs,omitempty"`
	ObfsParam string `json:"obfs_param,omitempty"`
	Remarks   string `json:"remarks,omitempty"`
}

// ParseHysteria parses a hysteria link
//
// https://v1.hysteria.network/zh/docs/uri-scheme/
func ParseHysteria(u *url.URL) (*Hysteria, error) {
	if u.Scheme != "hysteria" {
		return nil, E.New("not a hysteria link")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return nil, E.Cause(err, "invalid port")
	}
	link := &Hysteria{}
	link.Host = u.Hostname()
	link.Port = uint16(port)
	link.Remarks = u.Fragment
	if link.Host == "" {
		return nil, E.New("host is required")
	}
	if link.Port == 0 {
		return nil, E.New("port is required")
	}
	queries := u.Query()
	for key, values := range queries {
		switch key {
		case "protocol":
			protocol := "udp"
			switch values[0] {
			case "", "udp":
				break
			case "wechat-video", "faketcp":
				return nil, E.New("unsupported protocol: " + values[0])
			default:
				return nil, E.New("unknown network: " + values[0])
			}
			link.Protocol = protocol
		case "auth":
			link.Auth = values[0]
		case "peer":
			link.Peer = values[0]
		case "insecure", "allowInsecure":
			link.Insecure = values[0] == "1"
		case "upmbps":
			val, err := strconv.ParseUint(values[0], 10, 64)
			if err != nil {
				return nil, E.Cause(err, "invalid upmbps ", values[0])
			}
			link.UpMpbs = val
		case "downmbps":
			val, err := strconv.ParseUint(values[0], 10, 64)
			if err != nil {
				return nil, E.Cause(err, "invalid downmbps ", values[0])
			}
			link.DownMpbs = val
		case "alpn":
			link.ALPN = values[0]
		case "obfs":
			link.Obfs = values[0]
		case "obfsParam":
			link.ObfsParam = values[0]
		case "remarks":
			link.Remarks = values[0]
		}
	}
	if link.UpMpbs == 0 {
		return nil, E.New("upmbps is required")
	}
	if link.DownMpbs == 0 {
		return nil, E.New("downmbps is required")
	}
	return link, nil
}

// Outbound implements the Link interface
func (l *Hysteria) Outbound() (*option.Outbound, error) {
	return &option.Outbound{
		Type: C.TypeHysteria,
		Tag:  l.Remarks,
		HysteriaOptions: option.HysteriaOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     l.Host,
				ServerPort: l.Port,
			},
			AuthString: l.Auth,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: &option.OutboundTLSOptions{
					Enabled:    true,
					ALPN:       []string{l.ALPN},
					ServerName: l.Peer,
					Insecure:   l.Insecure,
				},
			},
			UpMbps:   int(l.UpMpbs),
			DownMbps: int(l.DownMpbs),
			Obfs:     l.ObfsParam,
		},
	}, nil
}

// URL implements the Link interface
func (l *Hysteria) URL() (string, error) {
	var uri url.URL
	uri.Scheme = "hysteria"
	uri.Host = fmt.Sprintf("%s:%d", l.Host, l.Port)
	uri.Fragment = l.Remarks
	query := uri.Query()
	if l.Protocol != "" {
		query.Set("protocol", l.Protocol)
	}
	if l.Auth != "" {
		query.Set("auth", l.Auth)
	}
	if l.Peer != "" {
		query.Set("peer", l.Peer)
	}
	if l.Insecure {
		query.Set("insecure", "1")
	}
	if l.UpMpbs != 0 {
		query.Set("upmbps", strconv.FormatUint(l.UpMpbs, 10))
	}
	if l.DownMpbs != 0 {
		query.Set("downmbps", strconv.FormatUint(l.DownMpbs, 10))
	}
	if l.ALPN != "" {
		query.Set("alpn", l.ALPN)
	}
	if l.Obfs != "" {
		query.Set("obfs", l.Obfs)
	}
	if l.ObfsParam != "" {
		query.Set("obfsParam", l.ObfsParam)
	}
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
