package workflow

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	// test.flaky fails twice, then succeeds.
	for _, tt := range []struct {
		attempts   int
		wantStatus Status
	}{
		{2, Failed},
		{3, Succeeded},
		{5, Succeeded},
	} {
		w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: flaky, type: test.flaky, retry: { attempts: `+strconv.Itoa(tt.attempts)+`, backoff: 10ms } }
links:
  - { from: start, to: flaky }
`)
		var logs []string
		r, err := Prepare(w, Options{Registry: testRegistry(t), Observer: func(e Event) {
			if e.Kind == Log {
				logs = append(logs, e.Message)
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		res := r.Run(context.Background())
		if res.Status != tt.wantStatus {
			t.Errorf("attempts %d: status %s (%v), want %s", tt.attempts, res.Status, res.Err, tt.wantStatus)
		}
		if want := "attempt 1 of " + strconv.Itoa(tt.attempts) + " failed: attempt 1 failed; retrying in 10ms"; len(logs) == 0 || logs[0] != want {
			t.Errorf("attempts %d: logs %q, want first %q", tt.attempts, logs, want)
		}
	}
}

func TestRetryExponentialBackoff(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: flaky, type: test.flaky, retry: { attempts: 3, backoff: 40ms, strategy: exponential } }
links:
  - { from: start, to: flaky }
`)
	begin := time.Now()
	res, _ := runWorkflow(t, w, nil)
	// waits: 40ms before attempt 2, 80ms before attempt 3
	if d := time.Since(begin); res.Status != Succeeded || d < 120*time.Millisecond {
		t.Errorf("status %s after %v, want success after >= 120ms", res.Status, d)
	}
}

func TestActionTimeout(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: slow, type: core.wait, timeout: 50ms, retry: { attempts: 2 }, with: { duration: 1h } }
  - { id: handle, type: test.ok, with: { value: handled } }
links:
  - { from: start, to: slow }
  - { from: slow, to: handle, when: failure }
`)
	begin := time.Now()
	res, trace := runWorkflow(t, w, nil)
	got := strings.Join(trace, "\n")
	if res.Status != Succeeded || time.Since(begin) > 2*time.Second {
		t.Fatalf("status %s after %v:\n%s", res.Status, time.Since(begin), got)
	}
	for _, want := range []string{
		"attempt 1 of 2 failed: timed out after 50ms",
		"finished slow: failed",
		"log handle: handled",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestJoinTimeout(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: fast, type: test.ok }
  - { id: slow, type: core.wait, with: { duration: 300ms } }
  - { id: join, type: test.ok, join: all, joinTimeout: 50ms }
  - { id: late, type: test.ok, with: { value: join failed } }
links:
  - { from: start, to: fast }
  - { from: start, to: slow }
  - { from: fast, to: join }
  - { from: slow, to: join }
  - { from: join, to: late, when: failure }
`)
	var joinErr string
	var order []string
	r, err := Prepare(w, Options{Registry: testRegistry(t), Observer: func(e Event) {
		if e.Kind == ActionFinished {
			order = append(order, e.ActionID)
			if e.ActionID == "join" && e.Err != nil {
				joinErr = e.Err.Error()
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background())
	if res.Status != Succeeded {
		t.Errorf("status %s (%v)", res.Status, res.Err)
	}
	if joinErr != "join timeout: still waiting for [slow] after 50ms" {
		t.Errorf("join error %q", joinErr)
	}
	// join fails before slow finishes; slow's late link is ignored.
	if got := strings.Join(order, ","); got != "start,fast,join,late,slow" {
		t.Errorf("finish order %s", got)
	}
}

func TestWorkflowTimeout(t *testing.T) {
	w := parse(t, `
settings:
  timeout: 50ms
actions:
  - { id: start, type: core.start }
  - { id: slow, type: core.wait, with: { duration: 1h } }
  - { id: handle, type: test.ok }
links:
  - { from: start, to: slow }
  - { from: slow, to: handle, when: failure }
`)
	begin := time.Now()
	res, trace := runWorkflow(t, w, nil)
	if res.Status != Failed || res.Err.Error() != "workflow timed out after 50ms" || time.Since(begin) > 2*time.Second {
		t.Errorf("result %+v after %v", res, time.Since(begin))
	}
	if strings.Contains(strings.Join(trace, "\n"), "started handle") {
		t.Errorf("failure link must not run after the workflow timed out:\n%s", strings.Join(trace, "\n"))
	}
}

func TestCancelDoesNotRunFailureLinks(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: slow, type: core.wait, retry: { attempts: 3, backoff: 1h }, with: { duration: 1h } }
  - { id: handle, type: test.ok }
links:
  - { from: start, to: slow }
  - { from: slow, to: handle, when: failure }
`)
	r, err := Prepare(w, Options{Registry: testRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	begin := time.Now()
	res := r.Run(ctx)
	if res.Status != Cancelled || time.Since(begin) > 2*time.Second {
		t.Errorf("result %+v after %v", res, time.Since(begin))
	}
}
