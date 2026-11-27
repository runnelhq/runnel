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

// TestIsBinary: the content decides for files without a known extension (R8): NUL bytes
// or bytes that are neither UTF-8 nor Windows-1252 text in the first 8 KiB are binary; a
// character cut at the end of the sniffed part is not.
func TestIsBinary(t *testing.T) {
	pad := strings.Repeat("a", sniffSize-1)
	for _, tt := range []struct {
		name   string
		data   string
		binary bool
	}{
		{"empty", "", false},
		{"ASCII", "name: demo\r\n", false},
		{"UTF-8", "Größe: 3 €\n", false},
		{"UTF-8 with BOM", "\xef\xbb\xbfa: 1\n", false},
		{"UTF-16 LE with BOM", "\xff\xfea\x00\n\x00", false},
		{"UTF-16 BE with BOM", "\xfe\xff\x00a\x00\n", false},
		{"Windows-1252", "Gr\xf6\xdfe: 3 \x80\r\n", false},
		{"NUL", "abc\x00def", true},
		{"NUL after 8 KiB", strings.Repeat("a", sniffSize) + "\x00", false},
		{"invalid UTF-8 with control bytes", "\x89PNG\r\n\x1a\n\xff\xd8", true},
		{"undefined in Windows-1252", "a\x81b", true},
		{"euro cut at 8 KiB", pad + "€ rest", false},
		{"euro cut at 8 KiB, invalid after", pad + "\xe2\x82\xac\xff\x01", false},
		{"invalid UTF-8 at the end of 8 KiB", pad[1:] + "\xff\x01 rest", true},
	} {
		if got := isBinary([]byte(tt.data)); got != tt.binary {
			t.Errorf("%s: isBinary = %v, want %v", tt.name, got, tt.binary)
		}
	}
}

// TestBinaryContent: a file with an unknown extension and binary content is refused like
// a program (415), and the refusal carries no content.
func TestBinaryContent(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "data", "random.dat"), "\x89\x01\x02\xfe\xfd\x7f")
	writeFile(t, filepath.Join(dir, "data", "legacy.csv"), "name;Gr\xf6\xdfe\r\nM\xfcller;3\r\n")
	rec := do(t, s, "GET", "/api/v1/files/data/random.dat", "")
	expect(t, rec, http.StatusUnsupportedMediaType, "cannot be shown in the editor")
	if rec.Header().Get("ETag") != "" || strings.Contains(rec.Body.String(), "\x89") {
		t.Errorf("refusal with content: %v %q", rec.Header(), rec.Body)
	}
	if rec := do(t, s, "GET", "/api/v1/files/data/legacy.csv", ""); rec.Code != http.StatusOK {
		t.Errorf("Windows-1252: %d %s", rec.Code, rec.Body)
	}
}

func BenchmarkIsBinary(b *testing.B) {
	data := []byte(strings.Repeat("name: Größe\r\n", 2000))
	b.ReportAllocs()
	for b.Loop() {
		isBinary(data)
	}
}
