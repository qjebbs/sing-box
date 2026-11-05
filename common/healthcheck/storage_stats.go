package healthcheck

import (
	"math"
	"slices"
	"time"

	C "github.com/sagernet/sing-box/constant"
)

// failureDelay is the delay a failed check waited for: a check fails only after the
// whole timeout. It is the cost of a failed attempt only for a node that never worked,
// see Stats.failureCost, and the upper bound of any cost.
var failureDelay = RTTOf(C.TCPTimeout)

// Timeout returns the delay a failed check waited for, the longest a check can take. No
// statistic of the successful checks can exceed it, so it is the cap of every score.
func Timeout() RTT {
	return failureDelay
}

// Stats is the statistics of RTTs
type Stats struct {
	All  int // total number of health checks
	Fail int // number of failed health checks
	// Jitter is the typical distance of the checks to their median, a robust estimate
	// of the standard deviation, see deviationOf.
	Jitter RTT
	// MeanDeviation is the average distance of the checks to their median, see
	// deviationOf.
	MeanDeviation RTT
	// Spread is the sample standard deviation of the checks, DeviationSpread the one of
	// their distances to the median. A standard error is the spread of a statistic over
	// √n, so these are what it is made of: the robust statistics above describe a node
	// well, but they are not the sum of squares a standard error is computed from.
	Spread          RTT
	DeviationSpread RTT
	// Floor is the fastest the node has been, the baseline of the node, see baseline. It
	// is the reference the idle objective measures against: what a node waits over its
	// own best, rather than over other nodes.
	Floor RTT
	// Bad is how many of the successful checks came out slower than
	// badCheckMultiple times the floor, the ones that are not merely noisy but queueing.
	// Their share is the frequency the idle objective counts, next to the depth.
	Bad     int
	Average RTT // average RTT of all health checks
	Median  RTT // median RTT of all health checks
	Max     RTT // maximum RTT of all health checks
	Min     RTT // minimum RTT of all health checks
	Latest  RTT // latest RTT of all health checks

	Expires time.Time // time of the statistics expires
}

// Stats get statistics and write cache for next call
// Make sure use Mutex.Lock() before calling it, RWMutex.RLock()
// is not an option since it writes cache.
//
// The delay statistics describe the checks that succeeded only. A failed check waited
// for the timeout, so it says nothing about the delay the node has when it works: a
// score that has to account for the failures reads Fail and All, and mixes the timeout
// in by their own share, see Stats.Cost. Counting a failure as a delay of the
// timeout instead would make the delays a description of both, and the consumer would
// have no way to tell them apart.
func (s *Storage) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	now := time.Now().Round(0)
	if !s.stats.Expires.IsZero() && now.Before(s.stats.Expires) {
		return s.stats
	}
	s.refreshStats(now)
	return s.stats
}

func (s *Storage) refreshStats(now time.Time) {
	latest, loaded := s.history.At(0)
	if !loaded || now.Sub(latest.Time) > s.validity {
		s.stats = Stats{}
		return
	}
	delays := make([]RTT, 0, s.history.Length())
	var expiresAt time.Time
	fail := 0
	for i := 0; i < s.history.Length(); i++ {
		// from latest to oldest
		history, _ := s.history.At(i)
		itemExpiresAt := history.Time.Add(s.validity)
		if itemExpiresAt.Before(now) {
			// the latter is invalid, so are the formers
			break
		}
		// the time when the oldest item expires
		expiresAt = itemExpiresAt
		if history.Delay == Failed {
			fail++
		} else {
			delays = append(delays, history.Delay)
		}
	}
	var (
		floor    RTT
		hasFloor bool
		bad      int
	)
	if len(delays) >= 2 {
		// The baseline is the fastest the node has been, so what goes into it is the
		// fastest check of the round, not the median of the round: a minimum of medians
		// says nothing about what the node can do, and in a quiet period every node is at
		// its own median, which made every score zero and left nothing to rank.
		//
		// The round is written before it is read, because a minimum over real delays
		// cannot make a score of zero: the baseline can never be above the median of the
		// round that just set it.
		s.baseline.Put(now, minOf(delays))
		floor, hasFloor = s.baseline.RTT(now)
		if hasFloor {
			// Counted before newStats, which replaces the delays with their distances to
			// the median, see deviationOf.
			bad = countAbove(delays, floor*badCheckMultiple)
		}
	}
	stats := newStats(len(delays)+fail, fail, latest.Delay, expiresAt, delays)
	stats.Floor, stats.Bad = floor, bad
	s.stats = stats
}

// badCheckMultiple is how much slower than the floor a check has to be to count as bad: at
// twice the fastest the node has been, the wait has not grown by a jitter, it has been
// doubled.
const badCheckMultiple = 2

// minOf returns the smallest of the values.
func minOf(values []RTT) RTT {
	min := RTT(math.MaxUint16)
	for _, value := range values {
		if min > value {
			min = value
		}
	}
	return min
}

// countAbove returns how many of the values are greater than limit.
func countAbove(values []RTT, limit RTT) int {
	count := 0
	for _, value := range values {
		if value > limit {
			count++
		}
	}
	return count
}

// newStats returns the statistics of the values, all of the checks they are a part of,
// and the number of those that failed.
func newStats(all int, fail int, latest RTT, expires time.Time, values []RTT) Stats {
	stats := Stats{
		All:     all,
		Fail:    fail,
		Latest:  latest,
		Expires: expires,
	}
	if len(values) == 0 {
		return stats
	}
	// the values are kept in milliseconds, sum them as int to not overflow
	sum := 0
	min := RTT(math.MaxUint16)
	for _, delay := range values {
		sum += int(delay)
		if min > delay {
			min = delay
		}
		if stats.Max < delay {
			stats.Max = delay
		}
	}
	stats.Average = RTT(sum / len(values))
	stats.Median = medianOf(values)
	stats.Min = min
	if len(values) < 2 {
		// a single check says nothing about how much the node varies, we assume it is
		// half of its delay. Without it a node whose one check came out low would look
		// like the steadiest one there is, and be picked over the nodes that are
		// measured.
		stats.Jitter = stats.Average / 2
		stats.MeanDeviation = stats.Average / 2
		return stats
	}
	stats.Spread = standardDeviationOf(values)
	stats.Jitter, stats.MeanDeviation = deviationOf(values, stats.Median)
	stats.DeviationSpread = standardDeviationOf(values)
	return stats
}

// standardDeviationOf returns the sample standard deviation of the values, zero for
// fewer than two of them.
//
// The statistics the groups rank by are robust ones, but the accuracy of an estimate is
// not a robust quantity: it is a sum of squares, and that is what a standard error is
// made of. So the spreads are kept as statistics of their own rather than read off the
// robust ones.
func standardDeviationOf(values []RTT) RTT {
	if len(values) < 2 {
		return 0
	}
	sum := 0
	for _, value := range values {
		sum += int(value)
	}
	mean := float64(sum) / float64(len(values))
	squares := 0.0
	for _, value := range values {
		difference := float64(value) - mean
		squares += difference * difference
	}
	return RTT(math.Round(math.Sqrt(squares / float64(len(values)-1))))
}

// madToSigma turns a median absolute deviation into an estimate of the standard
// deviation of the same samples.
//
// For a sample of a normal distribution with standard deviation σ, the absolute
// deviation from the median follows a half-normal distribution, so its median is the
// value that half of them stay under:
//
//	P(|X| ≤ q) = 1/2  ⟺  2·Φ(q/σ) − 1 = 1/2  ⟺  q = Φ⁻¹(3/4)·σ ≈ 0.67449·σ
//
// That is, MAD ≈ 0.67449σ, hence σ ≈ MAD / 0.67449 ≈ 1.4826·MAD. Φ⁻¹(3/4) is the 75th
// percentile of the standard normal distribution, 0.6744897501960817, and the reciprocal
// is the constant below.
//
// The scaling is what lets a robust deviation stand where a standard deviation is
// expected, and the retention of the connections needs it to be one: it reads the spread
// as the standard error of an estimate (1.25σ/√n and σ/√(2(n-1))), which are results
// about σ, not about the way it is measured.
const madToSigma = 1.482602218505602

// deviationOf returns how far the values are from their median, in the two ways the
// groups need it: the typical distance, and the average one. The values are reordered.
//
// The sample standard deviation is not used because a single slow check owns it: the
// squared distance to the average lets one sample out of ten move the result by hundreds
// of milliseconds, and it moves the average with it, so the estimate describes the worst
// moment of the window instead of the node. In a run of the url test group, a node whose
// checks were all within ±10ms of its median, save one, was reported with 106ms of
// deviation, 83% of which came from that one check.
//
// Both returned values measure the same distances, and they answer different questions:
//
//   - The median of the distances (scaled to σ) is how much the node usually varies. A
//     value only matters to a median once it crosses the middle of the samples, so it
//     takes half of them to move it: a check that timed out once does not describe the
//     node, and a node that is genuinely unstable moves the median by itself. This is
//     what a group ranks by when it wants the delay a node typically costs.
//
//   - The mean of the distances is how much the node cost in total. Every check counts
//     once, so a rare spike adds its own share instead of being ignored, and a spike that
//     repeats adds more. It is not squared, and the median it is measured from does not
//     move with the spike, so the number stays a statement about the node rather than
//     about one probe. No score is ranked by it: a group breaks a tie with it, and a
//     report shows it as the cost of the whole window.
//
// Neither value is 0 for a node that never varied: RTT reserves 0 for a missing value, so
// a cost of 0 is handled where it is read, see Stats.mixed.
func deviationOf(values []RTT, median RTT) (jitter RTT, meanDeviation RTT) {
	sum := 0
	for i, delay := range values {
		if delay > median {
			values[i] = delay - median
		} else {
			values[i] = median - delay
		}
		sum += int(values[i])
	}
	return RTT(math.Round(madToSigma * float64(medianOf(values)))), RTT(sum / len(values))
}

// Succeeded returns the number of checks that succeeded, the samples the delay
// statistics are computed from.
func (s Stats) Succeeded() int {
	return s.All - s.Fail
}

// Cost returns what an attempt costs on average, which is what a group that ranks a time
// ranks its nodes by: base plus the spread added to it, mixed with the failures.
//
// It is for a base that is a time the node costs. A score that is a share of the node's own
// floor cannot be mixed this way: it is dimensionless, so a failure worth a timeout and a
// value close to zero are not on its scale at all. The idle objective counts a failure as a
// bad check instead. See Stats.Floor and Stats.Bad.
func (s Stats) Cost(base RTT, spread RTT) RTT {
	if s.All == 0 {
		return Failed
	}
	return s.mixed(uint64(base) + uint64(spread))
}

// mixed returns the expected value of cost per attempt: an attempt either succeeds
// with cost, or fails, and the two are mixed by the share of the checks that failed.
// The result never exceeds the timeout.
func (s Stats) mixed(cost uint64) RTT {
	timeout := uint64(failureDelay)
	if cost > timeout {
		// an attempt never costs more than the timeout it waits for
		cost = timeout
	}
	// every check is one attempt, the failed ones are counted by their share
	attempts := uint64(s.All)
	failures := uint64(s.Fail)
	successes := attempts - failures
	result := RTT((successes*cost + failures*s.failureCost(cost)) / attempts)
	if result == Failed && successes > 0 {
		// A cost of zero is a cost like any other, but RTT reserves zero for a missing
		// value: the jitter of a node that does not vary is exactly that, and reporting it
		// as missing would rank the steadiest node behind every measured one.
		return Millisecond
	}
	return result
}

// failureRatio is how many attempts a failed one is worth. It costs more than one that
// worked, since the attempt had to be tried again, but far less than the timeout the
// check waited for: that is an absolute value, which buries a node that failed once
// behind every node that never failed, whatever its delay.
const failureRatio = 3

// failureCost returns what a failed attempt costs, in the scale of the node, the same
// way the tolerance is relative to the score of the best one.
func (s Stats) failureCost(cost uint64) uint64 {
	if s.Succeeded() == 0 {
		// nothing worked, the timeout is all that is known about the node
		return uint64(failureDelay)
	}
	timeout := uint64(failureDelay)
	if cost > timeout/failureRatio {
		return timeout
	}
	return cost * failureRatio
}

// medianOf returns the median of the values, the slice is reordered.
// It represents the typical RTT better than the average, since a single spike
// doesn't move it.
func medianOf(values []RTT) RTT {
	if len(values) == 0 {
		return Failed
	}
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return RTT((int(values[middle-1]) + int(values[middle])) / 2)
}
