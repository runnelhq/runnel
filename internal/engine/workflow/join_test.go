package workflow

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
)

// runTrace runs a workflow and returns the result and, per action, how often it
// started and whether it was skipped.
func runTrace(t *testing.T, w *model.Workflow, opts Options) (Result, map[string]int, []string) {
	t.Helper()
	var mu sync.Mutex
	starts := map[string]int{}
	var skipped []string
	opts.Registry = testRegistry(t)
	opts.Observer = func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		switch e.Kind {
		case ActionStarted:
			starts[e.ActionID]++
		case ActionSkipped:
			skipped = append(skipped, e.ActionID)
		}
	}
	r, err := Prepare(w, opts)
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background())
	slices.Sort(skipped)
	return res, starts, skipped
}

func TestJoinModes(t *testing.T) {
	// start → a, b (both succeed) → target
	graph := func(join string) string {
		return fmt.Sprintf(`
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.ok }
  - { id: target, type: test.ok%s }
links:
  - { from: start, to: a }
  - { from: start, to: b }
  - { from: a, to: target }
  - { from: b, to: target }
`, join)
	}
	for _, tt := range []struct {
		join string
		want int
	}{
		{"", 1},              // default: all
		{", join: all", 1},   // waits for both branches
		{", join: first", 1}, // starts with the first branch, ignores the second
		{", join: each", 2},  // starts once per branch
	} {
		res, starts, _ := runTrace(t, parse(t, graph(tt.join)), Options{})
		if res.Status != Succeeded || starts["target"] != tt.want {
			t.Errorf("join%q: status %s, target started %d times, want %d", tt.join, res.Status, starts["target"], tt.want)
		}
	}
}

func TestJoinAllWaitsForAllBranches(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: slow, type: core.wait, with: { duration: 80ms } }
  - { id: fast, type: test.ok }
  - { id: join, type: test.ok }
links:
  - { from: start, to: slow }
  - { from: start, to: fast }
  - { from: slow, to: join }
  - { from: fast, to: join }
`)
	var mu sync.Mutex
	var order []string
	r, err := Prepare(w, Options{Registry: testRegistry(t), Observer: func(e Event) {
		if e.Kind == ActionFinished {
			mu.Lock()
			order = append(order, e.ActionID)
			mu.Unlock()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res := r.Run(context.Background()); res.Status != Succeeded {
		t.Fatalf("result %+v", res)
	}
	if got := strings.Join(order, ","); got != "start,fast,slow,join" {
		t.Errorf("finish order %s, want start,fast,slow,join", got)
	}
}

func TestBranchesRunInParallel(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: core.wait, with: { duration: 150ms } }
  - { id: b, type: core.wait, with: { duration: 150ms } }
  - { id: c, type: core.wait, with: { duration: 150ms } }
links:
  - { from: start, to: a }
  - { from: start, to: b }
  - { from: start, to: c }
`)
	begin := time.Now()
	res, _, _ := runTrace(t, w, Options{})
	if res.Status != Succeeded {
		t.Fatalf("result %+v", res)
	}
	if d := time.Since(begin); d > 400*time.Millisecond {
		t.Errorf("three 150ms waits took %v; branches did not run in parallel", d)
	}
}

// The error handling pattern from the concept: two failure links into one action.
func TestDeadPathElimination(t *testing.T) {
	yaml := func(waitFile, load string) string {
		return fmt.Sprintf(`
actions:
  - { id: start, type: core.start }
  - { id: wait-file, type: %s }
  - { id: load, type: %s }
  - { id: archive, type: test.ok }
  - { id: notify-error, type: test.ok }
  - { id: abort, type: core.abort }
links:
  - { from: start, to: wait-file }
  - { from: wait-file, to: load }
  - { from: load, to: archive }
  - { from: wait-file, to: notify-error, when: failure }
  - { from: load, to: notify-error, when: failure }
  - { from: notify-error, to: abort }
`, waitFile, load)
	}
	tests := []struct {
		name, waitFile, load string
		wantStatus           Status
		wantStarts           map[string]int
		wantSkipped          string
	}{
		{"all ok", "test.ok", "test.ok", Succeeded,
			map[string]int{"start": 1, "wait-file": 1, "load": 1, "archive": 1},
			"abort,notify-error"},
		{"first step fails", "test.fail", "test.ok", Failed,
			map[string]int{"start": 1, "wait-file": 1, "notify-error": 1, "abort": 1},
			"archive,load"},
		{"second step fails", "test.ok", "test.fail", Failed,
			map[string]int{"start": 1, "wait-file": 1, "load": 1, "notify-error": 1, "abort": 1},
			"archive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, starts, skipped := runTrace(t, parse(t, yaml(tt.waitFile, tt.load)), Options{})
			if res.Status != tt.wantStatus {
				t.Errorf("status %s (err %v), want %s", res.Status, res.Err, tt.wantStatus)
			}
			if fmt.Sprint(starts) != fmt.Sprint(tt.wantStarts) {
				t.Errorf("starts %v, want %v", starts, tt.wantStarts)
			}
			if got := strings.Join(skipped, ","); got != tt.wantSkipped {
				t.Errorf("skipped %s, want %s", got, tt.wantSkipped)
			}
		})
	}
}

func TestJoinAllWithOneDeadBranch(t *testing.T) {
	// a fails (its success link dies), b succeeds: join all still starts once.
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.fail, continueOnError: false }
  - { id: handle, type: test.ok }
  - { id: b, type: test.ok }
  - { id: join, type: test.ok }
links:
  - { from: start, to: a }
  - { from: start, to: b }
  - { from: a, to: join }
  - { from: a, to: handle, when: failure }
  - { from: b, to: join }
`)
	res, starts, skipped := runTrace(t, w, Options{})
	if res.Status != Succeeded || starts["join"] != 1 || len(skipped) != 0 {
		t.Errorf("status %s (%v), starts %v, skipped %v", res.Status, res.Err, starts, skipped)
	}
}

func TestFailureInOneBranchFailsRunAfterOthersFinish(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: broken, type: test.fail, with: { value: x } }
  - { id: slow, type: core.wait, with: { duration: 50ms } }
  - { id: after-slow, type: test.ok }
links:
  - { from: start, to: broken }
  - { from: start, to: slow }
  - { from: slow, to: after-slow }
`)
	res, starts, _ := runTrace(t, w, Options{})
	if res.Status != Failed || !strings.Contains(res.Err.Error(), `action "broken" failed`) {
		t.Errorf("result %+v", res)
	}
	if starts["after-slow"] != 1 {
		t.Errorf("other branch should finish: starts %v", starts)
	}
}

func TestSuccessEndsRunAndCancelsOtherBranches(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: done, type: core.success }
  - { id: long, type: core.wait, with: { duration: 1h } }
  - { id: after-long, type: test.ok }
links:
  - { from: start, to: done }
  - { from: start, to: long }
  - { from: long, to: after-long, when: always }
`)
	begin := time.Now()
	res, starts, _ := runTrace(t, w, Options{})
	if res.Status != Succeeded || time.Since(begin) > 5*time.Second || starts["after-long"] != 0 {
		t.Errorf("status %s after %v, starts %v", res.Status, time.Since(begin), starts)
	}
}

func TestProjectDefaultJoin(t *testing.T) {
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.ok }
  - { id: target, type: test.ok }
  - { id: explicit, type: test.ok, join: all }
links:
  - { from: start, to: a }
  - { from: start, to: b }
  - { from: a, to: target }
  - { from: b, to: target }
  - { from: a, to: explicit }
  - { from: b, to: explicit }
`)
	_, starts, _ := runTrace(t, w, Options{DefaultJoin: model.JoinEach})
	if starts["target"] != 2 || starts["explicit"] != 1 {
		t.Errorf("starts %v, want target 2 (project default each) and explicit 1", starts)
	}
}

func TestLoopRestartsIteration(t *testing.T) {
	// "try" has an incoming link from start and a loop link. After a failure, its
	// success link is dead and "report" is skipped; the loop starts a new iteration,
	// which resets the states downstream of "try", so "report" runs after the success.
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: try, type: test.flaky }
  - { id: pause, type: test.ok }
  - { id: report, type: test.ok }
links:
  - { from: start, to: try }
  - { from: try, to: pause, when: failure }
  - { from: pause, to: try, loop: true }
  - { from: try, to: report }
`)
	res, starts, _ := runTrace(t, w, Options{})
	if res.Status != Succeeded || starts["try"] != 3 || starts["report"] != 1 || starts["pause"] != 2 {
		t.Errorf("status %s (%v), starts %v; want try 3x, pause 2x, report 1x", res.Status, res.Err, starts)
	}
}

func TestJoinAllReportsWaiting(t *testing.T) {
	// start → a, b, c → target (join all): target waits after the first and the second branch.
	w := parse(t, `
actions:
  - { id: start, type: core.start }
  - { id: a, type: test.ok }
  - { id: b, type: test.ok }
  - { id: c, type: test.ok }
  - { id: target, type: test.ok }
links:
  - { from: start, to: a }
  - { from: start, to: b }
  - { from: start, to: c }
  - { from: a, to: target }
  - { from: b, to: target }
  - { from: c, to: target }
`)
	var mu sync.Mutex
	var waiting []string
	opts := Options{Registry: testRegistry(t), Observer: func(e Event) {
		if e.Kind == ActionWaiting {
			mu.Lock()
			defer mu.Unlock()
			waiting = append(waiting, e.ActionID+": "+e.Message)
		}
	}}
	r, err := Prepare(w, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res := r.Run(context.Background()); res.Status != Succeeded {
		t.Fatalf("status %s: %v", res.Status, res.Err)
	}
	want := []string{
		"target: waiting for 2 more incoming links (join: all)",
		"target: waiting for 1 more incoming link (join: all)",
	}
	if !slices.Equal(waiting, want) {
		t.Errorf("waiting events %q, want %q", waiting, want)
	}
}
