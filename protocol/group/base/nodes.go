package base

import (
	"context"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// nodeSnapshot is the ranking of the nodes of one network, kept between picks until the
// results that made it change.
type nodeSnapshot struct {
	generation  uint64
	providersAt []time.Time
	all         []*Node
	selected    []*Node
	// chosen is the node the group picked, kept by a single node selection while
	// another one is not better by more than the tolerance.
	chosen string
}

// PickNode picks the node for a connection, nil when no node is available.
func (g *Group) PickNode(ctx context.Context, network string, destination M.Socksaddr) *Node {
	metadata := adapter.ContextFrom(ctx)
	if metadata == nil {
		metadata = &adapter.InboundContext{}
	}
	metadata.Destination = destination
	all, selected := g.selection(network)
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
func (g *Group) selection(network string) (all []*Node, selected []*Node) {
	generation := g.generation.Load()
	g.snapshotAccess.RLock()
	snapshot := g.snapshot[network]
	usable := snapshot != nil && snapshot.generation == generation && !g.providersChanged(snapshot)
	g.snapshotAccess.RUnlock()
	if usable {
		return snapshot.all, snapshot.selected
	}

	// One ranking at a time: the connections that arrive while it is being built wait
	// for it instead of building their own.
	g.snapshotAccess.Lock()
	defer g.snapshotAccess.Unlock()
	generation = g.generation.Load()
	if snapshot = g.snapshot[network]; snapshot != nil && snapshot.generation == generation && !g.providersChanged(snapshot) {
		return snapshot.all, snapshot.selected
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
	selected, chosen := g.pick.Select(ranked, previous)
	if g.snapshot == nil {
		g.snapshot = make(map[string]*nodeSnapshot)
	}
	g.snapshot[network] = &nodeSnapshot{
		generation:  generation,
		providersAt: providersAt,
		all:         all,
		selected:    selected,
		chosen:      chosen,
	}
	return all, selected
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
		selected, func(node *Node) string {
			return node.Tag()
		},
	))
}

// LogNodes logs the statistics of every node, the selected ones first.
func (g *Group) LogNodes() {
	all, selected := g.GetNodes()
	if len(all) == 0 {
		return
	}
	available := len(selected)
	g.logger.Info(
		g.pick.String(), ", ", available, " of ", len(all), " nodes available",
	)
	g.logger.Info("=== nodes available ===")
	objective := g.pick.Objective()
	scorer, isScorer := objective.(Scorer)
	for i, n := range objective.Rank(all) {
		if i == available {
			g.logger.Info("=== nodes unavailable ===")
		}
		if isScorer {
			if score := scorer.Score(n); score != healthcheck.Failed {
				g.logger.Info(n.String(), " SCORE=", score)
			} else {
				// healthcheck.Failed means there is no score to show, printing
				// 0ms would read as the best possible result.
				g.logger.Info(n.String(), " SCORE=None")
			}
			continue
		}
		g.logger.Info(n.String())
	}
}

// RetainedNodes returns the tags to keep alive: the selected nodes, plus the nodes
// in use that just left the selection because of the measurement noise. The extra
// ones only serve the connections they already have, new connections keep to the
// selection.
func (g *Group) RetainedNodes(inUse []string) []string {
	all, selected := g.GetNodes()
	if len(selected) == 0 {
		return []string{}
	}
	keeper, isKeeper := g.pick.Objective().(Keeper)
	if !isKeeper || len(inUse) == 0 {
		return nodeTags(selected)
	}
	retained := RetainNodes(all, selected, inUse, keeper)
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
func (g *Group) Nodes(network string) []*Node {
	all := make([]*Node, 0)
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
			scale := calcFactor(outbound.Tag(), g.cfg.pickBiases)
			stats := g.HealthCheck.Storage.Stats(outbound.Tag())
			status := calcStatus(&stats, g.cfg.maxRTT, g.cfg.maxFailRate)
			all = append(all, NewNode(outbound, idx, scale, stats, status))
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
	nodes := g.Nodes("")
	alive := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Status != StatusDead {
			alive = append(alive, node.Tag())
		}
	}
	if len(alive) == 0 {
		// No node is alive: nothing can be served, but outbounds may still be
		// tried, keep them referenced like the selection does.
		return common.Uniq(nodeTags(nodes))
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
func (g *Group) GetNodes() (all, selected []*Node) {
	return g.selection(N.NetworkTCP)
}

// RetainNodes returns the tags of selected, plus the tags of inUse that the keeper
// still accepts, in the order of inUse.
func RetainNodes(all, selected []*Node, inUse []string, keeper Keeper) []string {
	retained := nodeTags(selected)
	if len(selected) == 0 || len(inUse) == 0 {
		return retained
	}
	selectedTags := make(map[string]bool, len(retained))
	for _, tag := range retained {
		selectedTags[tag] = true
	}
	nodesByTag := make(map[string]*Node, len(all))
	for _, node := range all {
		nodesByTag[node.Tag()] = node
	}
	best := selected[0]
	for _, tag := range inUse {
		if selectedTags[tag] {
			continue
		}
		node, loaded := nodesByTag[tag]
		if !loaded || !keeper.Keep(node, best) {
			continue
		}
		retained = append(retained, tag)
	}
	return retained
}

func nodeTags(nodes []*Node) []string {
	return common.Uniq(common.Map(nodes, func(node *Node) string {
		return node.Tag()
	}))
}
