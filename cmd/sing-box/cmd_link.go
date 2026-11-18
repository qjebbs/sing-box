package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// parseLinkOutbounds parses outbound references into outbound tags.
//
// An argument is either the tag of an outbound from the configuration file, or a
// share link. The tag of a link is generated when the link does not carry one.
func parseLinkOutbounds(args []string) (tags []string, linkOutbounds []option.Outbound, requireConfig bool, err error) {
	for index, arg := range args {
		uri, parseErr := url.Parse(arg)
		if parseErr != nil || uri.Scheme == "" {
			requireConfig = true
			tags = append(tags, arg)
			continue
		}
		parsedLink, linkErr := link.Parse(arg)
		if linkErr != nil {
			return nil, nil, false, linkErr
		}
		outbound, outboundErr := parsedLink.Outbound(option.ProviderOutboundsOptions{})
		if outboundErr != nil {
			return nil, nil, false, outboundErr
		}
		if outbound.Tag == "" {
			outbound.Tag = fmt.Sprintf("outbound%d", index+1)
		}
		tags = append(tags, outbound.Tag)
		linkOutbounds = append(linkOutbounds, *outbound)
	}
	return tags, linkOutbounds, requireConfig, nil
}

// chainOutbounds returns the detour tag of the given outbound tags, and the relay
// outbound chaining them in order when more than one tag is given.
func chainOutbounds(tags []string) (detour string, relay *option.Outbound) {
	if len(tags) == 1 {
		return tags[0], nil
	}
	detour = strings.Join(tags, " => ")
	relay = &option.Outbound{
		Tag:  detour,
		Type: C.TypeRelay,
		Options: &option.RelayOptions{
			Outbounds: tags,
		},
	}
	return detour, relay
}
