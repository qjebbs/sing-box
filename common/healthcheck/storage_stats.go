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

// Stats is the statistics of RTTs
type Stats struct {
	All     int // total number of health checks
	Fail    int // number of failed health checks
	Jitter  RTT // jitter of RTTs, a robust estimate of their spread, see jitterOf
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
	stats := newStats(len(delays)+fail, fail, latest.Delay, expiresAt, delays)
	s.stats = stats
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
		// no enough data for a jitter, we assume it's half of the average rtt
		// if we don't do this, jitter of 1 round tested nodes is 0, will always
		// selected before 2 or more rounds tested nodes
		stats.Jitter = stats.Average / 2
		return stats
	}
	stats.Jitter = jitterOf(values, stats.Median)
	return stats
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
// expected, and nothing downstream has to know how it was estimated: the score of the
// objectives reads it as the std term (cost = MED + weight·σ), and the retention reads it
// as the standard error of an estimate (1.25σ/√n and σ/√(2(n-1))), both of which are
// results about σ, not about the way it is measured.
const madToSigma = 1.482602218505602

// jitterOf returns the jitter of the values, an estimate of the standard deviation
// around their median: the median absolute deviation, scaled to σ. The values are
// reordered.
//
// The sample standard deviation is not used because a single slow check owns it: the
// squared distance to the average lets one sample out of ten move the result by hundreds
// of milliseconds, and it moves the average with it, so the estimate describes the worst
// moment of the window instead of the node. In a run of the url test group, a node whose
// checks were all within ±10ms of its median, save one, was reported with 106ms of
// jitter, 88% of which came from that one check.
//
// The median absolute deviation is bounded by the middle of the samples instead: a value
// only matters once it crosses the median, so it takes half of the samples to move the
// estimate anywhere. A check that timed out once no longer ranks the node for the whole
// window, while a node that is genuinely unstable moves the median of the deviations by
// itself.
//
// The estimate is 0 when every sample is the same, which the delay keeps meaning: RTT
// reserves 0 for a missing value, so a cost of 0 is handled where it is read, see
// Stats.mixed.
func jitterOf(values []RTT, median RTT) RTT {
	for i, delay := range values {
		if delay > median {
			values[i] = delay - median
		} else {
			values[i] = median - delay
		}
	}
	return RTT(math.Round(madToSigma * float64(medianOf(values))))
}

// Succeeded returns the number of checks that succeeded, the samples the delay
// statistics are computed from.
func (s Stats) Succeeded() int {
	return s.All - s.Fail
}

// Cost returns what an attempt costs on average, which is what the groups rank their
// nodes by: base plus the jitter added to it, mixed with the failures.
//
// The caller chooses the metric: the median delay of the checks that succeeded, or
// their jitter, scaled by whatever weight the group gives it.
func (s Stats) Cost(base RTT, jitter RTT, jitterWeight float32) RTT {
	if s.All == 0 {
		return Failed
	}
	cost := uint64(base)
	if jitterWeight > 0 {
		// round instead of truncate, so the weight never loses a millisecond
		cost += uint64(float32(jitter)*jitterWeight + 0.5)
	}
	return s.mixed(cost)
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
		// value: the jitter of a steady node is exactly that, and reporting it as
		// missing would rank the steadiest node behind every measured one.
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
