package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/runnelhq/runnel/internal/version"
)

// contentSecurityPolicy allows only the editor's own files and API: no scripts, styles
// or fonts from elsewhere, no framing by other pages.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// handleUI serves the embedded editor. Files under assets/ carry a content hash in
// their name and may be cached for good; everything else is revalidated. Paths
// without a file extension (client-side views) get index.html.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(s.ui, name); errors.Is(err, fs.ErrNotExist) && path.Ext(name) == "" {
		name = "index.html"
	}
	if name == "index.html" {
		if _, err := fs.Stat(s.ui, name); err != nil {
			h.Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = fmt.Fprintf(w, "Runnel server %s. This build does not include the editor "+
				"(build it with \"make web\", see docs/dev/EDITOR.md); the API is available under %s.\n",
				version.Version, APIPrefix)
			return
		}
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, s.ui, name) //nolint:gosec // s.ui is the embedded build of the editor, not the file system
}

func handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version.Get())
}
