package link

import (
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
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
	opt := &option.VMessOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     v.Server,
			ServerPort: v.ServerPort,
		},
		UUID:     v.UUID,
		AlterId:  v.AlterID,
		Security: v.Security,
	}

	if v.TLS {
		opt.TLS = &option.OutboundTLSOptions{
			Enabled:    true,
			Insecure:   v.TLSAllowInsecure,
			ServerName: v.SNI,
			ALPN:       v.ALPN,
		}
		if len(v.ALPN) > 0 {
			opt.TLS.UTLS = &option.OutboundUTLSOptions{
				Enabled:     true,
				Fingerprint: v.Fingerprint,
			}
		}
	}

	topt := &option.V2RayTransportOptions{
		Type: v.Transport,
	}

	switch v.Transport {
	case "":
		topt = nil
	case C.V2RayTransportTypeHTTP:
		topt.HTTPOptions.Path = v.TransportPath
		if v.TransportHost != "" {
			topt.HTTPOptions.Host = []string{v.TransportHost}
			topt.HTTPOptions.Headers["Host"] = []string{v.TransportHost}
		}
	case C.V2RayTransportTypeWebsocket:
		topt.WebsocketOptions.Path = v.TransportPath
		topt.WebsocketOptions.Headers = map[string]badoption.Listable[string]{
			"Host": {v.TransportHost},
		}
	case C.V2RayTransportTypeQUIC:
		// do nothing
	case C.V2RayTransportTypeGRPC:
		topt.GRPCOptions.ServiceName = v.TransportHost
	}

	opt.Transport = topt
	return &option.Outbound{
		Type:    C.TypeVMess,
		Tag:     v.Tag,
		Options: opt,
	}, nil
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
