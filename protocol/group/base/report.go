package base

import (
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/protocol/group/selection"
	N "github.com/sagernet/sing/common/network"
)

// HealthReport implements adapter.HealthReporter. It reports every node of the group
// with the statistics the ranking reads, and the checks those statistics were computed
// from, so a ranking can be explained rather than only observed.
func (g *Group) HealthReport() adapter.HealthReport {
	if g.pick == nil || g.HealthCheck == nil {
		// the group is not started yet, there is nothing it has measured
		return adapter.HealthReport{Tag: g.Tag(), Type: g.Type()}
	}
	_, selected, _ := g.selection(N.NetworkTCP)
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
		Alive:      g.AliveTags(),
	}
	nodes := g.Nodes("")
	report.Nodes = make([]adapter.HealthNode, 0, len(nodes))
	for _, node := range nodes {
		report.Nodes = append(report.Nodes, g.nodeReport(node, selectedTags[node.Tag()]))
	}
	return report
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
