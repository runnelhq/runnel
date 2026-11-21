package triggers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/model"
)

// FuzzRequest checks that arbitrary requests from the network never make the parts of
// the HTTP triggers that read them panic: the path (routing), the query and the body
// (JSON object or list, form).
func FuzzRequest(f *testing.F) {
	f.Add("/api/customers/4711", "x=1&y=2", "application/json", `{"id": 4711, "tags": ["a"], "n": 1.5}`, true)
	f.Add("/greet/Moin", "", "application/x-www-form-urlencoded", "GREETING=Hi&x=%ZZ", false)
	f.Add("//a/../b/", "a=%", "text/plain; charset=x", "[1, {}]", true)
	segments := []model.Segment{{Text: "api"}, {Text: "customers"}, {Text: "id", Placeholder: true}}
	f.Fuzz(func(_ *testing.T, path, query, contentType, body string, list bool) {
		u, err := url.Parse("http://127.0.0.1:7301" + path + "?" + query)
		if err != nil {
			return
		}
		r := &http.Request{Method: "POST", URL: u, Header: http.Header{"Content-Type": {contentType}}, Host: u.Host}
		_, _, _, _ = requestValues(r, []byte(body), list)
		_, _ = match(segments, strings.Split(strings.Trim(u.Path, "/"), "/"))
		_ = requestOf(r, nil, "127.0.0.1")
	})
}
