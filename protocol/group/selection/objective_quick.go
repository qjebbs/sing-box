package selection

import (
	"math"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

const (
	// DefaultQuickTolerance is the default tolerance of quick, around the fastest node.
	// Its score is a delay of hundreds of milliseconds, so a difference worth switching
	// for is a share of a second: 50ms of a 500ms node.
	DefaultQuickTolerance = 50 * healthcheck.Millisecond
)

func jitterRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.Jitter)
}

func medianRTT(node *Node) healthcheck.RTT {
	return node.ScaleRTT(node.Median)
}

// quickScore is what an attempt on the node costs: the delay it typically takes, plus the
// delay its typical variation adds, with the failures priced in by their share.
func quickScore(node *Node) healthcheck.RTT {
	return node.Stats.Cost(medianRTT(node), jitterRTT(node))
}

// quickError is the error of quickScore: the median and the jitter are two estimates of
// the same samples, each with its own error, combined by their variance.
func quickError(node *Node) float64 {
	n := float64(node.Stats.Succeeded())
	medianError := medianToError * float64(node.ScaleRTT(node.Stats.Spread)) / math.Sqrt(n)
	jitterError := jitterToError * float64(node.ScaleRTT(node.Stats.DeviationSpread)) / math.Sqrt(n)
	return math.Sqrt(medianError*medianError + jitterError*jitterError)
}

// NewQuickObjective returns a new ScoreObjective. It ranks the nodes by what they
// typically cost: the median delay of the checks, plus the jitter, since a node that is
// near but unstable delays the traffic as a node that is simply slower does. The spread is
// the typical one, so a check that timed out once does not decide it, see
// healthcheck.Stats.MeanDeviation for the one that counts it.
func NewQuickObjective(options option.LoadBalancePickOptions) *ScoreObjective {
	objective := newScoreObjective(options, DefaultQuickTolerance)
	objective.name = ObjectiveQuick
	objective.scoreFunc = quickScore
	objective.noiseFunc = jitterRTT
	objective.errorFunc = quickError
	return objective
}
