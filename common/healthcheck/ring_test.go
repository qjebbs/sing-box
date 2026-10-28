package healthcheck

import (
	"testing"
)

func TestRing(t *testing.T) {
	t.Parallel()
	r := newRing[int](3)
	if r.Length() != 0 || r.Capacity() != 3 {
		t.Fatalf("want an empty ring of 3, got %v of %v", r.Length(), r.Capacity())
	}
	if _, loaded := r.At(0); loaded {
		t.Fatal("an empty ring has no value")
	}
	for i := 1; i <= 2; i++ {
		r.Push(i)
	}
	if r.Length() != 2 {
		t.Fatalf("want 2 values, got %v", r.Length())
	}
	// the values are read back from the most recent
	for distance, want := range []int{2, 1} {
		got, loaded := r.At(distance)
		if !loaded || got != want {
			t.Fatalf("at %v: want %v, got %v (loaded %v)", distance, want, got, loaded)
		}
	}
	if _, loaded := r.At(2); loaded {
		t.Fatal("there is no third value")
	}
	// a value older than the capacity is dropped
	r.Push(3)
	r.Push(4)
	for distance, want := range []int{4, 3, 2} {
		got, _ := r.At(distance)
		if got != want {
			t.Fatalf("at %v: want %v, got %v", distance, want, got)
		}
	}
	if _, loaded := r.At(3); loaded {
		t.Fatal("the ring holds the capacity and no more")
	}
}

func TestRingReplace(t *testing.T) {
	t.Parallel()
	r := newRing[string](2)
	// a replacement before any push is a push
	r.Replace("a")
	if got, _ := r.At(0); got != "a" {
		t.Fatalf("want a, got %v", got)
	}
	r.Push("b")
	r.Replace("c")
	if got, _ := r.At(0); got != "c" {
		t.Fatalf("want c after the replacement, got %v", got)
	}
	if got, _ := r.At(1); got != "a" {
		t.Fatalf("want a before it, got %v", got)
	}
	if r.Length() != 2 {
		t.Fatalf("a replacement adds no value, got %v", r.Length())
	}
}

func BenchmarkRingPush(b *testing.B) {
	r := newRing[int](32)
	for i := 0; i < b.N; i++ {
		r.Push(i)
	}
}
