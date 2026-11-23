package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
	"github.com/runnelhq/runnel/internal/paths"
)

func TestDownloadConfig(t *testing.T) {
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "saveTo": "a.zip", "paginate": map[string]any{"style": "link"}}, "saveTo and paginate cannot be combined")
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "ifExists": "skip"}, "ifExists needs saveTo")
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "saveTo": "a.zip", "ifExists": "keep"}, `invalid ifExists "keep"`)
}

// download runs http.request with saveTo relative to dir.
func download(t *testing.T, dir string, with map[string]any) (map[string]any, error) {
	t.Helper()
	res, _, err := actiontest.Execute(paths.WithBase(context.Background(), dir), actiontest.New(t, requestDefinition, with))
	out, _ := res.Output.(map[string]any)
	return out, err
}

// leftovers lists the files of dir: a failed download must leave no temporary file.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	return names
}

func TestDownload(t *testing.T) {
	zip := strings.Repeat("PK\x03\x04binary\x00", 1000)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/files/data.zip":
			// Like Graph: /content redirects to a download address.
			http.Redirect(w, r, "/download/data.zip?sig=abc", http.StatusFound)
		case "/download/data.zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte(zip))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"itemNotFound"}}`))
		}
	}))
	defer srv.Close()
	dir := t.TempDir()

	out, err := download(t, dir, map[string]any{"url": srv.URL + "/files/data.zip", "saveTo": "inbox/data.zip"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "inbox", "data.zip"))
	if string(data) != zip || out["size"] != int64(len(zip)) || out["file"] != filepath.Join(dir, "inbox", "data.zip") || out["body"] != nil || out["status"] != int64(200) {
		t.Fatalf("output %v, %d bytes", out, len(data))
	}

	// skip: no request; fail: refused before the request; replace (default): new content.
	calls = 0
	if out, err := download(t, dir, map[string]any{"url": srv.URL + "/files/data.zip", "saveTo": "inbox/data.zip", "ifExists": "skip"}); err != nil || out["skipped"] != true || calls != 0 {
		t.Errorf("skip: %v %v %d calls", out, err, calls)
	}
	if _, err := download(t, dir, map[string]any{"url": srv.URL + "/files/data.zip", "saveTo": "inbox/data.zip", "ifExists": "fail"}); err == nil || !strings.Contains(err.Error(), "exists (ifExists: fail)") || calls != 0 {
		t.Errorf("fail: %v %d calls", err, calls)
	}

	// An error answer is not saved: the file stays as it was, the message names the status.
	if _, err := download(t, dir, map[string]any{"url": srv.URL + "/files/gone.zip", "saveTo": "inbox/data.zip"}); err == nil || !strings.Contains(err.Error(), "404 Not Found") || !strings.Contains(err.Error(), "itemNotFound") {
		t.Errorf("404: %v", err)
	}
	if out, err := download(t, dir, map[string]any{"url": srv.URL + "/files/gone.zip", "saveTo": "inbox/other.zip", "failOnError": false}); err != nil || out["status"] != int64(404) || out["file"] != nil {
		t.Errorf("404 without failOnError: %v %v", out, err)
	}

	// Larger than maxSize: an error, no half file.
	if _, err := download(t, dir, map[string]any{"url": srv.URL + "/files/data.zip", "saveTo": "inbox/big.zip", "maxSize": "1KB"}); err == nil || !strings.Contains(err.Error(), "larger than 1KB") {
		t.Errorf("maxSize: %v", err)
	}
	if got := strings.Join(leftovers(t, dir), ","); got != "inbox/data.zip" {
		t.Errorf("files left: %s", got)
	}
}

// A download that takes longer than timeout is cancelled and leaves nothing behind.
func TestDownloadTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PK"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	_, err := download(t, dir, map[string]any{"url": srv.URL + "/slow.zip", "saveTo": "slow.zip", "timeout": "200ms"})
	if err == nil || !strings.Contains(err.Error(), "within 200ms (timeout)") {
		t.Errorf("timeout: %v", err)
	}
	if got := leftovers(t, dir); len(got) != 0 {
		t.Errorf("files left: %v", got)
	}
}

// BenchmarkDownload saves a 10 MB answer: memory must not grow with the size of the file.
func BenchmarkDownload(b *testing.B) {
	data := []byte(strings.Repeat("0123456789abcdef", 10<<16)) // 10 MB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	dir := b.TempDir()
	a, err := requestDefinition.New(map[string]any{"url": srv.URL, "saveTo": filepath.Join(dir, "out.bin")})
	if err != nil {
		b.Fatal(err)
	}
	ec := &action.ExecContext{Log: func(_ slog.Level, _ string) {}}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for range b.N {
		if _, err := a.Execute(context.Background(), ec); err != nil {
			b.Fatal(err)
		}
	}
}
