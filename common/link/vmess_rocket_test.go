package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
)

func TestVMessRocket(t *testing.T) {
	runTests(t, link.ParseVMessRocket, TestCases[*link.VMessRocket]{
		{
			Link: "vmess://YXV0bzp1dWlkQDE5Mi4xNjguMTAwLjE6NDQz?tls=tls&obfs=ws&obfsParam=host&path=%2Fpath&remarks=%E5%90%8D%E7%A7%B0",
			Want: &link.VMessRocket{
				Vmess: link.Vmess{
					Tag:           "名称",
					Server:        "192.168.100.1",
					Port:          443,
					UUID:          "uuid",
					AlterID:       0,
					Security:      "auto",
					Host:          "host",
					Transport:     C.V2RayTransportTypeWebsocket,
					Path:          "/path",
					TLS:           true,
					AllowInsecure: false,
				},
			},
		},
	})
}
