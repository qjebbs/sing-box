package link

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
)

// errors
var (
	ErrNotImplemented = E.New("not implemented")
	ErrBadFormat      = E.New("bad format")
)

// Link is the interface for links
type Link interface {
	// URL returns the url representation of the link
	URL() (string, error)
	// Outbound returns equivalent outbound options of the link
	Outbound(defaultOptions option.ProviderOutboundsOptions) (*option.Outbound, error)
}

// Parse parses a link string to Link
func Parse(s string) (Link, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	return ParseURL(u)
}

// ParseURL parses a link url to Link
func ParseURL(u *url.URL) (Link, error) {
	ps, err := getParsers(u)
	if err != nil {
		return nil, err
	}
	errs := make([]error, 0, len(ps))
	for _, p := range ps {
		lk, err := p.Parse(u)
		if err == nil {
			return lk, nil
		}
		errs = append(errs, fmt.Errorf("[%s] %s", p.Name, err))
	}
	if len(errs) == 1 {
		return nil, errs[0]
	}
	return nil, E.Errors(errs...)
}

func base64Decode(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	stdb64 := b64
	if pad := len(b64) % 4; pad != 0 {
		stdb64 += strings.Repeat("=", 4-pad)
	}

	b, err := base64.StdEncoding.DecodeString(stdb64)
	if err != nil {
		return base64.URLEncoding.DecodeString(b64)
	}
	return b, nil
}

func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func parseDuration(value string) (badoption.Duration, error) {
	d, err := time.ParseDuration(value)
	if err == nil {
		return badoption.Duration(d), nil
	}
	seconds, parseErr := time.ParseDuration(value + "s")
	if parseErr != nil {
		return 0, err
	}
	return badoption.Duration(seconds), nil
}

func isTrue(value string) bool {
	switch strings.ToLower(value) {
	case "1", "true":
		return true
	default:
		return false
	}
}

func anyTrue(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case string:
		return isTrue(value)
	case float64:
		return value != 0
	case int:
		return value != 0
	default:
		return false
	}
}

func splitComma(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
