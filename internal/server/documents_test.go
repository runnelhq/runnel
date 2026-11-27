package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocumentsAndImages: Markdown documents and images (E17) have kinds of their own and
// are served with their type; files of the project never run as a page, an SVG image (R8)
// runs no scripts when opened directly.
func TestDocumentsAndImages(t *testing.T) {
	dir, s := newProject(t)
	png := "\x89PNG\r\n\x1a\n"
	for name, data := range map[string]string{"README.md": "# demo\n", "docs/overview.png": png, "docs/x.svg": "<svg onload=\"alert(1)\"/>"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct{ path, kind, contentType string }{
		{"README.md", "document", "text/plain; charset=utf-8"},
		{"docs/overview.png", "image", "image/png"},
		{"docs/x.svg", "image", "image/svg+xml"},
	} {
		if got := fileKind(tt.path); got != tt.kind {
			t.Errorf("fileKind(%s) = %s, want %s", tt.path, got, tt.kind)
		}
		rec := do(t, s, "GET", "/api/v1/files/"+tt.path, "")
		h := rec.Header()
		if rec.Code != http.StatusOK || h.Get("Content-Type") != tt.contentType || h.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("GET %s: status %d, headers %v", tt.path, rec.Code, h)
		}
	}
	if rec := do(t, s, "GET", "/api/v1/projects/current/tree", ""); !strings.Contains(rec.Body.String(), `"kind": "document"`) {
		t.Errorf("tree without the document: %s", rec.Body)
	}
}
