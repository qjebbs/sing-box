package healthcheck

import (
	"testing"
	"time"
)

// lastWindowOf returns the most recent start of the window of the day that index names, at or
// before now. It walks the last day a window at a time rather than working the index out, so
// the test does not assume the way the baseline places a slot.
func lastWindowOf(now time.Time, index int) time.Time {
	for back := 0; back < baselineSlots; back++ {
		at := baselineWindowStart(now).Add(-time.Duration(back) * baselineSlot)
		if baselineSlotOf(at) == index {
			return at
		}
	}
	return time.Time{}
}

func TestBaselineLeavesOutTheSlotsOfThePreviousDay(t *testing.T) {
	t.Parallel()
	// A window the node was not measured in — an idle night, a check suspended for a while — is
	// not overwritten and not cleared. It simply stops being a slot of the day: it is not read,
	// and it is not stored either, so a restart cannot bring back a value that describes a day
	// the node no longer is.
	now := time.Now()
	var b baseline
	b.Put(now.Add(-23*time.Hour), 90)
	b.Put(now.Add(-25*time.Hour), 80)
	if floor, ok := b.RTT(now); !ok || floor != 90 {
		t.Fatalf("want the floor of the slot within the day, 90, got %d (%v)", floor, ok)
	}
	values, _, filled := b.export(now)
	if !filled {
		t.Fatal("want the slot within the day to export")
	}
	kept := baselineSlotOf(now.Add(-23 * time.Hour))
	for index, value := range values {
		if index != kept && value != 0 {
			t.Fatalf("want only the slot within the day stored, got slot %d = %d", index, value)
		}
	}
	if values[kept] != 90 {
		t.Fatalf("want 90 in the slot within the day, got %d", values[kept])
	}

	// the stale value is still in memory, and the next round of its own window replaces it
	b.Put(now.Add(-25*time.Hour+24*time.Hour), 70)
	if got := b.values[baselineSlotOf(now.Add(-time.Hour))]; got != 70 {
		t.Fatalf("want the window to start over from 70, got %d", got)
	}
}

func TestBaselineRestoresEverySlotIntoItsWindow(t *testing.T) {
	t.Parallel()
	// A day of slots is what a restart restores, not only the one holding the floor: the rest
	// of the day is a fallback the run would otherwise have to measure again, and until it has,
	// it reads a floor that is too high. One moment places all of them, because the position of
	// a value fixes the window of the day it came from.
	now := time.Now()
	var original baseline
	for index := 0; index < baselineSlots; index++ {
		original.Put(lastWindowOf(now, index).Add(baselineSlot/2), RTT(100+index))
	}
	values, window, filled := original.export(now)
	if !filled {
		t.Fatal("want a day of slots to export")
	}
	// one window places the whole day, and it is a window edge rather than the moment of the
	// round, so nothing downstream has to round a moment into a window again
	if !window.Equal(baselineWindowStart(now)) {
		t.Fatalf("want the start of the window, %v, got %v", baselineWindowStart(now), window)
	}
	var restored baseline
	if !restored.restore(values, window) {
		t.Fatal("want the slots back")
	}
	for index := range original.values {
		if restored.values[index] != original.values[index] {
			t.Fatalf("slot %d: want %d, got %d", index, original.values[index], restored.values[index])
		}
		if !restored.written[index].Equal(original.written[index]) {
			t.Fatalf("slot %d: want the window of %v, got %v", index, original.written[index], restored.written[index])
		}
	}
	// the floor the two hold is the same at every moment of the day that follows, not only at
	// the moment the record was written
	for offset := 0; offset < baselineSlots; offset++ {
		at := now.Add(time.Duration(offset) * baselineSlot)
		want, wantFilled := original.RTT(at)
		got, gotFilled := restored.RTT(at)
		if gotFilled != wantFilled || got != want {
			t.Fatalf("%v later: want floor %d (%v), got %d (%v)", time.Duration(offset)*baselineSlot, want, wantFilled, got, gotFilled)
		}
	}
}

func TestBaselineRestoreLeavesTheExpiryWhereItWas(t *testing.T) {
	t.Parallel()
	// The window a value came from is all the age it needs: a slot has to be dropped at the
	// moment it would have been without a restart, and the next measurement of the same window
	// has to reset it, not merge into it.
	now := time.Now()
	var original baseline
	measuredWindow := lastWindowOf(now, baselineSlotOf(now))
	original.Put(measuredWindow.Add(time.Minute), 150)
	values, window, filled := original.export(now)
	if !filled {
		t.Fatal("want the slot to export")
	}
	var restored baseline
	restored.restore(values, window)

	// a day after the window it was measured in, the slot is gone
	expired := measuredWindow.Add(baselineSlot * baselineSlots)
	if _, ok := restored.RTT(expired); ok {
		t.Fatal("want the restored slot to expire a day after its window")
	}
	// and the round that opens the same window again measures the slot afresh
	restored.Put(expired.Add(time.Minute), 400)
	if got := restored.values[baselineSlotOf(expired)]; got != 400 {
		t.Fatalf("want the new window to start over from 400, got %d", got)
	}
}

func TestBaselineRestoreRejectsValuesThatAreNotADay(t *testing.T) {
	t.Parallel()
	// A state this run cannot place is one it must not guess at: a different number of slots is
	// another shape of the day, not a shorter one.
	var restored baseline
	if restored.restore(make([]uint16, baselineSlots-1), time.Now()) {
		t.Fatal("want a day of slots and nothing else")
	}
	if restored.restore(make([]uint16, baselineSlots), time.Time{}) {
		t.Fatal("want a record with the moment it was read")
	}
	if _, ok := restored.RTT(time.Now()); ok {
		t.Fatal("want an empty baseline")
	}
}
