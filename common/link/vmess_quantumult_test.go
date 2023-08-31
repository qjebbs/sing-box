package link_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
)

func TestVMessQuantumult(t *testing.T) {
	t.Parallel()
	testCase := []struct {
		link string
		want link.Vmess
	}{
		{
			link: "vmess://cHMgPSB2bWVzcywxOTIuMTY4LjEwMC4xLDQ0MyxhZXMtMTI4LWdjbSwidXVpZCIsb3Zlci10bHM9dHJ1ZSxjZXJ0aWZpY2F0ZT0wLG9iZnM9d3Msb2Jmcy1wYXRoPSIvcGF0aCIsb2Jmcy1oZWFkZXI9Ikhvc3Q6aG9zdFtScl1bTm5dd2hhdGV2ZXI=",
			want: link.Vmess{
				Tag:              "ps",
				Server:           "192.168.100.1",
				ServerPort:       443,
				UUID:             "uuid",
				AlterID:          0,
				Security:         "aes-128-gcm",
				TransportHost:    "host",
				Transport:        C.V2RayTransportTypeWebsocket,
				TransportPath:    "/path",
				TLS:              true,
				TLSAllowInsecure: true,
			},
		},
	}
	for _, tc := range testCase {
		u, err := url.Parse(tc.link)
		if err != nil {
			t.Fatal(err)
		}
		link, err := link.ParseVMessQuantumult(u)
		if err != nil {
			t.Error(err)
			return
		}
		if !reflect.DeepEqual(link.Vmess, tc.want) {
			t.Errorf("want %#v, got %#v", tc.want, link.Vmess)
		}
	}
}
