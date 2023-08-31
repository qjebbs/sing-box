package link

import (
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// Vmess is the base struct of vmess link
type Vmess struct {
	Tag        string
	Server     string
	ServerPort uint16
	UUID       string
	AlterID    int
	Security   string

	Transport     string
	TransportHost string
	TransportPath string

	TLS              bool
	SNI              string
	ALPN             []string
	TLSAllowInsecure bool
	Fingerprint      string
}

// Outbound implements Link
func (v *Vmess) Outbound() (*option.Outbound, error) {
	out := &option.Outbound{
		Type: C.TypeVMess,
		Tag:  v.Tag,
		VMessOptions: option.VMessOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     v.Server,
				ServerPort: v.ServerPort,
			},
			UUID:     v.UUID,
			AlterId:  v.AlterID,
			Security: v.Security,
		},
	}

	if v.TLS {
		out.VMessOptions.TLS = &option.OutboundTLSOptions{
			Enabled:    true,
			Insecure:   v.TLSAllowInsecure,
			ServerName: v.SNI,
			ALPN:       v.ALPN,
		}
		if len(v.ALPN) > 0 {
			out.VMessOptions.TLS.UTLS = &option.OutboundUTLSOptions{
				Enabled:     true,
				Fingerprint: v.Fingerprint,
			}
		}
	}

	opt := &option.V2RayTransportOptions{
		Type: v.Transport,
	}

	switch v.Transport {
	case "":
		opt = nil
	case C.V2RayTransportTypeHTTP:
		opt.HTTPOptions.Path = v.TransportPath
		if v.TransportHost != "" {
			opt.HTTPOptions.Host = []string{v.TransportHost}
			opt.HTTPOptions.Headers["Host"] = []string{v.TransportHost}
		}
	case C.V2RayTransportTypeWebsocket:
		opt.WebsocketOptions.Path = v.TransportPath
		opt.WebsocketOptions.Headers = map[string]option.Listable[string]{
			"Host": {v.TransportHost},
		}
	case C.V2RayTransportTypeQUIC:
		// do nothing
	case C.V2RayTransportTypeGRPC:
		opt.GRPCOptions.ServiceName = v.TransportHost
	}

	out.VMessOptions.Transport = opt
	return out, nil
}

// URL implements Link
func (v *Vmess) URL() (string, error) {
	return "", ErrNotImplemented
}

// URLV2RayNG returns the shadowrocket url representation of vmess link
func (v *Vmess) URLV2RayNG() (string, error) {
	return (&VMessV2RayNG{Vmess: *v}).URL()
}

// URLShadowRocket returns the shadowrocket url representation of vmess link
func (v *Vmess) URLShadowRocket() (string, error) {
	return (&VMessRocket{Vmess: *v}).URL()
}

// URLQuantumult returns the quantumultx url representation of vmess link
func (v *Vmess) URLQuantumult() (string, error) {
	return (&VMessQuantumult{Vmess: *v}).URL()
}
