package link

import (
	"encoding/json"
	"net/url"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "V2RayNG",
		Scheme: []string{"vmess"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseVMessV2RayNG(u)
		},
	}))
}

// VMessV2RayNG is the vmess link of V2RayNG
type VMessV2RayNG struct {
	Vmess
}

// ParseVMessV2RayNG parses vmess link of V2RayNG
func ParseVMessV2RayNG(u *url.URL) (*VMessV2RayNG, error) {
	if u.Scheme != "vmess" {
		return nil, E.New("not a vmess link")
	}

	b64 := u.Host + u.Path
	b, err := base64Decode(b64)
	if err != nil {
		return nil, ErrBadFormat
	}
	ng := vmessNG{}
	if err := json.Unmarshal(b, &ng); err != nil {
		return nil, ErrBadFormat
	}
	if ng.V != 2 {
		return nil, E.New("unsupported version ", ng.V)
	}
	vm, err := ng.AsVMess()
	if err != nil {
		return nil, err
	}
	return &VMessV2RayNG{Vmess: *vm}, nil
}

// URL implements Link
func (l *VMessV2RayNG) URL() (string, error) {
	ng := &vmessNG{}
	err := ng.FromVMess(&l.Vmess)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(ng)
	if err != nil {
		return "", err
	}
	b64 := base64Encode(b)
	return "vmess://" + b64, nil
}

// https://github.com/2dust/v2rayN/wiki/分享链接格式说明(ver-2)
type vmessNG struct {
	V           number `json:"v,omitempty"`
	Ps          string `json:"ps,omitempty"`
	Add         string `json:"add,omitempty"`
	Port        number `json:"port,omitempty"`
	ID          string `json:"id,omitempty"`
	Aid         number `json:"aid,omitempty"`
	Scy         string `json:"scy,omitempty"`
	Net         string `json:"net,omitempty"`
	Type        string `json:"type,omitempty"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	TLS         string `json:"tls,omitempty"`
	SNI         string `json:"sni,omitempty"`
	ALPN        string `json:"alpn,omitempty"`
	Fingerprint string `json:"fp,omitempty"`
}

func (l vmessNG) AsVMess() (*Vmess, error) {
	var (
		transport string
		alpn      []string
	)

	switch l.Type {
	case "none", "":
		// ok
	default:
		return nil, E.New("unsupported type ", l.Type)
	}

	switch l.Net {
	case "ws", "websocket":
		transport = C.V2RayTransportTypeWebsocket
	case "http", "h2":
		transport = C.V2RayTransportTypeHTTP
	case "quci":
		transport = C.V2RayTransportTypeQUIC
	case "grpc":
		transport = C.V2RayTransportTypeGRPC
	default:
		// "kcp", "tcp" ...
		return nil, E.New("unsupported transport ", l.Net)
	}

	if l.ALPN != "" {
		alpn = strings.Split(l.ALPN, ",")
		for i := range alpn {
			alpn[i] = strings.TrimSpace(alpn[i])
		}
	}
	return &Vmess{
		Tag:        l.Ps,
		Server:     l.Add,
		ServerPort: uint16(l.Port),
		UUID:       l.ID,
		AlterID:    int(l.Aid),
		Security:   l.Scy,

		Transport:     transport,
		TransportHost: l.Host,
		TransportPath: l.Path,

		TLS:         l.TLS == "tls",
		SNI:         l.SNI,
		ALPN:        alpn,
		Fingerprint: l.Fingerprint,
	}, nil
}

func (l *vmessNG) FromVMess(v *Vmess) error {
	tls := ""
	if v.TLS {
		tls = "tls"
	}
	net := v.Transport
	switch v.Transport {
	case C.V2RayTransportTypeWebsocket:
		net = "ws"
	case C.V2RayTransportTypeHTTP:
		net = "http"
	case C.V2RayTransportTypeQUIC:
		net = "quic"
	}
	*l = vmessNG{
		V:           number(2),
		Ps:          v.Tag,
		Add:         v.Server,
		Port:        number(v.ServerPort),
		ID:          v.UUID,
		Aid:         number(v.AlterID),
		Scy:         v.Security,
		Net:         net,
		Host:        v.TransportHost,
		Path:        v.TransportPath,
		TLS:         tls,
		SNI:         v.SNI,
		ALPN:        strings.Join(v.ALPN, ","),
		Fingerprint: v.Fingerprint,
	}
	return nil
}
