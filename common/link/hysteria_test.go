package link_test

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestHysteria(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		link string
		want link.Hysteria
	}{
		{
			link: "hysteria://host:443?protocol=udp&auth=123456&peer=sni.domain&insecure=1&upmbps=100&downmbps=100&alpn=hysteria&obfs=xplus&obfsParam=123456#remarks",
			want: link.Hysteria{
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
	}
	for _, tc := range testCases {
		u, err := url.Parse(tc.link)
		if err != nil {
			t.Error(err)
			continue
		}
		link, err := link.ParseHysteria(u)
		if err != nil {
			t.Error(err)
			continue
		}
		if *link != tc.want {
			t.Errorf("want %v, got %v", tc.want, link)
		}
	}
}

func TestHysteriaConvert(t *testing.T) {
	t.Parallel()
	testCases := []*link.Hysteria{
		{
			Host:      "host",
			Port:      443,
			Protocol:  "udp",
			Auth:      "123456密码",
			Peer:      "sni.domain",
			Insecure:  true,
			UpMpbs:    100,
			DownMpbs:  100,
			ALPN:      "hysteria",
			Obfs:      "xplus",
			ObfsParam: "123456",
			Remarks:   "remarks",
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
			link, err := link.ParseHysteria(u)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(link, tc) {
				t.Errorf("want %#v, got %#v", tc, link)
			}
		})
	}
}
