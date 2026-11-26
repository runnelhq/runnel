package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const httpSource = `kind: Trigger
apiVersion: runnel/v1
name: customer
type: http
target: workflows/customer.workflow.yaml
with:
  method: post
  path: /api/customers/{id}/orders
  auth: { token: "${env:RUNNEL_TEST_TOKEN}" }
  maxConcurrentRuns: 3
`

func TestParseHTTPTrigger(t *testing.T) {
	tr, err := ParseTrigger("triggers/customer.yaml", []byte(httpSource))
	if err != nil {
		t.Fatal(err)
	}
	if tr.HTTPMethod() != "POST" || tr.Describe() != "POST /api/customers/{id}/orders" || tr.AuthNone() ||
		tr.TokenVariable() != "RUNNEL_TEST_TOKEN" || strings.Join(tr.Placeholders(), ",") != "id" || len(tr.Segments()) != 4 {
		t.Errorf("trigger %+v", tr)
	}
	t.Setenv("RUNNEL_TEST_TOKEN", "")
	if _, err := tr.Token(); err == nil || !strings.Contains(err.Error(), "environment variable RUNNEL_TEST_TOKEN with the token is not set") {
		t.Errorf("empty token: %v", err)
	}
	t.Setenv("RUNNEL_TEST_TOKEN", "s3cret")
	if tok, err := tr.Token(); err != nil || tok != "s3cret" {
		t.Errorf("token %q, %v", tok, err)
	}
	tr, err = ParseTrigger("x.yaml", []byte(strings.Replace(httpSource, `{ token: "${env:RUNNEL_TEST_TOKEN}" }`, "none", 1)))
	if err != nil || !tr.AuthNone() {
		t.Errorf("auth none: %v", err)
	}
	if tr.WaitsForRun() || tr.ResponseTimeout() != DefaultResponseTimeout || tr.HistoryMode() != HistoryAll {
		t.Error("accepted by default")
	}
	tr, err = ParseTrigger("x.yaml", []byte(httpSource+"  response: run\n  timeout: 5s\n"))
	if err != nil || !tr.WaitsForRun() || tr.ResponseTimeout() != 5*time.Second || tr.HistoryMode() != HistoryErrors {
		t.Errorf("response: run: %v", err)
	}
	tr, err = ParseTrigger("x.yaml", []byte(httpSource+"  history: none\n"))
	if err != nil || tr.HistoryMode() != HistoryNone {
		t.Errorf("history: %v", err)
	}

	for _, c := range []struct{ old, new, want string }{
		{"method: post", "method: HEAD", `invalid method "HEAD" (expected GET, POST, PUT, PATCH or DELETE)`},
		{"  method: post\n", "", `missing required field "method"`},
		{"/api/customers/{id}/orders", "api/customers", `path "api/customers" must start with /`},
		{"/api/customers/{id}/orders", "/api/customers/", "must not end with /"},
		{"/api/customers/{id}/orders", "/api//customers", "has an empty part (//)"},
		{"/api/customers/{id}/orders", "/api/c{id}", `invalid part "c{id}"`},
		{"/api/customers/{id}/orders", "/api/{1d}", `invalid placeholder "{1d}"`},
		{"/api/customers/{id}/orders", "/a/{id}/b/{id}", "placeholder {id} appears twice"},
		{`{ token: "${env:RUNNEL_TEST_TOKEN}" }`, `{ token: "abc123" }`, "the token must not be written into the file"},
		{`{ token: "${env:RUNNEL_TEST_TOKEN}" }`, "open", `invalid auth "open"`},
		{`  auth: { token: "${env:RUNNEL_TEST_TOKEN}" }` + "\n", "", `missing required field "auth"`},
		{"maxConcurrentRuns: 3", "maxConcurrentRuns: 3\n  response: wait", `invalid response "wait" (expected accepted or run)`},
		{"maxConcurrentRuns: 3", "maxConcurrentRuns: 3\n  timeout: 30s", "timeout belongs to response: run"},
		{"maxConcurrentRuns: 3", "maxConcurrentRuns: 3\n  response: run\n  timeout: 0s", "timeout must be at least 1s"},
		{"maxConcurrentRuns: 3", "maxConcurrentRuns: 3\n  history: some", `invalid history "some" (expected all, errors or none)`},
		{"maxConcurrentRuns: 3", "maxConcurrentRuns: -1", "maxConcurrentRuns must not be negative"},
		{"maxConcurrentRuns: 3", "maxConcurrentRuns: 3\n  overlap: allow", "overlap is not used by http triggers"},
	} {
		src := strings.Replace(httpSource, c.old, c.new, 1)
		if _, err := ParseTrigger("triggers/t.yaml", []byte(src)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q → %q: got %v, want %q", c.old, c.new, err, c.want)
		}
	}
}

// Routes that a request could match both are reported; the later trigger is left out.
func TestRouteConflicts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, method, path string) {
		src := strings.NewReplacer("name: customer", "name: "+name, "method: post", "method: "+method, "/api/customers/{id}/orders", path).Replace(httpSource)
		if err := os.MkdirAll(filepath.Join(dir, TriggersDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, TriggersDir, name+".yaml"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a", "GET", "/api/customers/{id}")
	write("b", "GET", "/api/customers/new")        // conflicts with a
	write("c", "POST", "/api/customers/new")       // other method
	write("d", "GET", "/api/customers/{x}/orders") // other length
	write("e", "get", "/API/customers/{key}")      // conflicts with a (case is ignored)
	list, err := LoadTriggers(dir)
	var names []string
	for _, tr := range list {
		names = append(names, tr.Name)
	}
	if strings.Join(names, ",") != "a,c,d" {
		t.Errorf("triggers %v", names)
	}
	if err == nil || !strings.Contains(err.Error(), `b.yaml:8:9: route GET /api/customers/new conflicts with trigger "a" (a.yaml): GET /api/customers/{id}; a request could match both`) ||
		!strings.Contains(err.Error(), `route GET /API/customers/{key} conflicts with trigger "a"`) {
		t.Errorf("problems %v", err)
	}
}
