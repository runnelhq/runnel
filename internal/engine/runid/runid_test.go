package runid

import (
	"strings"
	"testing"
	"time"
)

// Ids sort in the order they were made, also within one millisecond and when the clock
// goes back; the time stays readable in the first ten characters.
func TestMonotonic(t *testing.T) {
	now := time.Date(2026, 11, 25, 13, 58, 27, 703_000_000, time.UTC)
	prev := New(now)
	if len(prev) != 26 || strings.Trim(prev, crockford) != "" {
		t.Fatalf("id %q", prev)
	}
	for i := range 1000 {
		at := now
		if i%10 == 9 {
			at = now.Add(-time.Second) // clock went back
		}
		id := New(at)
		if id <= prev {
			t.Fatalf("%d: %s after %s", i, id, prev)
		}
		prev = id
	}
	later := New(now.Add(time.Hour))
	if later <= prev || later[:10] != New(now.Add(time.Hour))[:10] || later[:10] == prev[:10] {
		t.Errorf("later %s, prev %s", later, prev)
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
