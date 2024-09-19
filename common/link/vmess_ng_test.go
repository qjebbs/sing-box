package link_test

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
)

func TestVMessV2RayNG(t *testing.T) {
	t.Parallel()
	testCases := []link.VMessV2RayNG{
		{
			Vmess: link.Vmess{
				Tag:        "ps",
				Server:     "192.168.1.1",
				ServerPort: 443,
				UUID:       "uuid",
				AlterID:    4,
				Security:   "",

				Transport:     C.V2RayTransportTypeWebsocket,
				TransportHost: "host",
				TransportPath: "/path",

				TLS:         true,
				SNI:         "sni",
				ALPN:        []string{"h2", "http/1.1"},
				Fingerprint: "chrome",
			},
		},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprint("#", i), func(t *testing.T) {
			t.Parallel()
			uri, err := tc.URL()
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(uri)
			if err != nil {
				t.Fatal(err)
			}
			link, err := link.ParseVMessV2RayNG(u)
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(link.Vmess, tc.Vmess) {
				t.Errorf("want %#v, got %#v", tc, link)
			}
		})
	}
}
