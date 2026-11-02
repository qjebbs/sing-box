package balancer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.InterfaceUpdateListener = (*Balancer)(nil)

// Balancer is the load balancer
type Balancer struct {
	*healthcheck.HealthCheck
	Adapter providersAdapter

	logger  log.ContextLogger
	cfg     balancerConfig
	profile Profile

	Objective Objective
	Strategy  Strategy

	networks []string

	// snapshotAccess guards the cached ranking of the nodes, so a pick does not
	// rebuild and re-sort every node of the group for every connection it carries.
	snapshotAccess sync.RWMutex
	snapshot       map[string]*nodeSnapshot
	// generation is bumped whenever the results that the ranking reads are updated,
	// see Invalidate, so a cached ranking is only reused while it describes them.
	generation atomic.Uint64
}

// nodeSnapshot is the ranking of the nodes of one network, kept between picks until the
// results that made it change.
type nodeSnapshot struct {
	generation  uint64
	providersAt []time.Time
	all         []*Node
	selected    []*Node
	// chosen is the node the group picked, kept by a single node group while another
	// one is not better by more than the tolerance.
	chosen string
}

type providersAdapter interface {
	Providers() []adapter.Provider
}

// New creates a new load balancer
func New(
	logger log.ContextLogger,
	adapter providersAdapter,
	hc *healthcheck.HealthCheck,
	options option.LoadBalancePickOptions,
	profile Profile,
) (*Balancer, error) {
	cfg, err := configFromOptions(hc.Storage.Cap(), options)
	if err != nil {
		return nil, err
	}
	objective, err := newObjective(cfg)
	if err != nil {
		return nil, err
	}
	strategy, err := newStrategy(cfg)
	if err != nil {
		return nil, err
	}
	return &Balancer{
		HealthCheck: hc,
		Adapter:     adapter,
		logger:      logger,
		cfg:         cfg,
		profile:     profile,
		Objective:   objective,
		Strategy:    strategy,
	}, nil
}

// PickNode picks the node for a connection, nil when no node is available.
func (b *Balancer) PickNode(ctx context.Context, network string, destination M.Socksaddr) *Node {
	metadata := adapter.ContextFrom(ctx)
	if metadata == nil {
		metadata = &adapter.InboundContext{}
	}
	metadata.Destination = destination
	all, selected := b.selection(network)
	return b.Strategy.Pick(all, selected, metadata)
}

// Invalidate drops the cached ranking, so the next pick sees the results of the checks
// that just finished. It is called after a round of checks and after the check of a
// single node, see LoadBalance.healthChecksUpdated.
func (b *Balancer) Invalidate() {
	b.generation.Add(1)
}

// selection returns the nodes of the network, ranked and selected for the objective. The
// ranking is reused while the results it describes are unchanged, so a connection pays
// for a lookup instead of rebuilding and sorting every node of the group.
func (b *Balancer) selection(network string) (all []*Node, selected []*Node) {
	generation := b.generation.Load()
	b.snapshotAccess.RLock()
	snapshot := b.snapshot[network]
	usable := snapshot != nil && snapshot.generation == generation && !b.providersChanged(snapshot)
	b.snapshotAccess.RUnlock()
	if usable {
		return snapshot.all, snapshot.selected
	}

	// One ranking at a time: the connections that arrive while it is being built wait
	// for it instead of building their own.
	b.snapshotAccess.Lock()
	defer b.snapshotAccess.Unlock()
	generation = b.generation.Load()
	if snapshot = b.snapshot[network]; snapshot != nil && snapshot.generation == generation && !b.providersChanged(snapshot) {
		return snapshot.all, snapshot.selected
	}
	var chosen string
	if previous := b.snapshot[network]; previous != nil {
		chosen = previous.chosen
	}
	// The update times are read before the nodes they describe, so a provider that
	// changes while the ranking is being built leaves a snapshot that the next pick
	// sees as outdated.
	providersAt := b.providersUpdatedAt()
	all = b.Nodes(network)
	selected = Filter(b.Objective, all)
	if b.profile.Single {
		selected, chosen = b.keepCurrent(chosen, all, selected)
	}
	if b.snapshot == nil {
		b.snapshot = make(map[string]*nodeSnapshot)
	}
	b.snapshot[network] = &nodeSnapshot{
		generation:  generation,
		providersAt: providersAt,
		all:         all,
		selected:    selected,
		chosen:      chosen,
	}
	return all, selected
}

// keepCurrent replaces the selection of a single node group with the node it chose
// while that node is still as good as the best one within the tolerance. A change takes
// every new connection with it, so a difference that is not worth it keeps the node
// where it is.
//
// It runs while the ranking is built, so the choice is made once per check round rather
// than once per connection.
func (b *Balancer) keepCurrent(chosen string, all []*Node, selected []*Node) ([]*Node, string) {
	if len(selected) == 0 {
		return selected, ""
	}
	best := selected[0]
	scorer, isScorer := b.Objective.(Scorer)
	if !isScorer {
		return selected, best.Tag()
	}
	if chosen != "" && chosen != best.Tag() {
		for _, node := range all {
			if node.Tag() != chosen {
				continue
			}
			if node.Status == best.Status && scorer.Score(node) <= clusterLimit(scorer.Score(best), b.tolerance()) {
				return []*Node{node}, chosen
			}
			break
		}
	}
	return selected, best.Tag()
}

// tolerance is the band within which a difference is not worth a different node.
func (b *Balancer) tolerance() float32 {
	return float32Option(b.cfg.Tolerance, DefaultTolerance)
}

// providersChanged reports whether the content of a provider may have changed since the
// snapshot was built.
func (b *Balancer) providersChanged(snapshot *nodeSnapshot) bool {
	providers := b.Adapter.Providers()
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

func (b *Balancer) providersUpdatedAt() []time.Time {
	providers := b.Adapter.Providers()
	updatedAt := make([]time.Time, len(providers))
	for i, provider := range providers {
		updatedAt[i] = provider.UpdatedAt()
	}
	return updatedAt
}

// AvailableNodes returns the tags of the nodes new connections may use
func (b *Balancer) AvailableNodes() []string {
	_, selected := b.GetNodes()
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
func (b *Balancer) LogNodes() {
	all, selected := b.GetNodes()
	if len(all) == 0 {
		return
	}
	available := len(selected)
	b.logger.Info(
		b.cfg.Objective, "/", b.cfg.Strategy, ", ",
		available, " of ", len(all), " nodes available",
	)
	b.logger.Info("=== nodes available ===")
	scorer, isScorer := b.Objective.(Scorer)
	for i, n := range b.Objective.Rank(all) {
		if i == available {
			b.logger.Info("=== nodes unavailable ===")
		}
		if isScorer {
			if score := scorer.Score(n); score != healthcheck.Failed {
				b.logger.Info(n.String(), " SCORE=", score)
			} else {
				// healthcheck.Failed means there is no score to show, printing
				// 0ms would read as the best possible result.
				b.logger.Info(n.String(), " SCORE=None")
			}
			continue
		}
		b.logger.Info(n.String())
	}
}

// RetainedNodes returns the tags to keep alive: the selected nodes, plus the nodes
// in use that just left the selection because of the measurement noise. The extra
// ones only serve the connections they already have, new connections keep to the
// selection.
func (b *Balancer) RetainedNodes(inUse []string) []string {
	all, selected := b.GetNodes()
	if len(selected) == 0 {
		return []string{}
	}
	keeper, isKeeper := b.Objective.(Keeper)
	if !isKeeper || len(inUse) == 0 {
		return nodeTags(selected)
	}
	retained := RetainNodes(all, selected, inUse, keeper)
	if len(retained) > len(selected) {
		b.logger.Debug(
			"load balancing keeps ", len(retained)-len(selected),
			" node(s) out of the selection for their existing connections",
		)
	}
	return retained
}

// Nodes returns all nodes of the network with their health status. A node of an
// outbound group is replaced by the outbound it resolves to, so nested groups are
// measured and picked by the node they actually use.
func (b *Balancer) Nodes(network string) []*Node {
	all := make([]*Node, 0)
	idx := 0
	for _, provider := range b.Adapter.Providers() {
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
			scale := calcFactor(outbound.Tag(), b.cfg.pickBiases)
			stats := b.HealthCheck.Storage.Stats(outbound.Tag())
			status := calcStatus(&stats, b.cfg.maxRTT, b.cfg.maxFailRate)
			all = append(all, NewNode(outbound, idx, scale, stats, status))
		}
	}
	return all
}

// Networks returns the networks supported by the nodes of the group.
func (b *Balancer) Networks() []string {
	if b.networks == nil {
		b.networks = b.availableNetworks()
	}
	return b.networks
}

// AliveTags returns the tags of the nodes that are not dead. Unlike the objective,
// it ignores the selection, so the result only changes when a node's health
// changes, not when it merely stops being the preferred one.
func (b *Balancer) AliveTags() []string {
	nodes := b.Nodes("")
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
func (b *Balancer) availableNetworks() []string {
	var hasTCP, hasUDP bool
	nodes := b.Nodes("")
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
func (b *Balancer) GetNodes() (all, selected []*Node) {
	return b.selection(N.NetworkTCP)
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
