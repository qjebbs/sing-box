package link_test

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

func TestTrojanQt5(t *testing.T) {
	t.Parallel()
	testCases := []*link.TrojanQt5{
		{
			Remarks:       "remarks",
			Host:          "192.168.1.1",
			Port:          443,
			Password:      "password-密码",
			AllowInsecure: true,
			TFO:           true,
		},
		{
			Remarks:       "remarks",
			Host:          "example.com",
			Port:          443,
			Password:      "password-密码",
			AllowInsecure: true,
			SNI:           "example.org",
			TFO:           true,
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
			link, err := link.ParseTrojanQt5(u)
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(link, tc) {
				t.Errorf("want %#v, got %#v", tc, link)
			}
		})
	}
}
