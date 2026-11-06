package base

import (
	"time"

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

// HealthWindows implements adapter.HealthWindowsReporter. It measures the window the group
// ranks over against the checks that follow it, using the objective that ranks the group,
// so the answer describes the ranking rather than a copy of it.
//
// The checks of a node are cut down to the ones its statistics were computed from, so a
// window measured here is one the group could have measured: the storage keeps a check
// past its validity until the ring overwrites it.
func (g *Group) HealthWindows(options adapter.HealthWindowsOptions) adapter.HealthWindowsReport {
	report := adapter.HealthWindowsReport{Tag: g.Tag()}
	if g.pick == nil || g.HealthCheck == nil {
		// the group is not started yet, there is nothing it has measured
		return report
	}
	evaluator, isEvaluator := g.pick.Objective().(selection.WindowEvaluator)
	if !isEvaluator {
		// an objective that ranks no score has no window to measure
		return report
	}
	report.Objective = g.pick.Objective().Name()
	all := g.Nodes(N.NetworkTCP)
	if len(all) == 0 {
		all = g.Nodes("")
	}
	series := make([]selection.WindowSeries, 0, len(all))
	for _, node := range all {
		checks := g.HealthCheck.Storage.All(node.Tag())
		if len(checks) >= 2 {
			series = append(series, selection.WindowSeries{Node: node, Checks: checks})
		}
	}
	evaluation := evaluator.EvaluateWindows(series, selection.WindowOptions{
		Ahead: options.Ahead,
		Sizes: windowSizesOf(options.Configs),
	})
	report.Interval = evaluation.Interval.String()
	report.Nodes = evaluation.Nodes
	report.Checks = evaluation.Checks
	report.Ahead = evaluation.Ahead
	report.Note = evaluation.Note
	report.Configs = make([]adapter.HealthWindowResult, 0, len(evaluation.Sizes))
	for _, size := range evaluation.Sizes {
		report.Configs = append(report.Configs, adapter.HealthWindowResult{
			Stride:     size.Stride,
			Samples:    size.Samples,
			Span:       size.Span.String(),
			Windows:    size.Windows,
			Rho:        size.Rho,
			Delta:      size.Delta,
			Pool:       size.Pool,
			Agreement:  size.Agreement,
			Exact:      size.Exact,
			Reaction:   size.Reaction,
			Reactions:  size.Reactions,
			Recovery:   size.Recovery,
			Recoveries: size.Recoveries,
		})
	}
	return report
}

// ResizeHealthHistory implements adapter.HealthHistoryResizer. The checks already collected
// are kept: only the sizes of the history change.
//
// The sampling is what the ranking reads, so the nodes are rebuilt with it: the share of
// failures a node may have is a number of checks, and it has to stay that number of checks
// rather than turn into a different limit.
func (g *Group) ResizeHealthHistory(sampling uint, extra uint) adapter.HealthHistory {
	if g.HealthCheck == nil {
		// the group is not started yet, there is no history to keep
		return adapter.HealthHistory{}
	}
	options := g.HealthCheck.ConfigureHistory(sampling, extra)
	// the ranking is stale as soon as the sampling it was built from moves
	g.Invalidate()
	return adapter.HealthHistory{
		Sampling: options.Sampling,
		Extra:    options.Retained - options.Sampling,
		Retained: options.Retained,
		Span:     (options.Interval * time.Duration(options.Retained)).String(),
	}
}

func windowSizesOf(configs []adapter.HealthWindowConfig) []selection.WindowSize {
	sizes := make([]selection.WindowSize, 0, len(configs))
	for _, config := range configs {
		sizes = append(sizes, selection.WindowSize{Stride: config.Stride, Samples: config.Samples})
	}
	return sizes
}

// HealthText reports the health of the group as text: the line that states the selection,
// and one line per node, in the order of the ranking.
//
// It is not a second form of HealthReport: what that report states as fields, this one
// states as the sentence built from them, so a group can be read without a log and
// without parsing a tree of numbers.
func (g *Group) HealthText() (summary string, nodes []string) {
	if g.pick == nil || g.HealthCheck == nil {
		// the group is not started yet, there is nothing it has measured
		return "", nil
	}
	all, selected, _ := g.selection(N.NetworkTCP)
	if len(all) == 0 {
		// No node serves the network the selection is made for: describe what the group
		// has, with nothing selected.
		all = g.Nodes("")
	}
	ranked := g.pick.Objective().Rank(all)
	return g.pick.Summary(ranked, selected), selection.NodeLines(g.pick.Objective(), ranked, selected)
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
		Tag:       node.Tag(),
		Index:     node.Index,
		Status:    node.Status.String(),
		Suspended: node.Stats.Suspended,
		Networks:  node.Network(),
		Selected:  selected,
		Score:     g.scoreOf(node),
		All:       node.All,
		Fail:      node.Fail,
	}
	if node.Stats.Succeeded() > 0 {
		report.Latest = optionalRTT(node.Latest)
		report.Median = optionalRTT(node.Median)
		report.Average = optionalRTT(node.Average)
		report.Min = optionalRTT(node.Min)
		report.Max = optionalRTT(node.Max)
		report.Jitter = optionalRTT(node.Jitter)
		report.MeanDeviation = optionalRTT(node.MeanDeviation)
		report.Floor = optionalRTT(node.Floor)
		report.Bad = &node.Bad
		pressure, badShare := selection.IdleTerms(node)
		report.Pressure = &pressure
		report.BadShare = &badShare
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
