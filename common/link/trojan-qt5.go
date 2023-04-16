package link

import (
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
			link := &TrojanQt5{}
			return link, link.Parse(u)
		},
	}))
}

// TrojanQt5 represents a parsed Trojan-Qt5 link
type TrojanQt5 struct {
	Password       string
	Address        string
	Port           uint16
	Remarks        string
	AllownInsecure bool
	TFO            bool
}

// Options implements Link
func (l *TrojanQt5) Options() *option.Outbound {
	return &option.Outbound{
		Type: C.TypeTrojan,
		Tag:  l.Remarks,
		TrojanOptions: option.TrojanOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     l.Address,
				ServerPort: l.Port,
			},
			Password: l.Password,
			TLS: &option.OutboundTLSOptions{
				Enabled:    true,
				ServerName: l.Address,
				Insecure:   l.AllownInsecure,
			},
			DialerOptions: option.DialerOptions{
				TCPFastOpen: l.TFO,
			},
		},
	}
}

// Parse implements Link
//
// trojan://password@domain:port?allowinsecure=value&tfo=value#remarks
func (l *TrojanQt5) Parse(u *url.URL) error {
	if u.Scheme != "trojan" {
		return E.New("not a trojan-qt5 link")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return E.Cause(err, "invalid port")
	}
	l.Address = u.Hostname()
	l.Port = uint16(port)
	l.Remarks = u.Fragment
	if uname := u.User.Username(); uname != "" {
		l.Password = uname
	}
	queries := u.Query()
	for key, values := range queries {
		switch strings.ToLower(key) {
		case "allowinsecure":
			switch values[0] {
			case "0":
				l.AllownInsecure = false
			default:
				l.AllownInsecure = true
			}
		case "tfo":
			switch values[0] {
			case "0":
				l.TFO = false
			default:
				l.TFO = true
			}
		}
	}
	return nil
}
