package healthcheck

import "time"

const (
	// baselineSlot is how much of the day one slot of the baseline covers. The slots are
	// aligned to the wall clock, so a slot always covers the same part of the day, and
	// what it holds is the fastest the node has been at that time of day.
	baselineSlot = 30 * time.Minute
	// baselineSlots is how many slots a day has. A day of 24 hours is 48 slots of 30
	// minutes, so a baseline covers a whole day at every time of it, and a value older
	// than that is overwritten by the next measurement of the same slot.
	//
	// It is a constant rather than an option: it is the resolution of the memory, and
	// changing it trades a longer memory for fewer samples per slot, which only makes
	// sense together with the check interval. It also keeps the score comparable between
	// two nodes of the same group, which an option would not.
	baselineSlots = 24 * 60 / 30
)

// baseline is the fastest a node has been, kept per time of day: the day is cut into
// baselineSlots windows aligned to the wall clock, each one holds the fastest delay seen
// in it, and the baseline is the fastest of them.
//
// It is not a single minimum with an expiry date. A minimum only ever goes down, so the
// day it was measured on would keep describing the node until it expired, and the expiry
// would be a single step that throws away a whole day at once — a node that changes a
// little every day would then be read as steady until the moment it is read as changed.
// Rotating one slot at a time keeps a whole day of evidence at every moment, and follows
// a node that drifts by one slot of change per rotation.
//
// It is not thread safe, like the storage that holds it.
type baseline struct {
	values [baselineSlots]RTT
	// written is when the value of the slot was last written. It tells the slot of the
	// day the value belongs to, and whether it is still within the day it describes.
	written [baselineSlots]time.Time
}

// RTT returns the baseline at now: the smallest value of the slots still within a day of
// now, and whether there is one. The round is written before it is read, so a round is its
// own reference as soon as it holds the fastest delay: a minimum over real delays can never
// be above the median of the round that set it, and never makes a score of zero. See
// Storage.refreshStats.
func (b *baseline) RTT(now time.Time) (RTT, bool) {
	var (
		best  RTT
		found bool
	)
	for i := range b.values {
		if b.written[i].IsZero() || now.Sub(b.written[i]) >= baselineSlot*baselineSlots {
			continue
		}
		if !found || b.values[i] < best {
			best = b.values[i]
			found = true
		}
	}
	return best, found
}

// Put writes the fastest delay of a round into the slot of the time of day it was measured
// at, keeping the smallest value of the slot: a slot holds the fastest the node has been
// at that time of day, not the latest. A slot that was written a whole slot ago is a slot
// of the previous day, and starts over from this round.
func (b *baseline) Put(now time.Time, fastest RTT) {
	index := baselineSlotOf(now)
	if b.written[index].IsZero() || now.Sub(b.written[index]) >= baselineSlot || fastest < b.values[index] {
		b.values[index] = fastest
	}
	b.written[index] = now
}

// baselineSlotOf returns the slot of the day the time belongs to.
func baselineSlotOf(now time.Time) int {
	slot := int64(baselineSlot / time.Second)
	return int((now.Unix() / slot) % baselineSlots)
}
