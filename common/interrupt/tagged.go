package interrupt

import (
	"io"
	"net"
	"slices"
	"sync"
)

// TaggedGroup is a Group variant for outbound groups whose members come and go
// at runtime, such as load balancing.
//
// Every connection is bound to the tag of the member it uses, and Interrupt only
// closes the connections whose member is no longer current, so connections on
// the remaining members are kept. Groups with a single stable selection don't
// need this: they can simply close all their connections on every change.
type TaggedGroup struct {
	access sync.Mutex
	groups map[string]*Group
}

func NewTaggedGroup() *TaggedGroup {
	return &TaggedGroup{
		groups: make(map[string]*Group),
	}
}

func (g *TaggedGroup) group(outboundTag string) *Group {
	group, loaded := g.groups[outboundTag]
	if !loaded {
		group = NewGroup()
		g.groups[outboundTag] = group
	}
	return group
}

// Add registers a closer bound to outboundTag, see Group.Add.
func (g *TaggedGroup) Add(closer io.Closer, isExternal bool, outboundTag string) (remove func()) {
	g.access.Lock()
	group := g.group(outboundTag)
	g.access.Unlock()
	return group.Add(closer, isExternal)
}

func (g *TaggedGroup) NewConn(conn net.Conn, isExternal bool, outboundTag string) net.Conn {
	g.access.Lock()
	group := g.group(outboundTag)
	g.access.Unlock()
	return group.NewConn(conn, isExternal)
}

func (g *TaggedGroup) NewPacketConn(conn net.PacketConn, isExternal bool, outboundTag string) net.PacketConn {
	g.access.Lock()
	group := g.group(outboundTag)
	g.access.Unlock()
	return group.NewPacketConn(conn, isExternal)
}

// Tags returns the outbound tags that currently have connections. A tag whose
// connections are all closed is not reported, even though it is still known.
func (g *TaggedGroup) Tags() []string {
	g.access.Lock()
	defer g.access.Unlock()
	tags := make([]string, 0, len(g.groups))
	for outboundTag, group := range g.groups {
		if group.empty() {
			continue
		}
		tags = append(tags, outboundTag)
	}
	return tags
}

// Interrupt closes the connections bound to members not included in
// currentOutboundTags, the connections of the remaining members are kept.
//
// The members of keptOutboundTags are in between: their connections to the outside
// are kept as well, but the connections originating from this group are closed and
// the tag is forgotten, so it is treated as any other member next time.
func (g *TaggedGroup) Interrupt(
	interruptExternalConnections bool, currentOutboundTags []string, keptOutboundTags []string,
) {
	g.access.Lock()
	var outdatedGroups, keptGroups []*Group
	for outboundTag, group := range g.groups {
		if slices.Contains(currentOutboundTags, outboundTag) {
			continue
		}
		if slices.Contains(keptOutboundTags, outboundTag) {
			keptGroups = append(keptGroups, group)
			continue
		}
		outdatedGroups = append(outdatedGroups, group)
		delete(g.groups, outboundTag)
	}
	g.access.Unlock()
	for _, group := range keptGroups {
		// External connections stay, the internal ones don't. The tag stays in the
		// map so the remaining connections are still tracked.
		group.Interrupt(false)
	}
	for _, group := range outdatedGroups {
		group.Interrupt(interruptExternalConnections)
	}
}
