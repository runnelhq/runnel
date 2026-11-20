// Package runid creates the ids of runs (workflows and pipelines).
package runid

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// crockford is the alphabet of Crockford's Base32, used by ULIDs.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New returns a ULID (https://github.com/ulid/spec): 26 characters,
// sortable by creation time, e.g. "01JD3Z8K2Q4M7V1C9XH0RTW5AB".
func New(now time.Time) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(now.UnixMilli())<<16)
	_, _ = rand.Read(b[6:]) // never fails
	// 128 bits → 26 base32 characters (first character uses only 3 bits).
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
