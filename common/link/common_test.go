package link_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/sagernet/sing-box/common/link"
)

type TestCases[T link.Link] []TestCase[T]

type TestCase[T link.Link] struct {
	Link string
	Want T
}

type ParseFunc[T link.Link] func(*url.URL) (T, error)

func runTests[T link.Link](t *testing.T, parser ParseFunc[T], tests TestCases[T]) {
	t.Parallel()
	for i, tc := range tests {
		i, tc := i, tc
		t.Run(fmt.Sprint("Parse() #", i), func(t *testing.T) {
			t.Parallel()
			t.Logf("parsing %s", tc.Link)
			r, err := tc.Want.URL()
			if err == nil {
				t.Logf("URL() gives: %s", r)
			} else {
				t.Logf("URL() gives: %s", err)
			}
			u, err := url.Parse(tc.Link)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parser(u)
			if err != nil {
				t.Error(err)
				return
			}
			if err := assertJSONEqual(&tc.Want, got); err != nil {
				t.Errorf("Parse Test #%d: %s", i, err)
			}
		})
		t.Run(fmt.Sprint("URL() #", i), func(t *testing.T) {
			t.Parallel()
			uri, err := tc.Want.URL()
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(uri)
			if err != nil {
				t.Fatal(err)
			}
			link, err := parser(u)
			if err != nil {
				t.Error(err)
				return
			}
			if err := assertJSONEqual(tc.Want, link); err != nil {
				t.Errorf("Convert Test #%d: %s", i, err)
			}
		})
	}
}

func assertJSONEqual(want, got any) error {
	wantBytes, err := json.Marshal(want)
	if err != nil {
		return err
	}
	gotBytes, err := json.Marshal(got)
	if err != nil {
		return err
	}
	wantStr := string(wantBytes)
	gotStr := string(gotBytes)
	if wantStr != gotStr {
		return fmt.Errorf("want %s, got %s", wantStr, gotStr)
	}
	return nil
}
