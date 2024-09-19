package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"
)

// fileContent is the content from provider
type fileContent struct {
	*Information

	links     string
	linksHash string
	raw       string
	updated   time.Time
}

func parseFileContent(content string, updated time.Time) (*fileContent, error) {
	info, _ := ParseInfo(content)
	fc := &fileContent{
		Information: info,
		raw:         content,
		updated:     updated,
	}
	content = doBase64DecodeOrNothing(content)
	hasher := sha256.New()
	lines := strings.Split(content, "\n")
	links := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		if u.Scheme == "" {
			continue
		}
		links = append(links, line)
		hasher.Write([]byte(line))
	}

	fc.linksHash = hex.EncodeToString(hasher.Sum(nil))
	fc.links = strings.Join(links, "\n")
	return fc, nil
}
