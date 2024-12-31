package healthcheck

import (
	"strconv"
	"time"
)

// RTT constant values
const (
	// Failed is a special value to indicate a failed check
	// or unable to get a valid result, e.g.: average RTT
	// will be Fail(0) if a node is never tested.
	Failed RTT = 0

	Millisecond RTT = 1
	Second      RTT = 1000
)

// RTT is the round trip time with underlying type uint16, precision is millisecond
type RTT uint16

func (r RTT) String() string {
	if r > 1000 {
		return strconv.FormatFloat(float64(r)/1000, 'f', 2, 64) + "s"
	}
	return strconv.FormatUint(uint64(r), 10) + "ms"
}

// TimeDuration converts a rtt.Duration to time.Duration
func (r RTT) TimeDuration() time.Duration {
	return time.Duration(r) * time.Millisecond
}

type preciseDuration interface {
	~int64
}

// RTTOf converts a precise duration (underlying type int64, like `time.Duration`) to `rtt.Duration`
func RTTOf[T preciseDuration](d T) RTT {
	return RTT(d / T(time.Millisecond))
}

// RTTsOf converts precise durations (underlying type int64, like `[]time.Duration`) to `[]rtt.Duration`
func RTTsOf[T preciseDuration](values []T) []RTT {
	durations := make([]RTT, len(values))
	for i, value := range values {
		durations[i] = RTTOf(value)
	}
	return durations
}
