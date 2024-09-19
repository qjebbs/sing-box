package ping

import (
	"time"
)

// Statistics is the ping statistics
type Statistics struct {
	StartAt time.Time
	RTTs    []uint16

	Requests uint
	Fails    uint

	Max     uint
	Min     uint
	Average uint
}

// CalStats calculates ping statistics
func getStatistics(start time.Time, requests uint, rtts []uint16) *Statistics {
	var sum uint
	s := &Statistics{
		StartAt:  start,
		Requests: requests,
		Fails:    requests - uint(len(rtts)),
		RTTs:     rtts,
	}
	for _, v := range rtts {
		sum += uint(v)
		if s.Max == 0 || s.Min == 0 {
			s.Max = uint(v)
			s.Min = uint(v)
		}
		if uv := uint(v); uv > s.Max {
			s.Max = uv
		}
		if uv := uint(v); uv < s.Min {
			s.Min = uv
		}
	}
	if len(s.RTTs) > 0 {
		s.Average = uint(float64(sum) / float64(len(s.RTTs)))
	}
	return s
}
