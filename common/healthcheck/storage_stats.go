package healthcheck

import (
	"math"
	"slices"
	"time"

	C "github.com/sagernet/sing-box/constant"
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
// is not an option since it writes cache
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
	s.stats.Latest = latest.Delay
	failureDelay := s.FailureDelay
	if failureDelay == Failed {
		failureDelay = RTTOf(C.TCPTimeout)
	}
	// A failed check is not dropped from the distribution, it is counted with the
	// delay it would have had. Otherwise a node failing most of its checks would be
	// described by the few successful ones, and its stability would look excellent.
	delays := make([]RTT, 0, s.cap)
	var expiresAt time.Time
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
			s.stats.Fail++
			delay = failureDelay
		}
		delays = append(delays, delay)
	}

	s.stats.Expires = expiresAt
	s.stats.All = len(delays)
	if s.stats.All == 0 {
		return
	}
	// the values are kept in milliseconds, sum them as int to not overflow
	sum := 0
	min := RTT(math.MaxUint16)
	for _, delay := range delays {
		sum += int(delay)
		if min > delay {
			min = delay
		}
		if s.stats.Max < delay {
			s.stats.Max = delay
		}
	}
	s.stats.Average = RTT(sum / len(delays))
	s.stats.Median = medianOf(delays)
	s.stats.Min = min
	if len(delays) < 2 {
		// no enough data for standard deviation, we assume it's half of the average rtt
		// if we don't do this, standard deviation of 1 round tested nodes is 0, will always
		// selected before 2 or more rounds tested nodes
		s.stats.Deviation = s.stats.Average / 2
		return
	}
	variance := float64(0)
	for _, delay := range delays {
		variance += math.Pow(float64(delay)-float64(s.stats.Average), 2)
	}
	s.stats.Deviation = RTT(math.Sqrt(variance / float64(len(delays))))
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
