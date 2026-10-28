package group

import (
	"sort"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing/common"
)

// outboundSource is the provider side of the group: the outbounds it may use.
type outboundSource interface {
	Outbounds() []adapter.Outbound
}

// candidate is an outbound of the group with the health check statistics that
// decide whether it is used.
type candidate struct {
	// outbound is what the group dials, its network is what the candidate is
	// selected for.
	outbound adapter.Outbound
	// tag is what the health check statistics are stored by: a nested group is
	// checked by the outbound it resolves to.
	tag   string
	stats healthcheck.Stats
}

// class is the group a candidate is ranked in. The classes are ordered, a candidate
// of a better class is always preferred, whatever its delay.
type class int

const (
	// classMeasured is a candidate that passed its latest health check: the delay
	// is the only thing to compare.
	classMeasured class = iota
	// classUnmeasured is a candidate that was never checked, or whose statistics
	// expired. It is preferred over a failed candidate, but there is no delay to
	// compare it with the measured ones.
	classUnmeasured
	// classFailed is a candidate whose latest health check failed. It is only used
	// when nothing else is left.
	classFailed
)

func (c *candidate) class() class {
	switch {
	case c.stats.All == 0:
		return classUnmeasured
	case c.stats.Latest == healthcheck.Failed:
		return classFailed
	default:
		return classMeasured
	}
}

// selection is the node selection of the group. It reads the health check statistics
// and ranks the candidates itself: this group uses one outbound at a time, so it
// takes the one with the lowest median delay, and keeps it while another one is not
// faster by more than the tolerance.
type selection struct {
	source    outboundSource
	storage   *healthcheck.Storages
	tolerance healthcheck.RTT

	access   sync.Mutex
	selected map[string]selectionEntry
}

// selectionEntry is the candidate selected for a network. A stale entry is the last
// selection, kept to tell whether the new best candidate is worth a switch.
type selectionEntry struct {
	node  *candidate
	stale bool
}

func newSelection(source outboundSource, storage *healthcheck.Storages, tolerance healthcheck.RTT) *selection {
	return &selection{
		source:    source,
		storage:   storage,
		tolerance: tolerance,
		selected:  make(map[string]selectionEntry),
	}
}

// Selected returns the candidate selected for the network, nil when the group has no
// outbound for it. The selection is only computed again after it was invalidated.
func (s *selection) Selected(network string) *candidate {
	s.access.Lock()
	defer s.access.Unlock()
	if entry, loaded := s.selected[network]; loaded && !entry.stale {
		return entry.node
	}
	node := s.selectNode(network)
	if node != nil {
		s.selected[network] = selectionEntry{node: node}
	}
	return node
}

// Reselect selects the candidate for the network from the current health check
// results, and reports whether it replaced the previous selection. The previous
// candidate is still the one it is compared with, so the tolerance applies.
func (s *selection) Reselect(network string) (node *candidate, changed bool) {
	s.access.Lock()
	defer s.access.Unlock()
	previous := s.selected[network].node
	node = s.selectNode(network)
	if node == nil {
		delete(s.selected, network)
		return nil, previous != nil
	}
	s.selected[network] = selectionEntry{node: node}
	return node, previous != nil && previous.tag != node.tag
}

// Invalidate marks the cached selections stale, so the next call selects from the
// current health check results.
func (s *selection) Invalidate() {
	s.access.Lock()
	for network, entry := range s.selected {
		entry.stale = true
		s.selected[network] = entry
	}
	s.access.Unlock()
}

// candidates returns the outbounds of the network with their statistics, in rank
// order.
func (s *selection) candidates(network string) []*candidate {
	var all []*candidate
	seen := make(map[string]struct{})
	for _, outbound := range s.source.Outbounds() {
		if !common.Contains(outbound.Network(), network) {
			continue
		}
		tag := healthCheckTag(outbound)
		if _, loaded := seen[tag]; loaded {
			// the same outbound can be listed by several providers
			continue
		}
		seen[tag] = struct{}{}
		all = append(all, &candidate{
			outbound: outbound,
			tag:      tag,
			stats:    s.storage.Stats(tag),
		})
	}
	sort.SliceStable(all, func(i, j int) bool {
		left, right := all[i], all[j]
		leftClass, rightClass := left.class(), right.class()
		if leftClass != rightClass {
			return leftClass < rightClass
		}
		if leftClass != classMeasured {
			// no delay to compare, the declaration order is the best guess
			return false
		}
		return left.stats.Median < right.stats.Median
	})
	return all
}

// selectNode returns the candidate for the network. The current selection is kept
// while no other candidate is faster by more than the tolerance.
func (s *selection) selectNode(network string) *candidate {
	all := s.candidates(network)
	if len(all) == 0 {
		return nil
	}
	best := all[0]
	previous := s.selected[network].node
	if previous == nil || previous.tag == best.tag {
		return best
	}
	for _, node := range all {
		if node.tag != previous.tag {
			continue
		}
		if s.keep(node, best) {
			return node
		}
		break
	}
	return best
}

// keep reports whether the previous candidate should stay selected although another
// one is better. The difference must be within the tolerance, so a switch that the
// statistics cannot tell apart from noise doesn't drop the connections of the
// previous candidate.
func (s *selection) keep(previous, best *candidate) bool {
	if previous.class() != best.class() {
		// the classes are ordered, a candidate that lost its class is not kept
		return false
	}
	if best.class() != classMeasured {
		// no delay to compare, don't switch between nodes that look the same
		return true
	}
	return uint32(previous.stats.Median) <= uint32(best.stats.Median)+uint32(s.tolerance)
}

// healthCheckTag returns the tag the health check statistics of the outbound are
// stored by. A group of outbounds is checked by the outbound it resolves to.
func healthCheckTag(outbound adapter.Outbound) string {
	if group, isGroup := outbound.(adapter.OutboundGroup); isGroup {
		real, err := adapter.RealOutbound(group)
		if err == nil {
			return real.Tag()
		}
	}
	return outbound.Tag()
}
