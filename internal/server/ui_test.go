package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/version"
)

func newUIServer(t *testing.T) *Server {
	t.Helper()
	ui := fstest.MapFS{
		"index.html":          {Data: []byte("<!doctype html><title>Runnel</title>")},
		"favicon.svg":         {Data: []byte("<svg/>")},
		"assets/index-abc.js": {Data: []byte("console.log(1)")},
	}
	s, err := New(Options{Dir: t.TempDir(), Registry: actions.Builtin(), UI: ui})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUI(t *testing.T) {
	s := newUIServer(t)

	rec := do(t, s, "GET", "/", "")
	expect(t, rec, http.StatusOK, "<title>Runnel</title>")
	h := rec.Header()
	if h.Get("Cache-Control") != "no-cache" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != contentSecurityPolicy {
		t.Errorf("headers of index.html: %v", h)
	}

	rec = do(t, s, "GET", "/assets/index-abc.js", "")
	expect(t, rec, http.StatusOK, "console.log")
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control of an asset: %q", got)
	}
	expect(t, do(t, s, "GET", "/favicon.svg", ""), http.StatusOK, "<svg/>")

	// Client-side views get index.html; missing files are 404.
	expect(t, do(t, s, "GET", "/workflows/hello", ""), http.StatusOK, "<title>Runnel</title>")
	expect(t, do(t, s, "GET", "/assets/missing.js", ""), http.StatusNotFound, "")
	// The API is not shadowed by the UI.
	expect(t, do(t, s, "GET", "/api/v1/nothing", ""), http.StatusNotFound, "unknown endpoint")
	expect(t, do(t, s, "POST", "/", ""), http.StatusMethodNotAllowed, "method not allowed")
	// The host check applies to the UI as well.
	expect(t, do(t, s, "GET", "/", "", "Host", "evil.example"), http.StatusForbidden, "localhost")
}

func TestVersionEndpoint(t *testing.T) {
	s := newUIServer(t)
	rec := do(t, s, "GET", "/api/v1/version", "")
	var info version.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || info != version.Get() {
		t.Errorf("version %+v, err %v: %s", info, err, rec.Body)
	}
}
