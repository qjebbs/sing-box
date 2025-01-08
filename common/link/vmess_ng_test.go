package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
)

func TestVMessV2RayNG(t *testing.T) {
	runTests(t, link.ParseVMessV2RayNG, TestCases[*link.VMessV2RayNG]{
		{
			Link: "vmess://eyJ2IjoyLCJwcyI6InBzIiwiYWRkIjoiMTkyLjE2OC4xLjEiLCJwb3J0Ijo0NDMsImlkIjoidXVpZCIsImFpZCI6NCwibmV0Ijoid3MiLCJob3N0IjoiaG9zdCIsInBhdGgiOiIvcGF0aCIsInRscyI6InRscyIsInNuaSI6InNuaSIsImFscG4iOiJoMixodHRwLzEuMSIsImZwIjoiY2hyb21lIn0=",
			Want: &link.VMessV2RayNG{
				Vmess: link.Vmess{
					Tag:      "ps",
					Server:   "192.168.1.1",
					Port:     443,
					UUID:     "uuid",
					AlterID:  4,
					Security: "",

					Transport: C.V2RayTransportTypeWebsocket,
					Host:      "host",
					Path:      "/path",

					TLS:         true,
					SNI:         "sni",
					ALPN:        []string{"h2", "http/1.1"},
					Fingerprint: "chrome",
				},
			},
		},
	})
}
