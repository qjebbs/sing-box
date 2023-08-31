package dialer_test

import (
	"fmt"
	"testing"
)

func BenchmarkMap(b *testing.B) {
	b.ReportAllocs()

	outs := makeSlice()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rdr := makeMap(outs)
		_ = rdr["1"]
	}
}

func BenchmarkMapCached(b *testing.B) {
	b.ReportAllocs()

	outs := makeSlice()
	rdr := makeMap(outs)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rdr["1"]
	}
}

func BenchmarkSlice(b *testing.B) {
	b.ReportAllocs()

	outs := makeSlice()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chain := make([]*fakeOutbound, len(outs)+1)
		copy(chain, outs)
		chain[len(outs)] = nil
		_ = outs[0].tag == "1"
	}
}

func BenchmarkSliceCached(b *testing.B) {
	b.ReportAllocs()

	outs := makeSlice()
	chain := make([]*fakeOutbound, len(outs)+1)
	copy(chain, outs)
	chain[len(outs)] = nil
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = outs[0].tag == "1"
	}
}

func makeMap(s []*fakeOutbound) map[string]*fakeOutbound {
	m := make(map[string]*fakeOutbound, len(s)+1)
	for _, o := range s {
		m[o.tag] = o
	}
	m["a"] = nil
	return m
}

func makeSlice() []*fakeOutbound {
	n := 10
	s := make([]*fakeOutbound, n)
	for i := 0; i < n; i++ {
		s[i] = &fakeOutbound{tag: fmt.Sprint(i)}
	}
	return s
}

type fakeOutbound struct {
	tag string
}
