package base

import (
	"context"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/protocol/group/selection"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// nodeSnapshot is the ranking of the nodes of one network, kept between picks until the
// results that made it change.
type nodeSnapshot struct {
	generation  uint64
	providersAt []time.Time
	all         []*selection.Node
	// best is the head of the ranking, the node every other one is compared to.
	best     *selection.Node
	selected []*selection.Node
	// chosen is the node the group picked, kept by a single node selection while
	// another one is not better by more than the tolerance.
	chosen string
}

// PickNode picks the node for a connection, nil when no node is available.
func (g *Group) PickNode(ctx context.Context, network string, destination M.Socksaddr) *selection.Node {
	metadata := adapter.ContextFrom(ctx)
	if metadata == nil {
		metadata = &adapter.InboundContext{}
	}
	metadata.Destination = destination
	all, selected, _ := g.selection(network)
	return g.pick.Pick(all, selected, metadata)
}

// Invalidate drops the cached ranking, so the next pick sees the results of the checks
// that just finished. It is called after a round of checks and after the check of a
// single node, see healthChecksUpdated.
func (g *Group) Invalidate() {
	g.generation.Add(1)
}

// selection returns the nodes of the network, ranked and selected by the pick. The
// ranking is reused while the results it describes are unchanged, so a connection pays
// for a lookup instead of rebuilding and sorting every node of the group.
func (g *Group) selection(network string) (all []*selection.Node, selected []*selection.Node, best *selection.Node) {
	generation := g.generation.Load()
	g.snapshotAccess.RLock()
	snapshot := g.snapshot[network]
	usable := snapshot != nil && snapshot.generation == generation && !g.providersChanged(snapshot)
	g.snapshotAccess.RUnlock()
	if usable {
		return snapshot.all, snapshot.selected, snapshot.best
	}

	// One ranking at a time: the connections that arrive while it is being built wait
	// for it instead of building their own.
	g.snapshotAccess.Lock()
	defer g.snapshotAccess.Unlock()
	generation = g.generation.Load()
	if snapshot = g.snapshot[network]; snapshot != nil && snapshot.generation == generation && !g.providersChanged(snapshot) {
		return snapshot.all, snapshot.selected, snapshot.best
	}
	previous := ""
	if old := g.snapshot[network]; old != nil {
		previous = old.chosen
	}
	// The update times are read before the nodes they describe, so a provider that
	// changes while the ranking is being built leaves a snapshot that the next pick
	// sees as outdated.
	providersAt := g.providersUpdatedAt()
	all = g.Nodes(network)
	ranked := g.pick.Objective().Rank(all)
	if len(ranked) > 0 {
		best = ranked[0]
	}
	selected, chosen := g.pick.Select(ranked, previous)
	if g.snapshot == nil {
		g.snapshot = make(map[string]*nodeSnapshot)
	}
	g.snapshot[network] = &nodeSnapshot{
		generation:  generation,
		providersAt: providersAt,
		all:         all,
		best:        best,
		selected:    selected,
		chosen:      chosen,
	}
	return all, selected, best
}

// providersChanged reports whether the content of a provider may have changed since the
// snapshot was built.
func (g *Group) providersChanged(snapshot *nodeSnapshot) bool {
	providers := g.Providers()
	if len(providers) != len(snapshot.providersAt) {
		return true
	}
	for i, provider := range providers {
		if provider.UpdatedAt() != snapshot.providersAt[i] {
			return true
		}
	}
	return false
}

func (g *Group) providersUpdatedAt() []time.Time {
	providers := g.Providers()
	updatedAt := make([]time.Time, len(providers))
	for i, provider := range providers {
		updatedAt[i] = provider.UpdatedAt()
	}
	return updatedAt
}

// AvailableNodes returns the tags of the nodes new connections may use
func (g *Group) AvailableNodes() []string {
	_, selected := g.GetNodes()
	if len(selected) == 0 {
		return []string{}
	}
	return common.Uniq(common.Map(
		selected, func(node *selection.Node) string {
			return node.Tag()
		},
	))
}

// RetainedNodes returns the tags to keep alive: the selected nodes, plus the nodes
// in use that just left the selection because of the measurement noise. The extra
// ones only serve the connections they already have, new connections keep to the
// selection.
func (g *Group) RetainedNodes(inUse []string) []string {
	all, selected, best := g.selection(N.NetworkTCP)
	if len(selected) == 0 {
		return []string{}
	}
	keeper, isKeeper := g.pick.Objective().(selection.Keeper)
	if !isKeeper || len(inUse) == 0 {
		return selection.Tags(selected)
	}
	retained := selection.RetainNodes(all, selected, best, inUse, keeper)
	if len(retained) > len(selected) {
		g.logger.Debug(
			"load balancing keeps ", len(retained)-len(selected),
			" node(s) out of the selection for their existing connections",
		)
	}
	return retained
}

// Nodes returns all nodes of the network with their health status. A node of an
// outbound group is replaced by the outbound it resolves to, so nested groups are
// measured and picked by the node they actually use.
func (g *Group) Nodes(network string) []*selection.Node {
	all := make([]*selection.Node, 0)
	idx := 0
	for _, provider := range g.Providers() {
		for _, outbound := range provider.Outbounds() {
			idx++
			if network != "" && !common.Contains(outbound.Network(), network) {
				continue
			}
			if group, ok := outbound.(adapter.OutboundGroup); ok {
				real, err := adapter.RealOutbound(group)
				if err != nil {
					continue
				}
				outbound = real
			}
			stats := g.HealthCheck.Storage.Stats(outbound.Tag())
			all = append(all, g.nodes.NewNode(outbound, idx, stats))
		}
	}
	return all
}

// Networks returns the networks supported by the nodes of the group.
func (g *Group) Networks() []string {
	if g.networks == nil {
		g.networks = g.availableNetworks()
	}
	return g.networks
}

// AliveTags returns the tags of the nodes that are not dead. Unlike the objective,
// it ignores the selection, so the result only changes when a node's health
// changes, not when it merely stops being the preferred one.
func (g *Group) AliveTags() []string {
	return aliveTagsOf(g.Nodes(""))
}

// aliveTagsOf returns the tags of the nodes that are not dead, in the order given.
func aliveTagsOf(nodes []*selection.Node) []string {
	alive := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Status != selection.StatusDead {
			alive = append(alive, node.Tag())
		}
	}
	if len(alive) == 0 {
		// No node is alive: nothing can be served, but outbounds may still be
		// tried, keep them referenced like the selection does.
		return selection.Tags(nodes)
	}
	return common.Uniq(alive)
}

// availableNetworks returns available networks of the nodes of the group.
func (g *Group) availableNetworks() []string {
	var hasTCP, hasUDP bool
	nodes := g.Nodes("")
	for _, n := range nodes {
		if !hasTCP && common.Contains(n.Network(), N.NetworkTCP) {
			hasTCP = true
		}
		if !hasUDP && common.Contains(n.Network(), N.NetworkUDP) {
			hasUDP = true
		}
		if hasTCP && hasUDP {
			break
		}
	}
	switch {
	case hasTCP && hasUDP:
		return []string{N.NetworkTCP, N.NetworkUDP}
	case hasTCP:
		return []string{N.NetworkTCP}
	case hasUDP:
		return []string{N.NetworkUDP}
	default:
		return []string{}
	}
}

// GetNodes returns all nodes and the selected ones, in preference order
func (g *Group) GetNodes() (all, selected []*selection.Node) {
	all, selected, _ = g.selection(N.NetworkTCP)
	return
}
