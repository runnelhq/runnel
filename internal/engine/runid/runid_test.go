package runid

import (
	"strings"
	"testing"
	"time"
)

// Ids sort in the order they were made, also within one millisecond; the first ten
// characters are the time given.
func TestMonotonic(t *testing.T) {
	now := time.Date(2026, 11, 25, 13, 58, 27, 703_000_000, time.UTC)
	prev := New(now)
	if len(prev) != 26 || strings.Trim(prev, crockford) != "" {
		t.Fatalf("id %q", prev)
	}
	for i := range 1000 {
		id := New(now)
		if id <= prev || id[:10] != prev[:10] {
			t.Fatalf("%d: %s after %s", i, id, prev)
		}
		prev = id
	}
	if later := New(now.Add(time.Millisecond)); later <= prev || later[:10] == prev[:10] {
		t.Errorf("later %s, prev %s", later, prev)
	}
	if earlier := New(now.Add(-time.Second)); earlier[:10] >= prev[:10] { // the clock went back: its own time
		t.Errorf("earlier %s, prev %s", earlier, prev)
	}
}

// encode carries over all 128 bits: the largest value is "7ZZ…Z".
func TestEncode(t *testing.T) {
	var b [16]byte
	for i := range b {
		b[i] = 0xff
	}
	if got := encode(b); got != "7"+strings.Repeat("Z", 25) {
		t.Errorf("%s", got)
	}
}

func BenchmarkNew(b *testing.B) {
	now := time.Now()
	for b.Loop() {
		New(now)
	}
}
