package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestHysteria2(t *testing.T) {
	runTests(t, link.ParseHysteria2, TestCases[*link.Hysteria2]{
		{
			Link: "hysteria2://letmein@example.com/?insecure=1&obfs=salamander&obfs-password=gawrgura&pinSHA256=&sni=real.example.com#remarks",
			Want: &link.Hysteria2{
				Auth:         "letmein",
				Host:         "example.com",
				Port:         443,
				Obfs:         "salamander",
				ObfsPassword: "gawrgura",
				SNI:          "real.example.com",
				Insecure:     true,
				PingSHA256:   "",
				Remarks:      "remarks",
			},
		},
		{
			Link: "hy2://letmein@example.com/?insecure=1&obfs=salamander&obfs-password=gawrgura&pinSHA256=&sni=real.example.com#remarks",
			Want: &link.Hysteria2{
				Auth:         "letmein",
				Host:         "example.com",
				Port:         443,
				Obfs:         "salamander",
				ObfsPassword: "gawrgura",
				SNI:          "real.example.com",
				Insecure:     true,
				PingSHA256:   "",
				Remarks:      "remarks",
			},
		},
		{
			Link: "hysteria2://letmein:password@example.com/?insecure=1&obfs=salamander&obfs-password=gawrgura&pinSHA256=&sni=real.example.com#remarks",
			Want: &link.Hysteria2{
				User:         "letmein",
				Auth:         "password",
				Host:         "example.com",
				Port:         443,
				Obfs:         "salamander",
				ObfsPassword: "gawrgura",
				SNI:          "real.example.com",
				Insecure:     true,
				PingSHA256:   "",
				Remarks:      "remarks",
			},
		},
	})
}
