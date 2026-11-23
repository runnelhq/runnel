package pagination

import (
	"net/http"
	"strings"
	"testing"
)

// FuzzNextLink reads Link headers of answers (network input): no panic, and the next
// address is always a part of the header.
func FuzzNextLink(f *testing.F) {
	for _, seed := range []string{
		`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=9>; rel="last"`,
		`<a,b>; rel=next`, `<>; rel="next"`, `<x>`, `;;,,<<>>`, `<x>; rel="prev next"; title="a,b"`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, h string) {
		got := nextLink(http.Header{"Link": {h}})
		if got != "" && !strings.Contains(h, got) {
			t.Errorf("%q is not part of %q", got, h)
		}
	})
}
