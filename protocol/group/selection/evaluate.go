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
	var rhos, pools []float64
	for offset := ahead; offset < ahead+positions; offset += step {
		measured := o.measureAt(series, size, offset, ahead)
		if len(measured) < minEvaluationNodes {
			continue
		}
		rhos = append(rhos, rankCorrelation(measured))
		pools = append(pools, float64(o.poolOf(measured)))
	}
	if len(rhos) == 0 {
		return WindowResult{}, false
	}
	rho := meanOf(rhos)
	return WindowResult{
		Stride:  size.Stride,
		Samples: size.Samples,
		Span:    interval * time.Duration(size.Stride*size.Samples),
		Windows: len(rhos),
		Rho:     rho,
		Delta:   standardErrorOf(rhos, rho),
		Pool:    meanOf(pools),
	}, true
}

// measuredNode is one node as a window priced it, next to what the checks after the
// window priced it at.
type measuredNode struct {
	node   Node
	score  healthcheck.RTT
	future healthcheck.RTT
	fail   int
}

// measureAt prices every node over the window at offset and over the checks that follow
// it, skipping the nodes one of the two windows has nothing to say about.
func (o *ScoreObjective) measureAt(series []WindowSeries, size WindowSize, offset int, ahead int) []measuredNode {
	measured := make([]measuredNode, 0, len(series))
	for _, node := range series {
		window := node.checksStrided(offset, size)
		if len(window) == 0 {
			continue
		}
		stats := node.statsOf(window)
		if stats.Succeeded() == 0 {
			continue
		}
		future := node.statsOf(node.Checks[offset-ahead : offset])
		if future.Succeeded() == 0 {
			continue
		}
		current := *node.Node
		current.Stats = stats
		later := current
		later.Stats = future
		measured = append(measured, measuredNode{
			node:   current,
			score:  o.Score(&current),
			future: o.Score(&later),
			fail:   stats.Fail,
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
// the node has now, which is the one input the storage keeps outside its window.
func (s WindowSeries) statsOf(checks []*healthcheck.History) healthcheck.Stats {
	floor := s.Node.Stats.Floor
	if succeededOf(checks) < 2 {
		// too short to set a baseline, and the storage reports no baseline in that case
		floor = 0
	}
	// the expiry of the window is left to zero: it is what makes the storage recompute its
	// statistics, and the score reads none of it
	return healthcheck.StatsOf(checks, floor, time.Time{})
}

// poolOf returns how many nodes the band around the best one would let through.
func (o *ScoreObjective) poolOf(measured []measuredNode) int {
	best := 0
	for i := range measured {
		if measured[i].score < measured[best].score {
			best = i
		}
	}
	limit := o.Limit(&measured[best].node)
	count := 0
	for i := range measured {
		if measured[i].fail == 0 && measured[i].score <= limit {
			count++
		}
	}
	return count
}

func succeededOf(checks []*healthcheck.History) int {
	succeeded := 0
	for _, check := range checks {
		if check.Delay != healthcheck.Failed {
			succeeded++
		}
	}
	return succeeded
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
