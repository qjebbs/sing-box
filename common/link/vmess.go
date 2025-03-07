package link

import (
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

// Vmess is the base struct of vmess link
type Vmess struct {
	Tag      string `json:"tag,omitempty"`
	Server   string `json:"server,omitempty"`
	Port     uint16 `json:"port,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	AlterID  int    `json:"alterId,omitempty"`
	Security string `json:"security,omitempty"`

	Transport string `json:"transport,omitempty"`
	Host      string `json:"host,omitempty"`
	Path      string `json:"path,omitempty"`

	TLS           bool     `json:"tls,omitempty"`
	SNI           string   `json:"sni,omitempty"`
	ALPN          []string `json:"alpn,omitempty"`
	AllowInsecure bool     `json:"allowInsecure,omitempty"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
}

// Outbound implements Link
func (v *Vmess) Outbound() (*option.Outbound, error) {
	opt := &option.VMessOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     v.Server,
			ServerPort: v.Port,
		},
		UUID:     v.UUID,
		AlterId:  v.AlterID,
		Security: v.Security,
	}

	if v.TLS {
		opt.TLS = &option.OutboundTLSOptions{
			Enabled:    true,
			Insecure:   v.AllowInsecure,
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
		topt.HTTPOptions.Path = v.Path
		if v.Host != "" {
			topt.HTTPOptions.Host = []string{v.Host}
			topt.HTTPOptions.Headers = badoption.HTTPHeader{
				"Host": {v.Host},
			}
		}
	case C.V2RayTransportTypeWebsocket:
		topt.WebsocketOptions.Path = v.Path
		if v.Host != "" {
			topt.WebsocketOptions.Headers = badoption.HTTPHeader{
				"Host": {v.Host},
			}
		}
	case C.V2RayTransportTypeQUIC:
		// do nothing
	case C.V2RayTransportTypeGRPC:
		topt.GRPCOptions.ServiceName = v.Host
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
