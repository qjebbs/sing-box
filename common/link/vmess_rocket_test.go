package link_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
)

func TestVMessRocket(t *testing.T) {
	t.Parallel()
	testCases := []link.VMessRocket{{
		Vmess: link.Vmess{
			Tag:              "remarks",
			Server:           "192.168.100.1",
			ServerPort:       443,
			UUID:             "uuid",
			AlterID:          0,
			Security:         "auto",
			TransportHost:    "host",
			Transport:        C.V2RayTransportTypeWebsocket,
			TransportPath:    "/path",
			TLS:              true,
			TLSAllowInsecure: false,
		},
	}}
	for _, tc := range testCases {
		uri, err := tc.URL()
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(uri)
		if err != nil {
			t.Fatal(err)
		}
		link, err := link.ParseVMessRocket(u)
		if err != nil {
			t.Error(err)
			return
		}
		if !reflect.DeepEqual(link.Vmess, tc.Vmess) {
			t.Errorf("want %#v, got %#v", tc, link.Vmess)
		}
	}
}
