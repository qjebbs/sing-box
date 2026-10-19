package link_test

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/link"
	"github.com/sagernet/sing/common/json/badoption"
)

func TestTUIC(t *testing.T) {
	runTests(t, link.ParseTUIC, TestCases[*link.TUIC]{
		{
			Link: "tuic://uuid:password@example.com:443?congestion_control=bbr&udp_relay_mode=native&zero_rtt_handshake=1&heartbeat_interval=10s&sni=real.example.com&insecure=1&alpn=h3&fp=chrome&tfo=1#remarks",
			Want: &link.TUIC{
				UUID:              "uuid",
				Password:          "password",
				Host:              "example.com",
				Port:              443,
				CongestionControl: "bbr",
				UDPRelayMode:      "native",
				ZeroRTTHandshake:  true,
				Heartbeat:         badoption.Duration(10 * time.Second),
				SNI:               "real.example.com",
				Insecure:          true,
				ALPN:              []string{"h3"},
				Fingerprint:       "chrome",
				TFO:               true,
				Remarks:           "remarks",
			},
		},
		{
			Link: "tuic://uuid:password@example.com:443?udp_relay_mode=quic&udp_over_stream=1#remarks",
			Want: &link.TUIC{
				UUID:          "uuid",
				Password:      "password",
				Host:          "example.com",
				Port:          443,
				UDPOverStream: true,
				Remarks:       "remarks",
			},
		},
	})
}
