package link

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ Link = (*ShadowSocks)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "Shadowsocks",
		Scheme: []string{"ss"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseShadowSocks(u)
		},
	}))
}

// ShadowSocks represents a parsed shadowsocks link
type ShadowSocks struct {
	Method     string `json:"method,omitempty"`
	Password   string `json:"password,omitempty"`
	Address    string `json:"address,omitempty"`
	Port       uint16 `json:"port,omitempty"`
	Ps         string `json:"ps,omitempty"`
	Plugin     string `json:"plugin,omitempty"`
	PluginOpts string `json:"plugin-opts,omitempty"`
}

// ParseShadowSocks parses a shadowsocks link
//
// https://github.com/shadowsocks/shadowsocks-org/wiki/SIP002-URI-Scheme
func ParseShadowSocks(u *url.URL) (*ShadowSocks, error) {
	if u.Scheme != "ss" {
		return nil, E.New("not a ss link")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return nil, E.Cause(err, "invalid port")
	}
	link := &ShadowSocks{}
	link.Address = u.Hostname()
	link.Port = uint16(port)
	link.Ps = u.Fragment
	queries := u.Query()
	for key, values := range queries {
		switch key {
		case "plugin":
			parts := strings.SplitN(values[0], ";", 2)
			link.Plugin = parts[0]
			if len(parts) == 2 {
				link.PluginOpts = parts[1]
			}
		}
	}
	if uname := u.User.Username(); uname != "" {
		if pass, ok := u.User.Password(); ok {
			method, err := url.QueryUnescape(uname)
			if err != nil {
				return nil, err
			}
			password, err := url.QueryUnescape(pass)
			if err != nil {
				return nil, err
			}
			link.Method = method
			link.Password = password
		} else {
			dec, err := base64Decode(uname)
			if err != nil {
				return nil, err
			}
			parts := strings.Split(string(dec), ":")
			link.Method = parts[0]
			if len(parts) > 1 {
				link.Password = parts[1]
			}
		}
	}
	return link, nil
}

// Outbound implements Link
func (l *ShadowSocks) Outbound() (*option.Outbound, error) {
	return &option.Outbound{
		Type: C.TypeShadowsocks,
		Tag:  l.Ps,
		ShadowsocksOptions: option.ShadowsocksOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     l.Address,
				ServerPort: l.Port,
			},
			Method:        l.Method,
			Password:      l.Password,
			Plugin:        l.Plugin,
			PluginOptions: l.PluginOpts,
		},
	}, nil
}

// URL implements Link
func (l *ShadowSocks) URL() (string, error) {
	var uri url.URL
	uri.Scheme = "ss"
	uri.Host = fmt.Sprintf("%s:%d", l.Address, l.Port)
	uri.Fragment = l.Ps
	uri.User = url.UserPassword(url.QueryEscape(l.Method), url.QueryEscape(l.Password))
	query := uri.Query()
	if l.Plugin != "" {
		query.Set("plugin", strings.Join([]string{l.Plugin, l.PluginOpts}, ";"))
	}
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
