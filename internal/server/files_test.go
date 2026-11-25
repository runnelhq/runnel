package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Binary files stay in the tree (kind binary) but are not sent to the editor: known
// extensions and content with NUL bytes are refused (415), text in UTF-16 with BOM is
// not; files over MaxFileSize are not read at all (422).
func TestBinaryFiles(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "plugins", "demo", "bin", "demo.exe"), "MZ\x00\x00program")
	writeFile(t, filepath.Join(dir, "data", "dump.dat"), "abc\x00def")
	writeFile(t, filepath.Join(dir, "data", "utf16.yaml"), "\xff\xfea\x00:\x00 \x001\x00")
	writeFile(t, filepath.Join(dir, "data", "big.txt"), strings.Repeat("x", MaxFileSize+1))

	expect(t, do(t, s, "GET", "/api/v1/projects/current/tree", ""), http.StatusOK, `"kind": "binary"`)
	expect(t, do(t, s, "GET", "/api/v1/files/plugins/demo/bin/demo.exe", ""), http.StatusUnsupportedMediaType, "a binary file")
	expect(t, do(t, s, "GET", "/api/v1/files/data/dump.dat", ""), http.StatusUnsupportedMediaType, "cannot be shown in the editor")
	if rec := do(t, s, "GET", "/api/v1/files/data/utf16.yaml", ""); rec.Code != http.StatusOK {
		t.Errorf("UTF-16: %d %s", rec.Code, rec.Body)
	}
	expect(t, do(t, s, "GET", "/api/v1/files/data/big.txt", ""), http.StatusUnprocessableEntity, "too large for the editor")
}
