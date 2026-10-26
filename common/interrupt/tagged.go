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

// Interrupt closes the connections bound to members not included in
// currentOutboundTags, the connections of the remaining members are kept.
func (g *TaggedGroup) Interrupt(interruptExternalConnections bool, currentOutboundTags []string) {
	g.access.Lock()
	var outdatedGroups []*Group
	for outboundTag, group := range g.groups {
		if slices.Contains(currentOutboundTags, outboundTag) {
			continue
		}
		outdatedGroups = append(outdatedGroups, group)
		delete(g.groups, outboundTag)
	}
	g.access.Unlock()
	for _, group := range outdatedGroups {
		group.Interrupt(interruptExternalConnections)
	}
}
