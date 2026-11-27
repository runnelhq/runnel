package runs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/model"
)

func spec(body string) Spec {
	return Spec{
		Name:     "test.workflow.yaml",
		Data:     []byte("kind: Workflow\napiVersion: runnel/v1\nname: test\n" + body),
		Registry: actions.Builtin(),
	}
}

const waitBody = `
actions:
  - { id: start, type: core.start }
  - { id: wait, type: core.wait, with: { duration: "${DELAY}" } }
  - { id: done, type: core.log, with: { message: "waited ${DELAY}" } }
  - { id: never, type: core.log, with: { message: "x" } }
links:
  - { from: start, to: wait }
  - { from: wait, to: done }
  - { from: wait, to: never, when: failure }
`

// waitFor polls the run until cond holds.
func waitFor(t *testing.T, m *Manager, id string, cond func(Detail) bool) Detail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d, ok := get(m, id)
		if ok && cond(d) {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not reached; run %+v", d)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func finished(d Detail) bool { return d.Status != StatusRunning }

func TestStartAndRecord(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()

	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "10ms"}
	run, _, err := m.Start("workflows/test.workflow.yaml", s)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusRunning || run.Name != "test" || len(run.ID) != 26 {
		t.Errorf("run %+v", run)
	}
	d := waitFor(t, m, run.ID, finished)
	if d.Status != workflow.Succeeded || d.FinishedAt == nil || d.Error != "" {
		t.Errorf("run %+v", d.Run)
	}
	states := map[string]workflow.Status{}
	for _, a := range d.Actions {
		states[a.ID] = a.Status
	}
	want := map[string]workflow.Status{"start": workflow.Succeeded, "wait": workflow.Succeeded,
		"done": workflow.Succeeded, "never": workflow.Skipped}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Errorf("action states %v, want %v", states, want)
	}
	if len(d.Log) == 0 || d.Log[len(d.Log)-1].Message != "waited 10ms" || d.Log[len(d.Log)-1].Level != "info" {
		t.Errorf("log %+v", d.Log)
	}

	events, complete, _ := m.EventsSince(0)
	if !complete || events[0].Kind != workflow.RunStarted || events[len(events)-1].Kind != workflow.RunFinished {
		t.Fatalf("events %+v", events)
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) || ev.RunID != run.ID {
			t.Errorf("event %d: %+v", i, ev)
		}
	}
	if runs := list(m); len(runs) != 1 || runs[0].ID != run.ID {
		t.Errorf("runs %+v", runs)
	}
}

func TestStartFails(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()

	_, _, err := m.Start("x", spec("actions: [ { id: start, type: core.start }, { id: start, type: core.log } ]\n"))
	var diags model.Errors
	if !errors.As(err, &diags) || !diags.HasErrors() {
		t.Errorf("invalid workflow: %v", err)
	}
	if len(list(m)) != 0 || m.LastSeq() != 0 {
		t.Error("failed start left a run or events")
	}
}

func TestCancel(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "1h"}
	run, _, err := m.Start("x", s)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, m, run.ID, func(d Detail) bool { return d.Actions[1].Status == StatusRunning })
	if found, running := m.Cancel(run.ID); !found || !running {
		t.Fatalf("cancel: found %v, running %v", found, running)
	}
	d := waitFor(t, m, run.ID, finished)
	if d.Status != workflow.Cancelled || d.Actions[1].Status != workflow.Cancelled {
		t.Errorf("run %s, wait %s", d.Status, d.Actions[1].Status)
	}
	if found, running := m.Cancel(run.ID); !found || running {
		t.Errorf("second cancel: found %v, running %v", found, running)
	}
	if found, _ := m.Cancel("nope"); found {
		t.Error("unknown run found")
	}
}

func TestShutdown(t *testing.T) {
	m := NewManager()
	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "1h"}
	run, _, err := m.Start("x", s)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ := get(m, run.ID); d.Status != workflow.Cancelled {
		t.Errorf("status after shutdown %s", d.Status)
	}
	if _, _, err := m.Start("x", s); !errors.Is(err, ErrShutdown) {
		t.Errorf("start after shutdown: %v", err)
	}
}

func TestPruneAndEventWindow(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	quick := spec("actions:\n  - { id: start, type: core.start }\n")
	var first string
	for i := range MaxRuns + 5 {
		run, _, err := m.Start("x", quick)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = run.ID
		}
		waitFor(t, m, run.ID, finished)
	}
	if n := len(list(m)); n != MaxRuns {
		t.Errorf("%d runs kept, want %d", n, MaxRuns)
	}
	if _, ok := get(m, first); ok {
		t.Error("oldest run was not dropped")
	}

	// Each run has 4 events; push the first ones out of the window.
	for range MaxEvents / 4 {
		run, _, _ := m.Start("x", quick)
		waitFor(t, m, run.ID, finished)
	}
	if _, complete, _ := m.EventsSince(0); complete {
		t.Error("events since 0 should be incomplete after the window moved")
	}
	last := m.LastSeq()
	if events, complete, _ := m.EventsSince(last - 2); !complete || len(events) != 2 {
		t.Errorf("recent events: %d, complete %v", len(events), complete)
	}
}

func TestEventsWake(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	_, _, wake := m.EventsSince(m.LastSeq())
	if _, _, err := m.Start("x", spec("actions:\n  - { id: start, type: core.start }\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("subscriber not woken by new events")
	}
}

func TestPrepareErrors(t *testing.T) {
	s := spec("parameters:\n  - { name: TARGET, required: true }\n  - { name: N, type: int }\nactions:\n  - { id: start, type: core.start }\n")
	s.Params = map[string]string{"N": "many", "EXTRA": "1"}
	_, err := Prepare(s)
	if !errors.Is(err, ErrMissingParameters) || !strings.Contains(err.Error(), `parameter N (int): "many" is not a whole number (expected e.g. 1234); missing required parameter(s) [TARGET]`) {
		t.Errorf("error %v", err)
	}

	s.Params = map[string]string{"TARGET": "x", "EXTRA": "1"}
	p, err := Prepare(s)
	if err != nil || fmt.Sprint(p.Undeclared) != "[EXTRA]" || p.Runner == nil {
		t.Errorf("prepared %+v, err %v", p, err)
	}

	s.Env = "dev"
	if _, err := Prepare(s); err == nil || !strings.Contains(err.Error(), "no project (project.yaml) found") {
		t.Errorf("environment without project: %v", err)
	}
}

// get and list read runs like the API, failing on errors of the history.
func get(m *Manager, id string) (Detail, bool) {
	d, ok, err := m.Get(context.Background(), id)
	if err != nil {
		panic(err)
	}
	return d, ok
}

func list(m *Manager) []Run {
	r, err := m.Runs(context.Background())
	if err != nil {
		panic(err)
	}
	return r
}
