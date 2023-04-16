package link_test

import (
	"net/url"
	"testing"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
)

func TestVMessRocket(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		link string
		want link.Vmess
	}{
		{
			link: "vmess://YXV0bzp1dWlkQDE5Mi4xNjguMTAwLjE6NDQz/?remarks=remarks&obfs=ws&path=/path&obfsParam=host&tls=tls",
			want: link.Vmess{
				Tag:              "remarks",
				Server:           "192.168.100.1",
				ServerPort:       443,
				UUID:             "uuid",
				AlterID:          0,
				Security:         "auto",
				Host:             "host",
				Transport:        C.V2RayTransportTypeWebsocket,
				TransportPath:    "/path",
				TLS:              true,
				TLSAllowInsecure: false,
			},
		},
	}
	for _, tc := range testCases {
		u, err := url.Parse(tc.link)
		if err != nil {
			t.Fatal(err)
		}
		link := link.VMessRocket{}
		err = link.Parse(u)
		if err != nil {
			t.Error(err)
			return
		}
		if link.Vmess != tc.want {
			t.Errorf("want %#v, got %#v", tc.want, link.Vmess)
		}
	}
}
