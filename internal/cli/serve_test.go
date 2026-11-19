package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer that can be read while the server writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var listenPattern = regexp.MustCompile(`Listening on (http://127\.0\.0\.1:\d+)`)

func TestServe(t *testing.T) {
	chdirRepoRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errOut syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- ExecuteContext(ctx, []string{"serve", "--port", "0", "examples/hello/workflows"}, &out, &errOut)
	}()

	var base string
	for deadline := time.Now().Add(10 * time.Second); base == ""; {
		if m := listenPattern.FindStringSubmatch(out.String()); m != nil {
			base = m[1]
		} else if time.Now().After(deadline) {
			t.Fatalf("server did not start; output %q, errors %q", out.String(), errOut.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, err := http.Get(base + "/api/v1/projects/current")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name": "hello"`) {
		t.Errorf("status %d, body %s", resp.StatusCode, body)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 || !strings.Contains(out.String(), "Server stopped") {
			t.Errorf("exit code %d, output %q", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop after cancel")
	}
}

func TestServeErrors(t *testing.T) {
	code, _, errOut := run(t, "serve", t.TempDir())
	if code != 1 || !strings.Contains(errOut, "no project (project.yaml) found") {
		t.Errorf("without project: exit code %d, stderr %q", code, errOut)
	}
	code, _, errOut = run(t, "serve", "--port", "70000", ".")
	if code != 1 || !strings.Contains(errOut, "invalid --port 70000") {
		t.Errorf("invalid port: exit code %d, stderr %q", code, errOut)
	}
}
