package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestTrojanQt5(t *testing.T) {
	runTests(t, link.ParseTrojanQt5, TestCases[*link.TrojanQt5]{
		{
			Link: "trojan://password-%E5%AF%86%E7%A0%81@192.168.1.1:443?allowInsecure=1&tfo=1#remarks",
			Want: &link.TrojanQt5{
				Remarks:       "remarks",
				Server:        "192.168.1.1",
				Port:          443,
				Password:      "password-密码",
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
