package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		if code != 0 || !strings.Contains(out.String(), "Server stopped") || !strings.Contains(out.String(), "Starting runnel dev (") {
			t.Errorf("exit code %d, output %q", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop after cancel")
	}
}

// startServe starts "runnel serve" with args and returns its address and a channel
// with its exit code.
func startServe(ctx context.Context, t *testing.T, out, errOut *syncBuffer, args ...string) (string, chan int) {
	t.Helper()
	done := make(chan int, 1)
	go func() {
		done <- ExecuteContext(ctx, append([]string{"serve", "--port", "0"}, args...), out, errOut)
	}()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if m := listenPattern.FindStringSubmatch(out.String()); m != nil {
			return m[1], done
		} else if time.Now().After(deadline) {
			t.Fatalf("server did not start; output %q, errors %q", out.String(), errOut.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// With --stop-timeout, stopping lets running runs end before it cancels them (S41).
func TestServeStopTimeout(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"project.yaml": "kind: Project\napiVersion: runnel/v1\nname: p\n",
		"workflows/wait.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: wait\nparameters:\n  - { name: D }\n" +
			"actions:\n  - { id: start, type: core.start }\n  - { id: wait, type: core.wait, with: { duration: \"${D}\" } }\n" +
			"links:\n  - { from: start, to: wait }\n",
	})
	for _, c := range []struct {
		name, delay, want string
	}{
		{"ends in time", "300ms", "waiting up to 5s for 1 running run to end"},
		{"cancelled", "1h", "cancelling 1 run still running after 500ms"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out, errOut syncBuffer
			timeout := map[string]string{"300ms": "5s", "1h": "500ms"}[c.delay]
			base, done := startServe(ctx, t, &out, &errOut, "--stop-timeout", timeout, dir)
			resp, err := http.Post(base+"/api/v1/runs", "application/json", strings.NewReader(`{"path": "workflows/wait.workflow.yaml", "params": {"D": "`+c.delay+`"}}`))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("start: %d", resp.StatusCode)
			}
			cancel()
			select {
			case code := <-done:
				if code != 0 || !strings.Contains(out.String(), c.want) {
					t.Errorf("exit code %d, output %q", code, out.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("server did not stop")
			}
		})
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
	code, _, errOut = run(t, "serve", "--stop-timeout", "-1s", ".")
	if code != 1 || !strings.Contains(errOut, "invalid --stop-timeout -1s") {
		t.Errorf("invalid stop timeout: exit code %d, stderr %q", code, errOut)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
