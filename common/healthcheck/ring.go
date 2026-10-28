package healthcheck

// ring is a buffer of fixed capacity, written in order and read back from the most
// recent value. Once allocated it never allocates again: a value older than the capacity
// is dropped, so the oldest values are the ones that disappear first.
//
// It is not thread safe, like the storage that holds it.
type ring[T any] struct {
	values []T
	// idx is the slot of the most recent value.
	idx int
	// length is how many values were written, up to the capacity.
	length int
}

func newRing[T any](capacity int) *ring[T] {
	return &ring[T]{values: make([]T, capacity)}
}

// Push writes a value after the most recent one, dropping the oldest when the ring is
// full.
func (r *ring[T]) Push(value T) {
	if len(r.values) == 0 {
		return
	}
	r.idx = (r.idx + 1) % len(r.values)
	r.values[r.idx] = value
	if r.length < len(r.values) {
		r.length++
	}
}

// Replace overwrites the most recent value, or pushes it when there is none. It is what
// a check does when its result is corrected without adding a sample.
func (r *ring[T]) Replace(value T) {
	if r.length == 0 {
		r.Push(value)
		return
	}
	r.values[r.idx] = value
}

// Length returns how many values are held, up to the capacity.
func (r *ring[T]) Length() int {
	return r.length
}

// Capacity returns how many values can be held at once.
func (r *ring[T]) Capacity() int {
	return len(r.values)
}

// At returns the value written distance pushes ago, 0 being the most recent. It reports
// false when fewer values were written.
func (r *ring[T]) At(distance int) (T, bool) {
	if distance < 0 || distance >= r.length {
		var zero T
		return zero, false
	}
	idx := r.idx - distance
	if idx < 0 {
		idx += len(r.values)
	}
	return r.values[idx], true
}
