package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestHysteria(t *testing.T) {
	runTests(t, link.ParseHysteria, TestCases[*link.Hysteria]{
		{
			Link: "hysteria://host:443?protocol=udp&auth=123456&peer=sni.domain&insecure=1&upmbps=100&downmbps=100&alpn=hysteria&obfs=xplus&obfsParam=123456#remarks",
			Want: &link.Hysteria{
				Host:      "host",
				Port:      443,
				Protocol:  "udp",
				Auth:      "123456",
				Peer:      "sni.domain",
				Insecure:  true,
				UpMpbs:    100,
				DownMpbs:  100,
				ALPN:      "hysteria",
				Obfs:      "xplus",
				ObfsParam: "123456",
				Remarks:   "remarks",
			},
		},
	})
}
