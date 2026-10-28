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
	All       int // total number of health checks
	Fail      int // number of failed health checks
	Deviation RTT // standard deviation of RTTs
	Average   RTT // average RTT of all health checks
	Median    RTT // median RTT of all health checks
	Max       RTT // maximum RTT of all health checks
	Min       RTT // minimum RTT of all health checks
	Latest    RTT // latest RTT of all health checks
	// Connections is the number of connections the group carried, and
	// ConnectionFailures how many of them failed, see Storage.ReportConnection. They are
	// attempts as well, so they are mixed into the cost next to the checks, but they are
	// not samples: a connection is not a probe on a schedule, and there is no delay to
	// learn from it.
	//
	// Both outcomes are counted, so the share of them that failed doesn't grow with the
	// traffic of the node: a busy node is not scored worse than an idle one.
	Connections        int
	ConnectionFailures int

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
	// the connections outlive the checks they are counted next to, a node can drop the
	// traffic it carries without ever being checked
	connections, connectionFailures := s.ConnectionOutcomes(now)
	s.stats = Stats{
		Connections:        connections,
		ConnectionFailures: connectionFailures,
	}
	latest := s.history[s.idx]
	if now.Sub(latest.Time) > s.validity {
		return
	}
	delays := make([]RTT, 0, s.cap)
	var expiresAt time.Time
	fail := 0
	for i := 0; i < s.cap; i++ {
		// from latest to oldest
		idx := s.offset(-i)
		itemExpiresAt := s.history[idx].Time.Add(s.validity)
		if itemExpiresAt.Before(now) {
			// the latter is invalid, so are the formers
			break
		}
		// the time when the oldest item expires
		expiresAt = itemExpiresAt
		delay := s.history[idx].Delay
		if delay == Failed {
			fail++
		} else {
			delays = append(delays, delay)
		}
	}
	stats := newStats(len(delays)+fail, fail, latest.Delay, expiresAt, delays)
	stats.Connections = connections
	stats.ConnectionFailures = connectionFailures
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
		// no enough data for standard deviation, we assume it's half of the average rtt
		// if we don't do this, standard deviation of 1 round tested nodes is 0, will always
		// selected before 2 or more rounds tested nodes
		stats.Deviation = stats.Average / 2
		return stats
	}
	variance := float64(0)
	for _, delay := range values {
		variance += math.Pow(float64(delay)-float64(stats.Average), 2)
	}
	stats.Deviation = RTT(math.Sqrt(variance / float64(len(values))))
	return stats
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
// their deviation, scaled by whatever weight the group gives the jitter.
func (s Stats) Cost(base RTT, jitter RTT, jitterWeight float32) RTT {
	if s.All == 0 && s.Connections == 0 {
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
	// the connections are attempts too, only the probes have a delay: a connection that
	// was established counts like a probe that succeeded
	attempts := uint64(s.All + s.Connections)
	failures := uint64(s.Fail + s.ConnectionFailures)
	successes := attempts - failures
	return RTT((successes*cost + failures*s.failureCost(cost)) / attempts)
}

// failureRatio is how many attempts a failed one is worth. It costs more than one that
// worked, since the connection it carried had to be tried again, but far less than the
// timeout the check waited for: that is an absolute value, which buries a node that
// failed once behind every node that never failed, whatever its delay.
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
