package remote

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sagernet/sing-box/adapter"
	E "github.com/sagernet/sing/common/exceptions"
)

// ParseInfo parses the info
func ParseInfo(content string) (*adapter.ProviderInfo, error) {
	return parseShadowrocket(content)
}

// parseShadowrocket parses the info of Shadowrocket, e.g.:
// STATUS=🚀↑:0.53GB,↓:14.07GB,TOT:160GB💡Expires:2023-12-05
func parseShadowrocket(content string) (*adapter.ProviderInfo, error) {
	content = doBase64DecodeOrNothing(content)
	// split the first line of content
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) != 2 {
		return nil, E.New("invalid content")
	}
	line := strings.TrimSpace(lines[0])
	// remove emoji icons
	emojiRegex := regexp.MustCompile(`[🚀💡]`)
	line = emojiRegex.ReplaceAllString(line, ",")
	// remove the prefix "STATUS=,"
	if !strings.HasPrefix(line, "STATUS=,") {
		return nil, E.New("invalid content")
	}
	line = line[8:]
	// split sections with ","
	sections := strings.Split(line, ",")
	info := &adapter.ProviderInfo{}
	for _, section := range sections {
		// split key and value with ":"
		parts := strings.SplitN(section, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "↑":
			info.Upload = parseSize(value)
		case "↓":
			info.Download = parseSize(value)
		case "TOT":
			info.Total = parseSize(value)
		case "Expires":
			info.Expire = parseExpire(value)
		}
	}
	return info, nil
}

// parseSize parses the size, e.g.:
// 0.53GB
func parseSize(size string) int {
	var unit string
	var value float64
	_, err := fmt.Sscanf(size, "%f%s", &value, &unit)
	if err != nil {
		return 0
	}
	switch unit {
	case "GB":
		return int(value * 1024 * 1024 * 1024)
	case "MB":
		return int(value * 1024 * 1024)
	case "KB":
		return int(value * 1024)
	}
	return 0
}

// parseExpire parses the expire, e.g.:
// 2023-12-05
func parseExpire(expire string) int {
	t, err := time.Parse("2006-01-02", expire)
	if err != nil {
		return 0
	}
	return int(t.Unix())
}
