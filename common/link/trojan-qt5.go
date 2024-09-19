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

var _ Link = (*TrojanQt5)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "Trojan-Qt5",
		Scheme: []string{"trojan"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseTrojanQt5(u)
		},
	}))
}

// TrojanQt5 represents a parsed Trojan-Qt5 link
type TrojanQt5 struct {
	Remarks       string
	Host          string
	Port          uint16
	Password      string
	AllowInsecure bool
	SNI           string
	TFO           bool
}

// Outbound implements Link
func (l *TrojanQt5) Outbound() (*option.Outbound, error) {
	sni := l.SNI
	if sni == "" {
		sni = l.Host
	}
	return &option.Outbound{
		Type: C.TypeTrojan,
		Tag:  l.Remarks,
		TrojanOptions: option.TrojanOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     l.Host,
				ServerPort: l.Port,
			},
			Password: l.Password,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: &option.OutboundTLSOptions{
					Enabled:    true,
					ServerName: sni,
					Insecure:   l.AllowInsecure,
				},
			},
			DialerOptions: option.DialerOptions{
				TCPFastOpen: l.TFO,
			},
		},
	}, nil
}

// ParseTrojanQt5 parses a Trojan-Qt5 link
//
// trojan://password@domain:port?allowinsecure=value&sni=value&tfo=value#remarks
func ParseTrojanQt5(u *url.URL) (*TrojanQt5, error) {
	if u.Scheme != "trojan" {
		return nil, E.New("not a trojan-qt5 link")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return nil, E.Cause(err, "invalid port")
	}
	link := &TrojanQt5{}
	link.Host = u.Hostname()
	link.Port = uint16(port)
	link.Remarks = u.Fragment
	if uname := u.User.Username(); uname != "" {
		password, err := url.QueryUnescape(uname)
		if err != nil {
			return nil, err
		}
		link.Password = password
	}
	queries := u.Query()
	for key, values := range queries {
		switch strings.ToLower(key) {
		case "allowinsecure":
			switch values[0] {
			case "0":
				link.AllowInsecure = false
			default:
				link.AllowInsecure = true
			}
		case "sni":
			link.SNI = values[0]
		case "tfo":
			switch values[0] {
			case "0":
				link.TFO = false
			default:
				link.TFO = true
			}
		}
	}
	return link, nil
}

// URL implements Link
func (l *TrojanQt5) URL() (string, error) {
	var uri url.URL
	uri.Scheme = "trojan"
	uri.Host = fmt.Sprintf("%s:%d", l.Host, l.Port)
	uri.User = url.User(url.QueryEscape(l.Password))
	uri.Fragment = l.Remarks
	query := uri.Query()
	if l.AllowInsecure {
		query.Set("allowInsecure", "1")
	}
	if l.SNI != "" && l.SNI != l.Host {
		query.Set("sni", l.SNI)
	}
	if l.TFO {
		query.Set("tfo", "1")
	}
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
