package base

import (
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/protocol/group/selection"
	N "github.com/sagernet/sing/common/network"
)

// HealthReport implements adapter.HealthReporter. It reports every node of the network
// the group ranks, with the statistics the ranking reads and the checks those statistics
// were computed from, so a ranking can be explained rather than only observed.
//
// The nodes and the selection are read from the same snapshot of the ranking, never from
// the storage: a round of checks writes its results node by node and drops the ranking
// only when the last one is in, so a live read of the statistics would describe something
// that no selection was made from.
func (g *Group) HealthReport() adapter.HealthReport {
	if g.pick == nil || g.HealthCheck == nil {
		// the group is not started yet, there is nothing it has measured
		return adapter.HealthReport{Tag: g.Tag(), Type: g.Type()}
	}
	all, selected, best := g.selection(N.NetworkTCP)
	if len(all) == 0 {
		// No node serves the network the selection is made for: list what the group
		// has, with nothing selected.
		all = g.Nodes("")
	}
	selectedTags := make(map[string]bool, len(selected))
	for _, node := range selected {
		selectedTags[node.Tag()] = true
	}
	report := adapter.HealthReport{
		Tag:        g.Tag(),
		Type:       g.Type(),
		Objective:  g.pick.Objective().Name(),
		Selection:  g.pick.String(),
		Networks:   g.Networks(),
		Generation: g.generation.Load(),
		Providers:  g.providersReport(),
		Selected:   selection.Tags(selected),
		Alive:      aliveTagsOf(all),
		Limit:      g.limitOf(best),
		Band:       g.bandOf(best),
	}
	if clusterer, isClusterer := g.pick.Objective().(selection.Clusterer); isClusterer {
		report.Tolerance = optionalRTT(clusterer.Tolerance())
		report.MinNodes = clusterer.MinNodes()
		report.MaxNodes = clusterer.MaxNodes()
	}
	if best != nil {
		report.Best = best.Tag()
	}
	report.Nodes = make([]adapter.HealthNode, 0, len(all))
	for _, node := range all {
		report.Nodes = append(report.Nodes, g.nodeReport(node, selectedTags[node.Tag()]))
	}
	return report
}

// limitOf returns the score up to which another node still counts as equally good as
// best, nil for an objective whose band is not bounded and for a node that has none.
func (g *Group) limitOf(best *selection.Node) *uint16 {
	if best == nil {
		return nil
	}
	bander, isBander := g.pick.Objective().(selection.Bander)
	if !isBander {
		return nil
	}
	return optionalRTT(bander.Limit(best))
}

// bandOf returns the width of the band around the best node, the tolerance plus the margin
// the measurement adds to it, nil for an objective that has none.
func (g *Group) bandOf(best *selection.Node) *uint16 {
	band, loaded := selection.BandOf(g.pick.Objective(), best)
	if !loaded {
		return nil
	}
	return optionalRTT(band)
}

func (g *Group) providersReport() []adapter.HealthProvider {
	providers := g.Providers()
	report := make([]adapter.HealthProvider, 0, len(providers))
	for _, provider := range providers {
		report = append(report, adapter.HealthProvider{
			Tag:       provider.Tag(),
			Type:      provider.Type(),
			UpdatedAt: provider.UpdatedAt(),
		})
	}
	return report
}

// nodeReport reports one node. selected is whether the selection of the network new
// connections use reaches it, which is false for a node of another network even when the
// objective has a score for it: the score is a property of the node, the selection is not.
func (g *Group) nodeReport(node *selection.Node, selected bool) adapter.HealthNode {
	report := adapter.HealthNode{
		Tag:      node.Tag(),
		Index:    node.Index,
		Status:   node.Status.String(),
		Networks: node.Network(),
		Selected: selected,
		Score:    g.scoreOf(node),
		All:      node.All,
		Fail:     node.Fail,
	}
	if node.Stats.Succeeded() > 0 {
		report.Latest = optionalRTT(node.Latest)
		report.Median = optionalRTT(node.Median)
		report.Average = optionalRTT(node.Average)
		report.Min = optionalRTT(node.Min)
		report.Max = optionalRTT(node.Max)
		report.Jitter = optionalRTT(node.Jitter)
		report.MeanDeviation = optionalRTT(node.MeanDeviation)
		report.Spread = optionalRTT(node.Spread)
		report.DeviationSpread = optionalRTT(node.DeviationSpread)
	}
	if !node.Expires.IsZero() {
		expires := node.Expires
		report.Expires = &expires
	}
	samples := g.HealthCheck.Storage.All(node.Tag())
	report.Samples = make([]adapter.HealthSample, 0, len(samples))
	for _, sample := range samples {
		report.Samples = append(report.Samples, adapter.HealthSample{
			Time:  sample.Time,
			Delay: uint16(sample.Delay),
		})
	}
	return report
}

// scoreOf returns the score the objective ranks the node by, nil for an objective that
// does not score, so a group of the alive objective reports no score instead of a zero.
func (g *Group) scoreOf(node *selection.Node) *uint16 {
	scorer, isScorer := g.pick.Objective().(selection.Scorer)
	if !isScorer {
		return nil
	}
	score := scorer.Score(node)
	if score == healthcheck.Failed {
		return nil
	}
	value := uint16(score)
	return &value
}

// optionalRTT returns the delay to report, nil for a value that is absent.
func optionalRTT(rtt healthcheck.RTT) *uint16 {
	if rtt == healthcheck.Failed {
		return nil
	}
	value := uint16(rtt)
	return &value
}
