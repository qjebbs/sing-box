package selection

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/sagernet/sing-box/common/healthcheck"
)

// WindowOptions is what to measure of the window a group ranks over.
type WindowOptions struct {
	// Ahead is how many checks a window is asked to predict: the checks that follow it
	// are what the ranking of the window is compared against.
	Ahead int
	// Sizes are the window sizes to measure. Empty measures the default grid, which is
	// every stride up to three checks over the window sizes a group is likely to be
	// configured with.
	Sizes []WindowSize
}

// WindowSize is one window: how many checks it holds, and how many are skipped between
// them.
type WindowSize struct {
	Stride  int
	Samples int
}

// WindowSeries is the history of one node, most recent first, with the node whose score
// the window is priced by.
type WindowSeries struct {
	Node   *Node
	Checks []*healthcheck.History
}

// WindowEvaluation is how well a window predicts the checks that follow it.
type WindowEvaluation struct {
	// Interval is the time between two checks, read from the timestamps: the window is
	// described by how many checks it holds, and this says how wide each of them is.
	Interval time.Duration
	// Nodes is how many nodes were measured, Checks the longest history among them, and
	// Ahead how many checks each window was asked to predict. They are reported even when
	// no window fits, so a report with no measurement says what it was missing.
	Nodes  int
	Checks int
	Ahead  int
	Sizes  []WindowResult
	// Note says why nothing was measured, absent when something was: an empty report is
	// the answer to a question about a history that is not long enough yet, and the
	// caller cannot tell that from the numbers alone.
	Note string
}

// WindowResult is one window size, averaged over every position it was measured at.
type WindowResult struct {
	Stride  int
	Samples int
	Span    time.Duration
	// Windows is how many positions the average is over.
	Windows int
	// Rho is the rank correlation between the score of the window and the score of the
	// checks that follow it, over the same nodes: 1 when a window orders the nodes
	// exactly as their coming checks do, 0 when it orders them at random.
	Rho float64
	// Delta is the standard error of Rho over the positions, zero when the history only
	// fits one of them: a single position is a measurement, but not a precision.
	Delta float64
	// Pool is the number of nodes the selection made of the window would consider, i.e.
	// the ones inside the band around the best node with no failure behind them.
	Pool float64
	// Agreement is how much of the two pools the positions share, the Jaccard index of the
	// nodes the window would select and the nodes the checks after it select: 1 when they
	// are the same set, 0 when they have none in common. Unlike Rho it says nothing about
	// the order inside a pool, which is what a load balance group serves from.
	Agreement float64
	// Exact is the share of positions where the two pools were the same set, the question
	// Agreement averages: a node that entered or left counts there, however small it is.
	Exact float64
	// Reaction is how many checks a node that starts going bad stays in the pool, averaged
	// over Reactions reactions: the price of a window steady enough to agree with itself.
	// A node still in the pool ahead checks later counts as ahead+1.
	Reaction float64
	// Reactions is how many times the history holds a node that turned bad while it was in
	// the pool. Reaction has no meaning without them, so it is zero when there are none.
	Reactions int
	// Recovery is the other side of the same coin: how many checks a node that turns good
	// again takes to come back into the pool, averaged over Recoveries recoveries.
	Recovery float64
	// Recoveries is how many times the history holds a node that turned good again while it
	// was out of the pool.
	Recoveries int
}

// minEvaluationNodes is the fewest nodes a position is measured over: a rank correlation
// of two or three nodes says nothing about the ranking.
const minEvaluationNodes = 4

// maxEvaluationPositions caps the work a single call does: the positions are thinned out
// evenly rather than measured one by one, which keeps the average just as representative.
const maxEvaluationPositions = 64

// EvaluateWindows measures the window a group ranks over against the checks that follow
// it.
//
// It is the same objective that scores every round, applied to a shorter history: the
// checks of a window are priced by healthcheck.StatsOf into the statistics the round
// itself is priced from, and the node is cloned with them, so a change to the score or to
// the band changes what this reports without changing anything here.
func (o *ScoreObjective) EvaluateWindows(series []WindowSeries, options WindowOptions) WindowEvaluation {
	ahead := options.Ahead
	if ahead < 2 {
		ahead = 8
	}
	longest := 0
	for _, node := range series {
		longest = max(longest, len(node.Checks))
	}
	evaluation := WindowEvaluation{
		Interval: checkInterval(series),
		Nodes:    len(series),
		Checks:   longest,
		Ahead:    ahead,
	}
	sizes := options.Sizes
	if len(sizes) == 0 {
		// the default grid: a few densities over the window sizes a group is likely to be
		// configured with, which is what the recommendation in the documentation covers
		for _, stride := range []int{1, 2, 3} {
			for _, samples := range []int{5, 6, 8, 10, 12, 15, 20} {
				sizes = append(sizes, WindowSize{Stride: stride, Samples: samples})
			}
		}
	}
	for _, size := range sizes {
		if size.Stride < 1 || size.Samples < 2 || size.Samples*size.Stride > longest-ahead {
			continue
		}
		result, ok := o.evaluateSize(series, size, ahead, longest, evaluation.Interval)
		if ok {
			evaluation.Sizes = append(evaluation.Sizes, result)
		}
	}
	if len(evaluation.Sizes) == 0 {
		evaluation.Note = missingNote(evaluation, sizes)
	}
	return evaluation
}

// missingNote says what the history was missing, so an empty report reads as a window that
// is not there yet rather than as a group that has nothing.
func missingNote(evaluation WindowEvaluation, sizes []WindowSize) string {
	if evaluation.Nodes == 0 {
		return "no node has a history to measure"
	}
	required := 0
	for _, size := range sizes {
		if needed := size.Samples*size.Stride + evaluation.Ahead; required == 0 || needed < required {
			required = needed
		}
	}
	if required == 0 {
		return fmt.Sprintf("history holds %d checks", evaluation.Checks)
	}
	return fmt.Sprintf(
		"history holds %d checks, the smallest window needs %d: %d checks and %d to predict",
		evaluation.Checks, required, required-evaluation.Ahead, evaluation.Ahead,
	)
}

// evaluateSize averages one window size over every position it fits in.
func (o *ScoreObjective) evaluateSize(
	series []WindowSeries, size WindowSize, ahead int, longest int, interval time.Duration,
) (WindowResult, bool) {
	positions := longest - size.Samples*size.Stride - ahead + 1
	if positions < 1 {
		return WindowResult{}, false
	}
	step := max(1, positions/maxEvaluationPositions)
	var rhos, pools, agreements, exacts []float64
	for offset := ahead; offset < ahead+positions; offset += step {
		measured := o.measureAt(series, size, offset, ahead)
		if len(measured) < minEvaluationNodes {
			continue
		}
		rhos = append(rhos, rankCorrelation(measured))
		windowPool, futurePool := o.poolMembers(measured)
		agreement := jaccardOf(windowPool, futurePool)
		pools = append(pools, float64(len(windowPool)))
		agreements = append(agreements, agreement)
		if agreement == 1 {
			exacts = append(exacts, 1)
		} else {
			exacts = append(exacts, 0)
		}
	}
	if len(rhos) == 0 {
		return WindowResult{}, false
	}
	rho := meanOf(rhos)
	reaction, reactions := o.reactionOf(series, size, ahead, longest)
	recovery, recoveries := o.recoveryOf(series, size, ahead, longest)
	return WindowResult{
		Stride:     size.Stride,
		Samples:    size.Samples,
		Span:       interval * time.Duration(size.Stride*size.Samples),
		Windows:    len(rhos),
		Rho:        rho,
		Delta:      standardErrorOf(rhos, rho),
		Pool:       meanOf(pools),
		Agreement:  meanOf(agreements),
		Exact:      meanOf(exacts),
		Reaction:   reaction,
		Reactions:  reactions,
		Recovery:   recovery,
		Recoveries: recoveries,
	}, true
}

// measuredNode is one node as a window priced it, next to what the checks after the
// window priced it at.
type measuredNode struct {
	// index is the position of the node in the series the window was measured over, so a
	// node keeps its identity across the offsets of the same history.
	index  int
	node   Node
	score  healthcheck.RTT
	future healthcheck.RTT
	// later is the node priced by the checks after the window: the band of the future
	// selection is sized by that window, not by the one being measured.
	later     Node
	fail      int
	laterFail int
}

// measureAt prices every node over the window at offset and over the checks that follow
// it, skipping the nodes one of the two windows has nothing to say about.
func (o *ScoreObjective) measureAt(series []WindowSeries, size WindowSize, offset int, ahead int) []measuredNode {
	measured := make([]measuredNode, 0, len(series))
	for index, node := range series {
		window := node.checksStrided(offset, size)
		if len(window) == 0 {
			continue
		}
		stats := node.statsOf(window, node.longRunAt(offset))
		if stats.Succeeded() == 0 {
			continue
		}
		future := node.statsOf(node.Checks[offset-ahead:offset], node.longRunAt(offset-ahead))
		if future.Succeeded() == 0 {
			continue
		}
		current := *node.Node
		current.Stats = stats
		later := current
		later.Stats = future
		measured = append(measured, measuredNode{
			index:     index,
			node:      current,
			score:     o.Score(&current),
			future:    o.Score(&later),
			later:     later,
			fail:      stats.Fail,
			laterFail: future.Fail,
		})
	}
	return measured
}

// measuredWindowAt prices every node over the window at offset alone: a window older than
// the horizon has no complete future left to be measured against, but it still has a pool,
// and that pool is what a group would have served from at the time.
func (o *ScoreObjective) measuredWindowAt(series []WindowSeries, size WindowSize, offset int) []measuredNode {
	measured := make([]measuredNode, 0, len(series))
	for index, node := range series {
		window := node.checksStrided(offset, size)
		if len(window) == 0 {
			continue
		}
		stats := node.statsOf(window, node.longRunAt(offset))
		if stats.Succeeded() == 0 {
			continue
		}
		current := *node.Node
		current.Stats = stats
		measured = append(measured, measuredNode{
			index: index,
			node:  current,
			score: o.Score(&current),
			fail:  stats.Fail,
		})
	}
	return measured
}

// checksStrided returns the checks of the window at offset: Samples of them, Stride checks
// apart. A window the history is too short for is no window at all rather than a shorter
// one, which would be priced on a different number of checks than the others.
func (s WindowSeries) checksStrided(offset int, size WindowSize) []*healthcheck.History {
	end := offset + size.Samples*size.Stride
	if end > len(s.Checks) {
		return nil
	}
	checks := make([]*healthcheck.History, 0, size.Samples)
	for i := offset; i < end; i += size.Stride {
		checks = append(checks, s.Checks[i])
	}
	return checks
}

// statsOf prices a window of the history the way a round is priced: against the baseline
// the node has now and the failure estimate of the checks older than the window, which are
// the two inputs the storage keeps outside its window.
func (s WindowSeries) statsOf(checks []*healthcheck.History, longRun float64) healthcheck.Stats {
	// the expiry of the window is left to zero: it is what makes the storage recompute its
	// statistics, and the score reads none of it
	return healthcheck.StatsOf(checks, s.Node.Stats.Floor, time.Time{}, longRun)
}

// longRunAt returns the failure estimate the storage held when the check at offset was the
// newest one: everything from there back, replayed, see healthcheck.ReplayFailureRate. A
// history that does not reach behind the window has nothing to replay and reports no
// estimate, which is what a group that keeps only its sampling would have had.
func (s WindowSeries) longRunAt(offset int) float64 {
	if offset >= len(s.Checks) {
		return 0
	}
	return healthcheck.ReplayFailureRate(s.Checks[offset:])
}

// poolMembers returns the nodes the selection made of the window would consider, and those
// of the nodes the selection made of the checks after it would: both are read by the band
// of their own best node, so the two sets are the ones a group would have served from at
// the two moments. A node is identified by its position in measured, which is the same in
// both readings.
func (o *ScoreObjective) poolMembers(measured []measuredNode) (window, future map[int]struct{}) {
	window = o.poolOf(measured, func(node measuredNode) (Node, healthcheck.RTT, int) {
		return node.node, node.score, node.fail
	})
	future = o.poolOf(measured, func(node measuredNode) (Node, healthcheck.RTT, int) {
		return node.later, node.future, node.laterFail
	})
	return window, future
}

// poolOf returns the nodes the band around the best one would let through: the nodes with
// no failure behind them whose score is within the limit of the best node, as view reads
// both out of a measured node. The nodes are keyed by the position they hold in the series,
// so the pools of two offsets of the same history can be compared.
func (o *ScoreObjective) poolOf(
	measured []measuredNode, view func(measuredNode) (Node, healthcheck.RTT, int),
) map[int]struct{} {
	best := 0
	var bestScore healthcheck.RTT
	for i := range measured {
		_, score, _ := view(measured[i])
		if i == 0 || score < bestScore {
			best, bestScore = i, score
		}
	}
	node, _, _ := view(measured[best])
	limit := o.Limit(&node)
	members := make(map[int]struct{}, len(measured))
	for _, each := range measured {
		_, score, fail := view(each)
		if fail == 0 && score <= limit {
			members[each.index] = struct{}{}
		}
	}
	return members
}

// jaccardOf returns how much of the two sets is shared: 1 when they are the same set, 0
// when they share nothing. Two empty sets are the same set.
func jaccardOf(a, b map[int]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	shared := 0
	for node := range a {
		if _, ok := b[node]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// reactionOf returns how many checks a node that starts slowing down stays in the pool,
// averaged over the reactions the history holds.
//
// A check is bad when it is slower than twice the node's floor, the rule the statistics
// count as Bad. A failure is not a reaction: the pool already requires a window with no
// failure in it, so a node is dropped on the check that fails, whatever the window holds.
// What a longer window can do is keep serving a node that merely got slower, and that is
// what this measures.
//
// The event is read from the window that does not hold the bad check yet, which has to
// include the node, so the answer is about a node that was being served. A node still in
// the pool ahead checks later returns ahead+1, the horizon of the report.
func (o *ScoreObjective) reactionOf(
	series []WindowSeries, size WindowSize, ahead int, longest int,
) (float64, int) {
	limit := longest - size.Samples*size.Stride
	if limit < 1 {
		return 0, 0
	}
	known := newPools(o, series, size, limit+1)
	var lags []float64
	for index, node := range series {
		for check := 0; check+1 < len(node.Checks); check++ {
			if !o.badCheck(&node, check) || o.badCheck(&node, check+1) {
				// only the first check of a run of bad ones is where the node turned bad
				continue
			}
			// the window that does not see it yet, and that starts after the horizon
			before := check + 1
			if before+ahead > limit || before < ahead {
				continue
			}
			if _, served := known.at(before)[index]; !served {
				continue
			}
			lag := float64(ahead + 1)
			for step := 1; step <= ahead; step++ {
				if _, kept := known.at(before - step)[index]; !kept {
					lag = float64(step)
					break
				}
			}
			lags = append(lags, lag)
		}
	}
	return meanOf(lags), len(lags)
}

// recoveryOf returns how many checks a node that is usable again takes to get back into
// the pool, averaged over the recoveries the history holds.
//
// It is the other half of the same question, and it is paired with the drop that preceded
// it: the node has to have been served when it turned bad, so the answer is about a node
// the group had and lost, not about the many nodes that were never in the pool. Both a
// failure and a slow run count as turning bad, because both cost checks: a failure leaves
// the pool at once, but it has to leave the window before the node can come back, which is
// what makes a single failed check cost as many checks as the window holds. That is why the
// horizon here is the window rather than the prediction: ahead checks are not enough to see
// a node come back from a failure.
func (o *ScoreObjective) recoveryOf(
	series []WindowSeries, size WindowSize, ahead int, longest int,
) (float64, int) {
	limit := longest - size.Samples*size.Stride
	if limit < 1 {
		return 0, 0
	}
	// the horizon is the whole window: a failure has to leave the window before the node
	// can come back, so a return measured over the horizon of the prediction would report
	// the window itself as the answer
	horizon := max(ahead, size.Samples*size.Stride)
	if horizon > limit {
		return 0, 0
	}
	known := newPools(o, series, size, limit+1)
	var lags []float64
	for index, node := range series {
		dropped := false
		// older to newer, which is the order the node lives through
		for check := len(node.Checks) - 2; check >= 0; check-- {
			if !o.goodCheck(&node, check) && o.goodCheck(&node, check+1) {
				// the node turned bad: it counts only if it was being served
				before := check + 1
				_, dropped = known.at(before)[index]
				dropped = dropped && before <= limit && before >= horizon
				continue
			}
			if !dropped || !o.goodCheck(&node, check) || o.goodCheck(&node, check+1) {
				continue
			}
			// the window that does not see the recovery yet
			start := check + 1
			if start > limit || start < horizon {
				continue
			}
			dropped = false
			if _, back := known.at(start)[index]; back {
				// it never left, so there is no return to measure
				continue
			}
			lag := float64(horizon + 1)
			for step := 1; step <= horizon; step++ {
				if _, back := known.at(start - step)[index]; back {
					lag = float64(step)
					break
				}
			}
			lags = append(lags, lag)
		}
	}
	return meanOf(lags), len(lags)
}

// pools remembers the pool of one window size at each offset of the history, so the
// metrics that walk it pay for each offset once.
type pools struct {
	objective *ScoreObjective
	series    []WindowSeries
	size      WindowSize
	known     map[int]map[int]struct{}
}

func newPools(o *ScoreObjective, series []WindowSeries, size WindowSize, capacity int) *pools {
	return &pools{
		objective: o,
		series:    series,
		size:      size,
		known:     make(map[int]map[int]struct{}, capacity),
	}
}

// at returns the pool of the window at offset, empty when the window is too short to be
// measured there.
func (p *pools) at(offset int) map[int]struct{} {
	if cached, ok := p.known[offset]; ok {
		return cached
	}
	measured := p.objective.measuredWindowAt(p.series, p.size, offset)
	var window map[int]struct{}
	if len(measured) >= minEvaluationNodes {
		window, _ = p.objective.poolMembers(measured)
	}
	p.known[offset] = window
	return window
}

// badCheck reports whether the check at index is slower than twice the node's floor, the
// rule the statistics count as Bad. A node without a floor has no bad checks: nothing says
// what slow means for it yet.
func (o *ScoreObjective) badCheck(series *WindowSeries, index int) bool {
	floor := series.Node.Stats.Floor
	if floor <= 0 {
		return false
	}
	// a failed check is not a slow one, and it is zero, so the comparison rules it out too
	return series.Checks[index].Delay > floor*2
}

// goodCheck reports whether the check at index is neither failed nor slower than twice the
// node's floor, the state a node has to be in to be qualified again.
func (o *ScoreObjective) goodCheck(series *WindowSeries, index int) bool {
	if series.Checks[index].Delay == healthcheck.Failed {
		return false
	}
	return !o.badCheck(series, index)
}

// checkInterval returns the time between two checks, from the median gap of the first
// history that has one.
func checkInterval(series []WindowSeries) time.Duration {
	for _, node := range series {
		if len(node.Checks) < 2 {
			continue
		}
		gaps := make([]float64, 0, len(node.Checks)-1)
		for i := 0; i < len(node.Checks)-1; i++ {
			gaps = append(gaps, float64(node.Checks[i].Time.Sub(node.Checks[i+1].Time)))
		}
		sort.Float64s(gaps)
		return time.Duration(medianOfSorted(gaps))
	}
	return 0
}

// rankCorrelation returns the Spearman rank correlation between the score of the window
// and the score of the checks that follow it.
func rankCorrelation(measured []measuredNode) float64 {
	scores := make([]float64, len(measured))
	futures := make([]float64, len(measured))
	for i, node := range measured {
		scores[i] = float64(node.score)
		futures[i] = float64(node.future)
	}
	return pearsonOf(averageRanks(scores), averageRanks(futures))
}

// averageRanks returns the rank of each value, 1 for the smallest, ties sharing the mean
// of the ranks they cover: a score is a whole number of milliseconds, so ties are the
// rule rather than the exception.
func averageRanks(values []float64) []float64 {
	order := make([]int, len(values))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return values[order[a]] < values[order[b]]
	})
	ranks := make([]float64, len(values))
	for i := 0; i < len(order); {
		j := i
		for j+1 < len(order) && values[order[j+1]] == values[order[i]] {
			j++
		}
		rank := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			ranks[order[k]] = rank
		}
		i = j + 1
	}
	return ranks
}

func pearsonOf(a []float64, b []float64) float64 {
	meanA, meanB := meanOf(a), meanOf(b)
	var covariance, varianceA, varianceB float64
	for i := range a {
		da, db := a[i]-meanA, b[i]-meanB
		covariance += da * db
		varianceA += da * da
		varianceB += db * db
	}
	if varianceA == 0 || varianceB == 0 {
		return 0
	}
	return covariance / math.Sqrt(varianceA*varianceB)
}

func meanOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func standardErrorOf(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += (value - mean) * (value - mean)
	}
	return math.Sqrt(sum/float64(len(values))) / math.Sqrt(float64(len(values)))
}

func medianOfSorted(sorted []float64) float64 {
	switch {
	case len(sorted) == 0:
		return 0
	case len(sorted)%2 == 1:
		return sorted[len(sorted)/2]
	default:
		return (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
}

// WindowEvaluator is the objective of a group that can measure its own window, i.e. the
// ones that rank a score. It is what the group is asked for rather than what it computes:
// the measurement is only worth reading if the score it ranks by is the one it measures.
type WindowEvaluator interface {
	EvaluateWindows(series []WindowSeries, options WindowOptions) WindowEvaluation
}
