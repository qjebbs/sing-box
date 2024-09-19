package healthcheck

import (
	"math"
	"time"
)

// Stats is the statistics of RTTs
type Stats struct {
	All       int // total number of health checks
	Fail      int // number of failed health checks
	Deviation RTT // standard deviation of RTTs
	Average   RTT // average RTT of all health checks
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
	min := RTT(math.MaxUint16)
	sum := RTT(0)
	cnt := 0
	validRTTs := make([]RTT, 0, s.cap)
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
		if s.history[idx].Delay == Failed {
			s.stats.Fail++
			continue
		}
		cnt++
		sum += s.history[idx].Delay
		validRTTs = append(validRTTs, s.history[idx].Delay)
		if s.stats.Max < s.history[idx].Delay {
			s.stats.Max = s.history[idx].Delay
		}
		if min > s.history[idx].Delay {
			min = s.history[idx].Delay
		}
	}

	s.stats.Expires = expiresAt
	s.stats.All = cnt + s.stats.Fail
	if cnt > 0 {
		s.stats.Average = RTT(int(sum) / cnt)
	}
	if s.stats.All == 0 || s.stats.Fail == s.stats.All {
		return
	}
	s.stats.Min = min
	var std float64
	if cnt < 2 {
		// no enough data for standard deviation, we assume it's half of the average rtt
		// if we don't do this, standard deviation of 1 round tested nodes is 0, will always
		// selected before 2 or more rounds tested nodes
		std = float64(s.stats.Average / 2)
	} else {
		variance := float64(0)
		for _, rtt := range validRTTs {
			variance += math.Pow(float64(rtt)-float64(s.stats.Average), 2)
		}
		std = math.Sqrt(variance / float64(cnt))
	}
	s.stats.Deviation = RTT(std)
}
