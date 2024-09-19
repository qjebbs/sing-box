package link

import (
	"fmt"
	"net/url"
	"strconv"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ Link = (*VMessRocket)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "ShadowRocket",
		Scheme: []string{"vmess"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseVMessRocket(u)
		},
	}))
}

// VMessRocket is the vmess link of ShadowRocket
type VMessRocket struct {
	Vmess
}

// ParseVMessRocket parses a ShadowRocket vmess link
func ParseVMessRocket(u *url.URL) (*VMessRocket, error) {
	link := &VMessRocket{}
	if u.Scheme != "vmess" {
		return nil, E.New("not a vmess link")
	}

	b, err := base64Decode(u.Host)
	if err != nil {
		return nil, ErrBadFormat
	}
	// auto:uuid@192.168.100.1:443
	hostURL, err := url.Parse("vmess://" + string(b))
	if err != nil {
		return nil, ErrBadFormat
	}
	sec := hostURL.User.Username()
	if sec == "" {
		sec = "auto"
	}
	link.Security = sec
	uuid, ok := hostURL.User.Password()
	if ok {
		link.UUID = uuid
	}
	link.Server = hostURL.Hostname()
	port, err := strconv.ParseUint(hostURL.Port(), 10, 16)
	if err != nil {
		return nil, E.Cause(err, "invalid port ", hostURL.Port())
	}
	link.ServerPort = uint16(port)
	link.AlterID = 0

	for key, values := range u.Query() {
		switch key {
		case "remarks":
			link.Tag = firstValueOf(values)
		case "path":
			link.TransportPath = firstValueOf(values)
		case "tls":
			link.TLS = firstValueOf(values) == "tls"
		case "obfs":
			v := firstValueOf(values)
			switch v {
			case "ws", "websocket":
				link.Transport = C.V2RayTransportTypeWebsocket
			case "http":
				link.Transport = C.V2RayTransportTypeHTTP
			}
		case "obfsParam":
			link.TransportHost = firstValueOf(values)
		}
	}
	return link, nil
}

// URL implements Link
func (v *VMessRocket) URL() (string, error) {
	security := v.Security
	if security == "" {
		security = "auto"
	}
	host := fmt.Sprintf("%s:%s@%s:%d", security, v.UUID, v.Server, v.ServerPort)
	host = base64Encode([]byte(host))
	var uri url.URL
	uri.Scheme = "vmess"
	uri.Host = host
	query := uri.Query()
	if v.Tag != "" {
		query.Set("remarks", v.Tag)
	}
	if v.TransportPath != "" {
		query.Set("path", v.TransportPath)
	}
	if v.TLS {
		query.Set("tls", "tls")
	}
	switch v.Transport {
	case C.V2RayTransportTypeWebsocket:
		query.Set("obfs", "ws")
	case C.V2RayTransportTypeHTTP:
		query.Set("obfs", "http")
	}
	if v.TransportHost != "" {
		query.Set("obfsParam", v.TransportHost)
	}
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}

func firstValueOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
