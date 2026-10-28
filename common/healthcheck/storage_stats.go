package healthcheck

import (
	"math"
	"slices"
	"time"
)

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

	Expires time.Time // time of the statistics expires
}

// Stats get statistics and write cache for next call
// Make sure use Mutex.Lock() before calling it, RWMutex.RLock()
// is not an option since it writes cache.
//
// The delay statistics describe the checks that succeeded only. A failed check waited
// for the timeout, so it says nothing about the delay the node has when it works: a
// score that has to account for the failures reads Fail and All, and mixes the timeout
// in by their own share, see Stats.Mixed. Counting a failure as a delay of the
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
	s.stats = Stats{}
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
	s.stats = newStats(len(delays)+fail, fail, latest.Delay, expiresAt, delays)
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

// Mixed returns the expected value of value per attempt: an attempt either succeeds
// with value, or fails after waiting for failureDelay, so the two are mixed by the
// share of the checks that failed. The result never exceeds failureDelay.
func (s Stats) Mixed(value RTT, failureDelay RTT) RTT {
	if s.All == 0 {
		return Failed
	}
	mixed := (uint64(s.Succeeded())*uint64(value) + uint64(s.Fail)*uint64(failureDelay)) / uint64(s.All)
	if mixed > uint64(failureDelay) {
		return failureDelay
	}
	return RTT(mixed)
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
