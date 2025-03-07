package link_test

import (
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestXray(t *testing.T) {
	runTests(t, link.ParseXray, TestCases[*link.Xray]{
		{
			Link: "vmess://99c80931-f3f1-4f84-bffd-6eed6030f53d@qv2ray.net:31415?encryption=none#VMessTCPNaked",
			Want: &link.Xray{
				Scheme:     "vmess",
				UUID:       "99c80931-f3f1-4f84-bffd-6eed6030f53d",
				Server:     "qv2ray.net",
				Port:       31415,
				Encryption: "none",
				Tag:        "VMessTCPNaked",
			},
		},
		{
			Link: "vmess://c7199cd9-964b-4321-9d33-842b6fcec068@qv2ray.net:64338?encryption=none&security=tls&sni=fastgit.org#VMessTCPTLSSNI",
			Want: &link.Xray{
				Scheme:     "vmess",
				UUID:       "c7199cd9-964b-4321-9d33-842b6fcec068",
				Server:     "qv2ray.net",
				Port:       64338,
				Encryption: "none",
				Security:   "tls",
				SNI:        "fastgit.org",
				Tag:        "VMessTCPTLSSNI",
			},
		},
		{
			Link: "vless://399ce595-894d-4d40-add1-7d87f1a3bd10@qv2ray.net:50288?type=kcp&seed=69f04be3-d64e-45a3-8550-af3172c63055#VLESSmKCPSeed",
			Want: &link.Xray{
				Scheme:        "vless",
				UUID:          "399ce595-894d-4d40-add1-7d87f1a3bd10",
				Server:        "qv2ray.net",
				Port:          50288,
				TransportType: "kcp",
				Seed:          "69f04be3-d64e-45a3-8550-af3172c63055",
				Tag:           "VLESSmKCPSeed",
			},
		},
		{
			Link: "vmess://44efe52b-e143-46b5-a9e7-aadbfd77eb9c@qv2ray.net:6939?type=ws&security=tls&host=qv2ray.net&path=%2Fsomewhere#VMessWebSocketTLS",
			Want: &link.Xray{
				Scheme:        "vmess",
				UUID:          "44efe52b-e143-46b5-a9e7-aadbfd77eb9c",
				Server:        "qv2ray.net",
				Port:          6939,
				TransportType: "ws",
				Security:      "tls",
				Host:          "qv2ray.net",
				Path:          "/somewhere",
				Tag:           "VMessWebSocketTLS",
			},
		},
		{
			Link: "vmess://%E5%AF%86%E7%A0%81@qv2ray.net:31415?type=kcp&seed=%E4%B8%AD%E6%96%87#%E8%8A%82%E7%82%B9%E5%90%8D",
			Want: &link.Xray{
				Scheme:        "vmess",
				UUID:          "密码",
				Server:        "qv2ray.net",
				Port:          31415,
				Tag:           "节点名",
				TransportType: "kcp",
				Seed:          "中文",
			},
		},
	})
}
