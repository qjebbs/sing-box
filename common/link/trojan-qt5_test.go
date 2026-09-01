package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestTrojanQt5(t *testing.T) {
	runTests(t, link.ParseTrojanQt5, TestCases[*link.TrojanQt5]{
		{
			Link: "trojan://0de72799-270c-4ca7-8d12-73b8959dbf31@sdv3-hk.kunlun04dns.com:6602?allowInsecure=1&peer=openssl.nodesni.com&tfo=1#remarks",
			Want: &link.TrojanQt5{
				Remarks:       "remarks",
				Server:        "sdv3-hk.kunlun04dns.com",
				Port:          6602,
				Password:      "0de72799-270c-4ca7-8d12-73b8959dbf31",
				SNI:           "openssl.nodesni.com",
				AllowInsecure: true,
				TFO:           true,
			},
		},
		{
			Link: "trojan://password-%E5%AF%86%E7%A0%81@example.com:443?allowInsecure=1&sni=example.org&tfo=1#remarks",
			Want: &link.TrojanQt5{
				Remarks:       "remarks",
				Server:        "example.com",
				Port:          443,
				Password:      "password-密码",
				AllowInsecure: true,
				SNI:           "example.org",
				TFO:           true,
			},
		},
	})
}
