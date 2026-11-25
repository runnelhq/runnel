// Package runid creates the ids of runs (workflows and pipelines).
package runid

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// crockford is the alphabet of Crockford's Base32, used by ULIDs.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu   sync.Mutex
	last [16]byte // the last id, to keep ids monotonic
)

// New returns a ULID (https://github.com/ulid/spec): 26 characters,
// sortable by creation time, e.g. "01JD3Z8K2Q4M7V1C9XH0RTW5AB". Ids are monotonic in
// the process: an id from the same millisecond as the last one (or from an earlier one,
// if the clock went back) is that id plus one, so that runs sort in the order they
// started (the history lists them by id).
func New(now time.Time) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(now.UnixMilli())<<16)
	_, _ = rand.Read(b[6:]) // never fails
	mu.Lock()
	if bytes.Equal(b[:6], last[:6]) {
		b = last
		for i := 15; i >= 6; i-- { // plus one (80 random bits: no overflow in practice)
			b[i]++
			if b[i] != 0 {
				break
			}
		}
	}
	last = b
	mu.Unlock()
	return encode(b)
}

// encode writes 128 bits as 26 base32 characters (the first character uses only 3 bits).
func encode(b [16]byte) string {
	hi := binary.BigEndian.Uint64(b[:8])
	lo := binary.BigEndian.Uint64(b[8:])
	var out [26]byte
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}
