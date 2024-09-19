package link_test

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestHysteria2(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		link string
		want link.Hysteria2
	}{
		{
			link: "hysteria2://letmein@example.com/?insecure=1&obfs=salamander&obfs-password=gawrgura&pinSHA256=&sni=real.example.com#remarks",
			want: link.Hysteria2{
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
			link: "hy2://letmein@example.com/?insecure=1&obfs=salamander&obfs-password=gawrgura&pinSHA256=&sni=real.example.com#remarks",
			want: link.Hysteria2{
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
			link: "hysteria2://letmein:password@example.com/?insecure=1&obfs=salamander&obfs-password=gawrgura&pinSHA256=&sni=real.example.com#remarks",
			want: link.Hysteria2{
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
	}
	for _, tc := range testCases {
		u, err := url.Parse(tc.link)
		if err != nil {
			t.Error(err)
			continue
		}
		link, err := link.ParseHysteria2(u)
		if err != nil {
			t.Error(err)
			continue
		}
		if *link != tc.want {
			t.Errorf("want %v, got %v", tc.want, link)
		}
	}
}

func TestHysteria2Convert(t *testing.T) {
	t.Parallel()
	testCases := []*link.Hysteria2{
		{
			Auth:         "letmein汉字",
			Host:         "host",
			Port:         443,
			Obfs:         "salamander",
			ObfsPassword: "gawrgura汉字",
			SNI:          "real.example.com",
			Insecure:     true,
			PingSHA256:   "",
			Remarks:      "remarks汉字",
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
			link, err := link.ParseHysteria2(u)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(link, tc) {
				t.Errorf("want %#v, got %#v", tc, link)
			}
		})
	}
}
