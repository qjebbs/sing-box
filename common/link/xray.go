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
	"github.com/sagernet/sing/common/json/badoption"
)

var _ Link = (*Xray)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "Xray",
		Scheme: []string{"vless", "vmess"},
		Parse: func(u *url.URL) (Link, error) {
			lnk, err := ParseXray(u)
			if err != nil {
				return nil, err
			}
			return lnk, lnk.compatiblily()
		},
	}))
}

// Xray is the base struct of xray VMessAEAD / VLESS link
// https://github.com/XTLS/Xray-core/discussions/716
type Xray struct {
	Scheme string `json:"schema"` // vmess, vless
	Server string `json:"server"`
	Port   uint16 `json:"port"`
	UUID   string `json:"uuid"`
	Tag    string `json:"tag,omitempty"`

	// query fields

	// protocol

	Encryption string `json:"encryption,omitempty"` // for vmess: auto, none, aes-128-gcm, chacha20-poly1305

	// Transport

	TransportType string `json:"type,omitempty"`        // tcp, kcp, ws, http, grpc, httpupgrade, xhttp
	HeaderType    string `json:"headerType,omitempty"`  // for mkcp: none, srtp, utp, wechat-video, dtls, wireguard
	Seed          string `json:"seed,omitempty"`        // for mkcp
	Host          string `json:"host,omitempty"`        // for http, ws, httpupgrade, xhttp
	Path          string `json:"path,omitempty"`        // for http, ws, httpupgrade, xhttp
	Mode          string `json:"mode,omitempty"`        // for grpc, xhttp
	ServiceName   string `json:"serviceName,omitempty"` // for grpc
	Authority     string `json:"authority,omitempty"`   // for grpc
	Extra         string `json:"extra,omitempty"`       // for xhttp

	// TLS

	Security      string   `json:"security,omitempty"`      // TLS security: none, tls, reality
	Fingerprint   string   `json:"fp,omitempty"`            // for utls, reality
	SNI           string   `json:"sni,omitempty"`           // for tls
	ALPN          []string `json:"alpn,omitempty"`          // for tls
	AllowInsecure bool     `json:"allowInsecure,omitempty"` // for tls
	Flow          string   `json:"flow,omitempty"`          // for xtls: "", xtls-rprx-vision, xtls-rprx-vision-udp443
	PubKey        string   `json:"pbk,omitempty"`           // for reality
	ShortID       string   `json:"sid,omitempty"`           // for reality
	SipderX       string   `json:"spx,omitempty"`           // for reality
}

// ParseXray parses a vless link
func ParseXray(u *url.URL) (*Xray, error) {
	link := &Xray{}
	if err := link.parse(u); err != nil {
		return nil, err
	}
	return link, link.check()
}

// Outbound implements Link
func (v *Xray) Outbound() (*option.Outbound, error) {
	if err := v.compatiblily(); err != nil {
		return nil, err
	}
	switch v.Scheme {
	case "vmess":
		return v.outboundVmess()
	case "vless":
		return v.outboundVless()
	}
	return nil, E.New("unknown type: ", v.Scheme)
}

func (v *Xray) outboundVmess() (*option.Outbound, error) {
	opt := &option.VMessOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     v.Server,
			ServerPort: v.Port,
		},
		UUID:     v.UUID,
		AlterId:  0,
		Security: v.Encryption,
	}

	opt.TLS = v.tlsOption()
	opt.Transport = v.transportOption()
	return &option.Outbound{
		Type:    C.TypeVMess,
		Tag:     v.Tag,
		Options: opt,
	}, nil
}

func (v *Xray) outboundVless() (*option.Outbound, error) {
	opt := &option.VLESSOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     v.Server,
			ServerPort: v.Port,
		},
		UUID: v.UUID,
		Flow: v.Flow,
	}
	opt.TLS = v.tlsOption()
	opt.Transport = v.transportOption()
	return &option.Outbound{
		Type:    C.TypeVLESS,
		Tag:     v.Tag,
		Options: opt,
	}, nil

}

func (v *Xray) tlsOption() *option.OutboundTLSOptions {
	if v.Security != "tls" && v.Security != "reality" {
		return nil
	}
	tls := &option.OutboundTLSOptions{
		Enabled:    true,
		Insecure:   v.AllowInsecure,
		ServerName: v.SNI,
		ALPN:       v.ALPN,
	}
	if v.Security == "reality" {
		tls.Reality = &option.OutboundRealityOptions{
			Enabled:   true,
			PublicKey: v.PubKey,
			ShortID:   v.ShortID,
		}
	}
	if len(v.ALPN) > 0 {
		tls.UTLS = &option.OutboundUTLSOptions{
			Enabled:     true,
			Fingerprint: v.Fingerprint,
		}
	}
	return tls
}

func (v *Xray) transportOption() *option.V2RayTransportOptions {
	topt := &option.V2RayTransportOptions{}
	switch v.TransportType {
	case "", "tcp":
		return nil
	case "http":
		topt.Type = C.V2RayTransportTypeHTTP
		topt.HTTPOptions.Path = v.Path
		if v.Host != "" {
			topt.HTTPOptions.Host = []string{v.Host}
			topt.HTTPOptions.Headers["Host"] = []string{v.Host}
		}
	case "ws":
		topt.Type = C.V2RayTransportTypeWebsocket
		topt.WebsocketOptions.Path = v.Path
		topt.WebsocketOptions.Headers = map[string]badoption.Listable[string]{
			"Host": {v.Host},
		}
	case "quic":
		topt.Type = C.V2RayTransportTypeQUIC
		// do nothing
	case C.V2RayTransportTypeGRPC:
		topt.Type = C.V2RayTransportTypeGRPC
		topt.GRPCOptions = option.V2RayGRPCOptions{
			ServiceName: v.ServiceName,
		}
	case C.V2RayTransportTypeHTTPUpgrade:
		topt.Type = C.V2RayTransportTypeHTTPUpgrade
		topt.HTTPUpgradeOptions = option.V2RayHTTPUpgradeOptions{
			Host: v.Host,
			Path: v.Path,
		}
	}
	return topt
}

// URL implements Link
func (v *Xray) URL() (string, error) {
	if err := v.check(); err != nil {
		return "", err
	}
	var uri url.URL
	uri.Scheme = v.Scheme
	uri.Host = fmt.Sprintf("%s:%d", v.Server, v.Port)
	uri.Fragment = v.Tag
	uri.User = url.User(v.UUID)
	query := uri.Query()
	if v.TransportType != "" {
		query.Set("type", v.TransportType)
	}
	if v.Encryption != "" {
		query.Set("encryption", v.Encryption)
	}
	if v.Security != "" {
		query.Set("security", v.Security)
	}
	if v.Host != "" {
		query.Set("host", v.Host)
	}
	if v.Path != "" {
		query.Set("path", v.Path)
	}
	if v.HeaderType != "" {
		query.Set("headerType", v.HeaderType)
	}
	if v.Seed != "" {
		query.Set("seed", v.Seed)
	}
	if v.ServiceName != "" {
		query.Set("serviceName", v.ServiceName)
	}
	if v.Mode != "" {
		query.Set("mode", v.Mode)
	}
	if v.Authority != "" {
		query.Set("authority", v.Authority)
	}
	if v.Extra != "" {
		query.Set("extra", v.Extra)
	}
	if v.Fingerprint != "" {
		query.Set("fp", v.Fingerprint)
	}
	if v.SNI != "" {
		query.Set("sni", v.SNI)
	}
	if len(v.ALPN) > 0 {
		query.Set("alpn", strings.Join(v.ALPN, ","))
	}
	if v.AllowInsecure {
		query.Set("allowInsecure", "1")
	}
	if v.Flow != "" {
		query.Set("flow", v.Flow)
	}
	if v.PubKey != "" {
		query.Set("pbk", v.PubKey)
	}
	if v.ShortID != "" {
		query.Set("sid", v.ShortID)
	}
	if v.SipderX != "" {
		query.Set("spx", v.SipderX)
	}
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}

func (v *Xray) parse(u *url.URL) error {
	if u.Scheme != "vless" && u.Scheme != "vmess" {
		return E.New("not a xray link")
	}
	// reset all fields
	*v = Xray{
		Scheme: u.Scheme,
		Server: u.Hostname(),
		Tag:    u.Fragment,
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return E.Cause(err, "invalid port")
	}
	v.Port = uint16(port)
	if uname := u.User.Username(); uname != "" {
		v.UUID = uname
	}
	for key, values := range u.Query() {
		value := values[0]
		switch key {
		case "type":
			v.TransportType = value
		case "encryption":
			v.Encryption = value
		case "security":
			v.Security = value
		case "path":
			v.Path = value
		case "host":
			v.Host = value
		case "headerType":
			v.HeaderType = value
		case "seed":
			v.Seed = value
		case "serviceName":
			v.ServiceName = value
		case "mode":
			v.Mode = value
		case "authority":
			v.Authority = value
		case "extra":
			v.Extra = value
		case "fp":
			v.Fingerprint = value
		case "sni":
			v.SNI = value
		case "alpn":
			for _, item := range strings.Split(value, ",") {
				v.ALPN = append(v.ALPN, strings.TrimSpace(item))
			}
		case "allowInsecure":
			switch value {
			case "true", "1":
				v.AllowInsecure = true
			default:
				v.AllowInsecure = false
			}
		case "flow":
			v.Flow = value
		case "pbk":
			v.PubKey = value
		case "sid":
			v.ShortID = value
		case "spx":
			v.SipderX = value
		}
	}
	return nil
}

func (v *Xray) check() error {
	switch v.Scheme {
	case "vmess", "vless":
	default:
		return E.New("unknown type: ", v.Scheme)
	}
	if v.Server == "" {
		return E.New("missing server")
	}
	if v.Port == 0 {
		return E.New("missing port")
	}
	if v.UUID == "" {
		return E.New("missing UUID")
	}
	switch v.TransportType {
	case "", "tcp", "kcp", "ws", "http", "grpc", "httpupgrade", "xhttp":
	default:
		return E.New("unknown transport: ", v.TransportType)
	}
	if v.Scheme == "vmess" {
		switch v.Encryption {
		case "", "none", "auto", "aes-128-gcm", "chacha20-poly1305":
		default:
			return E.New("unknown vmess security: ", v.Encryption)
		}
	} else {
		switch v.Encryption {
		case "", "none":
		default:
			return E.New("unknown vless security: ", v.Encryption)
		}
	}
	switch v.Security {
	case "", "none", "tls", "reality":
	default:
		return E.New("unknown tls: ", v.Security)
	}
	switch v.TransportType {
	case "http", "ws", "httpupgrade", "xhttp":
	default:
		if v.Path != "" {
			return E.New("path is not supported in ", v.TransportType)
		}
		if v.Host != "" {
			return E.New("host is not supported in ", v.TransportType)
		}
	}
	if v.TransportType != "kcp" {
		if v.HeaderType != "" {
			return E.New("headerType is not supported in ", v.TransportType)
		}
		if v.Seed != "" {
			return E.New("seed is not supported in ", v.TransportType)
		}
	}
	switch v.HeaderType {
	case "", "none", "srtp", "utp", "wechat-video", "dtls", "wireguard":
	default:
		return E.New("unknown headerType: ", v.HeaderType)
	}
	if v.TransportType != "grpc" {
		if v.ServiceName != "" {
			return E.New("serviceName is not supported in ", v.TransportType)
		}
		if v.Authority != "" {
			return E.New("authority is not supported in ", v.TransportType)
		}
	}
	if v.Mode != "" {
		switch v.TransportType {
		case "grpc":
			switch v.Mode {
			case "gun", "multi", "guna":
			default:
				return E.New("unknown grpc mode: ", v.Mode)
			}
		case "xhttp":
			switch v.Mode {
			case "auto", "packet-up", "stream-up":
			default:
				return E.New("unknown xhttp mode: ", v.Mode)
			}
		}
	}
	if v.TransportType != "xhttp" && v.Extra != "" {
		return E.New("extra is not supported in xhttp")
	}
	if v.Security == "reality" {
		if v.Fingerprint == "" {
			return E.New("missing fp for reality")
		}
		if v.PubKey == "" {
			return E.New("missing pbk for reality")
		}

	}
	switch v.Flow {
	case "", "xtls-rprx-vision", "xtls-rprx-vision-udp443":
	default:
		return E.New("unknown flow: ", v.Flow)
	}
	return nil
}

func (v *Xray) compatiblily() error {
	if err := v.check(); err != nil {
		return err
	}
	switch v.TransportType {
	case "", "tcp", "http", "ws", "httpupgrade", "grpc":
	default:
		return E.New("unsupported transport: ", v.TransportType)
	}
	if v.TransportType == "grpc" {
		if v.Mode != "" && v.Mode != "gun" {
			return E.New("unsupported grpc mode: ", v.Mode)
		}
		if v.Authority != "" {
			return E.New("unsupported authority in grpc")
		}
	}
	if v.Flow != "" {
		if v.Scheme == "vmess" {
			return E.New("unsupported flow in vmess")
		}
		if v.Flow != "xtls-rprx-vision" {
			return E.New("unsupported flow: ", v.Flow)
		}
	}
	return nil
}
