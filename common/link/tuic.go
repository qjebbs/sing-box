package link

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
)

var _ Link = (*TUIC)(nil)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "TUIC",
		Scheme: []string{"tuic"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseTUIC(u)
		},
	}))
}

// TUIC represents a parsed TUIC link.
//
// The TUIC protocol specification does not define a URI scheme.
// https://github.com/tuic-protocol/tuic
type TUIC struct {
	UUID              string             `json:"uuid,omitempty"`
	Password          string             `json:"password,omitempty"`
	Host              string             `json:"host,omitempty"`
	Port              uint16             `json:"port,omitempty"`
	CongestionControl string             `json:"congestion_control,omitempty"`
	UDPRelayMode      string             `json:"udp_relay_mode,omitempty"`
	UDPOverStream     bool               `json:"udp_over_stream,omitempty"`
	ZeroRTTHandshake  bool               `json:"zero_rtt_handshake,omitempty"`
	Heartbeat         badoption.Duration `json:"heartbeat,omitempty"`
	SNI               string             `json:"sni,omitempty"`
	Insecure          bool               `json:"insecure,omitempty"`
	DisableSNI        bool               `json:"disable_sni,omitempty"`
	ALPN              []string           `json:"alpn,omitempty"`
	Fingerprint       string             `json:"fingerprint,omitempty"`
	TFO               bool               `json:"tfo,omitempty"`
	Remarks           string             `json:"remarks,omitempty"`
}

// ParseTUIC parses a TUIC link.
func ParseTUIC(u *url.URL) (*TUIC, error) {
	if u.Scheme != "tuic" {
		return nil, E.New("not a tuic link")
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, E.New("missing uuid")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return nil, E.Cause(err, "invalid port")
	}
	password, _ := u.User.Password()
	link := &TUIC{
		UUID:     u.User.Username(),
		Password: password,
		Host:     u.Hostname(),
		Port:     uint16(port),
		Remarks:  u.Fragment,
	}
	for key, values := range u.Query() {
		value := values[0]
		switch key {
		case "congestion_control":
			if value != "cubic" {
				link.CongestionControl = value
			}
		case "udp_relay_mode":
			link.UDPRelayMode = value
		case "udp_over_stream":
			link.UDPOverStream = isTrue(value)
		case "zero_rtt_handshake", "reduce_rtt":
			link.ZeroRTTHandshake = isTrue(value)
		case "heartbeat_interval":
			heartbeat, err := parseDuration(value)
			if err != nil {
				return nil, E.Cause(err, "invalid heartbeat_interval: ", value)
			}
			link.Heartbeat = heartbeat
		case "sni":
			link.SNI = value
		case "insecure", "skip-cert-verify", "allow_insecure":
			link.Insecure = isTrue(value)
		case "disable_sni":
			link.DisableSNI = isTrue(value)
		case "alpn":
			link.ALPN = splitComma(value)
		case "fp":
			link.Fingerprint = value
		case "tfo", "tcp-fast-open", "tcp_fast_open":
			link.TFO = isTrue(value)
		}
	}
	if link.UDPOverStream {
		link.UDPRelayMode = ""
	}
	return link, nil
}

// Outbound implements Link.
func (l *TUIC) Outbound(opt option.ProviderOutboundsOptions) (*option.Outbound, error) {
	dialerOptions := opt.DialerOptions
	if l.TFO {
		dialerOptions.AbstractDialerOptions.TCPFastOpen = true
	}
	serverName := l.SNI
	if serverName == "" {
		serverName = l.Host
	}
	tlsOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: serverName,
		Insecure:   l.Insecure,
		DisableSNI: l.DisableSNI,
		ALPN:       l.ALPN,
	}
	if l.Fingerprint != "" {
		tlsOptions.UTLS = &option.OutboundUTLSOptions{
			Enabled:     true,
			Fingerprint: l.Fingerprint,
		}
	}
	o := &option.TUICOutboundOptions{
		DialerOptions: dialerOptions,
		ServerOptions: option.ServerOptions{
			Server:     l.Host,
			ServerPort: l.Port,
		},
		UUID:              l.UUID,
		Password:          l.Password,
		CongestionControl: l.CongestionControl,
		UDPRelayMode:      l.UDPRelayMode,
		UDPOverStream:     l.UDPOverStream,
		ZeroRTTHandshake:  l.ZeroRTTHandshake,
		Heartbeat:         l.Heartbeat,
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
	}
	return &option.Outbound{
		Type:    C.TypeTUIC,
		Tag:     l.Remarks,
		Options: o,
	}, nil
}

// URL implements Link.
func (l *TUIC) URL() (string, error) {
	var uri url.URL
	uri.Scheme = "tuic"
	uri.Host = fmt.Sprintf("%s:%d", l.Host, l.Port)
	uri.Fragment = l.Remarks
	if l.Password != "" {
		uri.User = url.UserPassword(l.UUID, l.Password)
	} else {
		uri.User = url.User(l.UUID)
	}
	query := uri.Query()
	if l.CongestionControl != "" {
		query.Set("congestion_control", l.CongestionControl)
	}
	if l.UDPRelayMode != "" {
		query.Set("udp_relay_mode", l.UDPRelayMode)
	}
	if l.UDPOverStream {
		query.Set("udp_over_stream", "1")
	}
	if l.ZeroRTTHandshake {
		query.Set("zero_rtt_handshake", "1")
	}
	if l.Heartbeat != 0 {
		query.Set("heartbeat_interval", time.Duration(l.Heartbeat).String())
	}
	if l.SNI != "" {
		query.Set("sni", l.SNI)
	}
	if l.Insecure {
		query.Set("insecure", "1")
	}
	if l.DisableSNI {
		query.Set("disable_sni", "1")
	}
	if len(l.ALPN) > 0 {
		query.Set("alpn", strings.Join(l.ALPN, ","))
	}
	if l.Fingerprint != "" {
		query.Set("fp", l.Fingerprint)
	}
	if l.TFO {
		query.Set("tfo", "1")
	}
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
