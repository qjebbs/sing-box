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
	Bad int
	// FailureRate is how often the node has failed over the last few hours, estimated in
	// wall-clock time and outliving the window, see failureRate. Fail over All describes
	// the checks the sampling read; this describes the node. It is read through
	// FailureShare, which lets the window win while it has something to say.
	FailureRate float32
	Average     RTT // average RTT of all health checks
	Median      RTT // median RTT of all health checks
	Max         RTT // maximum RTT of all health checks
	Min         RTT // minimum RTT of all health checks
	Latest      RTT // latest RTT of all health checks

	// Suspended reports that the node was checked because its connections were failing, and
	// that check failed too, see Storage.Update. It is the one thing here that is not a
	// statistic: the window still describes what the interval measured, and this describes
	// what the traffic the node carried has shown, so it is read next to the statistics
	// rather than mixed into them. It is cleared by the first check that passes.
	Suspended bool

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

// Floors returns the current baseline values by wall-clock slot, with zero for an
// unmeasured slot. It returns nil when no baseline is available.
func (s *Storage) Floors() []uint16 {
	if s == nil {
		return nil
	}
	values, _, filled := s.baseline.export(now())
	if !filled {
		return nil
	}
	return values
}

func (s *Storage) refreshStats(now time.Time) {
	latest, loaded := s.history.At(0)
	if !loaded || now.Sub(latest.Time) > s.options.Validity {
		floor, _ := s.baseline.RTT(now)
		s.stats = Stats{
			Floor:       floor,
			Suspended:   s.suspended,
			FailureRate: float32(s.failures.rate()),
		}
		return
	}
	delays := make([]RTT, 0, s.history.Length())
	var expiresAt time.Time
	fail := 0
	for i := 0; i < s.history.Length() && uint(i) < s.options.Sampling; i++ {
		// from latest to oldest, up to the sampling the statistics read
		history, _ := s.history.At(i)
		itemExpiresAt := history.Time.Add(s.options.Validity)
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
	var floor RTT
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
	}
	// The floor outlives the window, so it is read even when the window is too short to set
	// one: a restored floor is what a restart must not wait for.
	floor, _ = s.baseline.RTT(now)
	s.stats = statsOf(len(delays)+fail, fail, latest.Delay, expiresAt, delays, floor, s.failures.rate())
	s.stats.Suspended = s.suspended
}

// StatsOf returns the statistics of the given checks, most recent first, as of expires:
// the computation Storage.Stats does over its window, so a window of the history can be
// priced exactly the way the group priced it.
//
// The baseline is the one input of the statistics the storage keeps outside the window, so
// it is passed in. A floor of zero means the node has none yet, and Bad stays zero.
//
// longRun is the failure estimate of the checks older than this window, see
// ReplayFailureRate: it is the one statistic a window does not carry, and passing it in
// keeps a window measured here priced the way the group priced it.
func StatsOf(checks []*History, floor RTT, expires time.Time, longRun float64) Stats {
	if len(checks) == 0 {
		return Stats{}
	}
	delays := make([]RTT, 0, len(checks))
	fail := 0
	for _, check := range checks {
		if check.Delay == Failed {
			fail++
		} else {
			delays = append(delays, check.Delay)
		}
	}
	return statsOf(len(checks), fail, checks[0].Delay, expires, delays, floor, longRun)
}

// statsOf builds the statistics of a window of checks from the delays that succeeded.
func statsOf(all int, fail int, latest RTT, expires time.Time, delays []RTT, floor RTT, longRun float64) Stats {
	var bad int
	if floor > 0 {
		// Counted before newStats, which replaces the delays with their distances to the
		// median, see deviationOf.
		bad = countAbove(delays, floor*badCheckMultiple)
	}
	stats := newStats(all, fail, latest, expires, delays)
	stats.Floor, stats.Bad = floor, bad
	stats.FailureRate = float32(longRun)
	return stats
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

// FailureShare returns the share of the attempts that failed: the window's own share, or
// the long-run estimate, whichever is larger.
//
// The window is what the node is doing now and the estimate is what it does when the window
// happens to be clean, so the larger of the two is the cautious reading of the same
// question. With no window at all there is nothing to weigh the estimate against, and the
// estimate stands alone.
func (s Stats) FailureShare() float64 {
	share := float64(s.FailureRate)
	if s.All > 0 {
		if window := float64(s.Fail) / float64(s.All); window > share {
			share = window
		}
	}
	return math.Min(1, share)
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
// with cost, or fails, and the two are mixed by the share of the attempts that failed, see
// FailureShare. The result never exceeds the timeout.
func (s Stats) mixed(cost uint64) RTT {
	timeout := uint64(failureDelay)
	if cost > timeout {
		// an attempt never costs more than the timeout it waits for
		cost = timeout
	}
	// every check is one attempt; the window's failures are counted by their share, and
	// the node's own record stands in when the window holds none
	share := s.FailureShare()
	result := RTT((1-share)*float64(cost) + share*float64(s.failureCost(cost)))
	if result == Failed && share < 1 {
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
