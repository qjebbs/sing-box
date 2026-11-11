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
	// written is the start of the window the value of the slot was measured in. It tells the
	// slot of the day the value belongs to, and whether it is still within the day it
	// describes.
	//
	// It is the window rather than the moment of the round that wrote it, because the windows
	// are cut from the wall clock: the start of a window is the same for every run, so the
	// whole array is placed by one moment, see export and restore. A round only ever writes
	// the window it is in, so a value of another window is a value of another day.
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
// at that time of day, not the latest. A value that came from another window of the day is
// a value of the previous day, and starts over from this round.
func (b *baseline) Put(now time.Time, fastest RTT) {
	index := baselineSlotOf(now)
	window := baselineWindowStart(now)
	if b.written[index].IsZero() || !b.written[index].Equal(window) || fastest < b.values[index] {
		b.values[index] = fastest
	}
	b.written[index] = window
}

// baselineSlotOf returns the slot of the day the time belongs to.
func baselineSlotOf(now time.Time) int {
	slot := int64(baselineSlot / time.Second)
	return int((now.Unix() / slot) % baselineSlots)
}

// baselineWindowStart returns the start of the window of the day the time belongs to.
func baselineWindowStart(now time.Time) time.Time {
	slot := int64(baselineSlot / time.Second)
	return time.Unix(now.Unix()/slot*slot, 0)
}

// dayStartOf returns the midnight of the day of at, in the time zone of at.
func dayStartOf(at time.Time) time.Time {
	year, month, day := at.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, at.Location())
}

// export returns the values of the slots still within a day of now, the window of the day they
// were read in, and whether there is one of them. A slot that holds nothing is zero, which no
// delay is.
//
// The window is returned as its start rather than as now, which is what places the values: a
// value is read as the one of the window n windows before it, so a moment in the middle of a
// window would carry the same information in a form that invites arithmetic on a window edge.
// The start is what a slot of that day is written as, see Put, so the two meet exactly.
//
// A slot of the previous day is left out, which is what a window the node was not measured in
// becomes once a day has passed: an idle night goes by without the slot being overwritten, and
// the value is neither read nor stored again, so nothing has to clear it. It is overwritten by
// the next round of its own window, whenever that comes.
func (b *baseline) export(now time.Time) (values []uint16, window time.Time, filled bool) {
	window = baselineWindowStart(now)
	values = make([]uint16, baselineSlots)
	for i := range b.values {
		if b.written[i].IsZero() || now.Sub(b.written[i]) >= baselineSlot*baselineSlots {
			continue
		}
		values[i] = uint16(b.values[i])
		filled = true
	}
	return values, window, filled
}

// restore puts the slots back into the windows they were measured in. The slot of a value is
// its position, and the window of that slot is the one n windows before at, which is the start
// of the window the values were read in: the value a slot holds is the fastest of the whole
// window, so the window it came from is all the moment it needs.
//
// The age of a value comes back with it, so the record keeps the life it had: it is dropped
// at the moment it would have been without a restart, and the slot is reset by the same Put
// that would have reset it. A slot this run has measured itself is newer than what was
// stored, and is kept.
//
// It reports false when the values are not one per slot of the day, and when every slot of the
// record is older than what this run holds: reading the same record again is then nothing.
func (b *baseline) restore(values []uint16, at time.Time) bool {
	if len(values) != baselineSlots || at.IsZero() {
		return false
	}
	// a moment inside a window names the same window as its start, so a record written by a
	// build that kept the moment rather than the start is still placed the same way
	window := baselineWindowStart(at)
	current := baselineSlotOf(at)
	restored := false
	for index, value := range values {
		if value == 0 {
			continue
		}
		back := (current - index + baselineSlots) % baselineSlots
		written := window.Add(-time.Duration(back) * baselineSlot)
		if !b.written[index].IsZero() && !written.After(b.written[index]) {
			continue
		}
		b.values[index] = RTT(value)
		b.written[index] = written
		restored = true
	}
	return restored
}
