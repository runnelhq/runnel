package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
)

// history: none drops the run when it ends and publishes no events; errors keeps
// only failed runs; Wait still returns the state (S38b2).
func TestHistory(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	start := func(history, delay string) (string, Detail) {
		t.Helper()
		s := spec(waitBody)
		if delay == "" { // a run that fails
			s = spec("actions:\n  - { id: start, type: core.start }\n  - { id: stop, type: core.abort }\nlinks:\n  - { from: start, to: stop }\n")
		}
		s.Params, s.History, s.Trigger = map[string]string{"DELAY": delay}, history, "http:t"
		run, _, err := m.Start("workflows/a.workflow.yaml", s)
		if err != nil {
			t.Fatal(err)
		}
		d, ok := m.Wait(context.Background(), run.ID)
		if !ok {
			t.Fatalf("%s: not found", history)
		}
		return run.ID, d
	}
	seq := m.LastSeq()
	id, d := start(HistoryNone, "1ms")
	if d.Status != "succeeded" || len(d.Log) == 0 {
		t.Errorf("none: %+v", d.Run)
	}
	gone := func(id string) bool {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if _, ok := get(m, id); !ok {
				return true
			}
		}
		return false
	}
	if !gone(id) || m.LastSeq() != seq {
		t.Errorf("none: kept or events published (%d → %d)", seq, m.LastSeq())
	}
	id, _ = start(HistoryErrors, "1ms")
	if !gone(id) {
		t.Error("errors: a run that succeeded is kept")
	}
	id, d = start(HistoryErrors, "")
	if d.Status != "failed" {
		t.Fatalf("errors: %+v", d.Run)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := get(m, id); !ok {
		t.Error("errors: a failed run is dropped")
	}
	if _, ok := m.Wait(context.Background(), "none"); ok {
		t.Error("Wait for an unknown run")
	}
}

// WaitLaunched returns the state of a launched run even when it has already ended and
// its history dropped it from memory, where Wait by id no longer finds it: runnel run
// reported such a run as failed (exit code 1) when it ended before Wait was called.
func TestWaitLaunchedAfterDrop(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	for _, history := range []string{HistoryErrors, HistoryNone} {
		s := spec(waitBody)
		s.Params = map[string]string{"DELAY": "0s"}
		pend, _, err := m.PrepareRun("workflows/a.workflow.yaml", s)
		if err != nil {
			t.Fatal(err)
		}
		pend.History = history
		if _, ok := m.WaitLaunched(context.Background(), pend); ok {
			t.Errorf("%s: WaitLaunched before Launch", history)
		}
		if _, err := m.Launch(pend); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
			if _, ok := get(m, pend.ID()); !ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: a run that succeeded is kept", history)
			}
		}
		if _, ok := m.Wait(context.Background(), pend.ID()); ok {
			t.Errorf("%s: Wait by id found a dropped run", history)
		}
		if d, ok := m.WaitLaunched(context.Background(), pend); !ok || d.Status != "succeeded" || len(d.Log) == 0 {
			t.Errorf("%s: WaitLaunched %v %+v", history, ok, d.Run)
		}
	}
}

// Runs of http triggers have their own limit, so that they do not push out others.
func TestHTTPRunLimit(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "0s"}
	first, _, err := m.Start("workflows/a.workflow.yaml", s)
	if err != nil {
		t.Fatal(err)
	}
	m.Wait(context.Background(), first.ID)
	s.Trigger = "http:t"
	for range MaxHTTPRuns + 20 {
		run, _, err := m.Start("workflows/a.workflow.yaml", s)
		if err != nil {
			t.Fatal(err)
		}
		m.Wait(context.Background(), run.ID)
	}
	m.mu.Lock()
	m.prune()
	m.mu.Unlock()
	if _, ok := get(m, first.ID); !ok {
		t.Error("the run started by hand was pushed out")
	}
	http := 0
	for _, r := range list(m) {
		if strings.HasPrefix(r.Trigger, "http:") {
			http++
		}
	}
	if http != MaxHTTPRuns {
		t.Errorf("%d runs of http triggers kept", http)
	}
}

// The cache of checked files: a changed file or project is read again; files with
// errors are not kept.
func TestCache(t *testing.T) {
	data := []byte("kind: Workflow\napiVersion: runnel/v1\nname: c\nactions:\n  - { id: start, type: core.start }\n")
	w1, _, err := parseWorkflow("c.workflow.yaml", data)
	w2, _, _ := parseWorkflow("c.workflow.yaml", data)
	if err != nil || w1 != w2 {
		t.Errorf("not cached: %v", err)
	}
	w3, _, _ := parseWorkflow("c.workflow.yaml", append(data, "description: changed\n"...))
	if w3 == w1 || w3.Description != "changed" {
		t.Error("changed content not read again")
	}
	broken := []byte("kind: Workflow\napiVersion: runnel/v1\nname: c\nactions:\n  - { id: a, type: core.log }\nlinks:\n  - { from: a, to: x }\n")
	if _, diags, err := parseWorkflow("b.workflow.yaml", broken); err != nil || !diags.HasErrors() {
		t.Errorf("broken: %v %v", diags, err)
	}
	workflowCache.mu.Lock()
	_, kept := workflowCache.m[contentKey("b.workflow.yaml", broken)]
	workflowCache.mu.Unlock()
	if kept {
		t.Error("a file with errors was cached")
	}

	dir := project(t, map[string]string{"environments/dev.yaml": "kind: Environment\napiVersion: runnel/v1\nname: dev\nvariables: { A: one }\n"})
	p1, err := loadProject(dir)
	p2, _ := loadProject(dir)
	if err != nil || p1 != p2 {
		t.Fatalf("project not cached: %v", err)
	}
	env := filepath.Join(dir, model.EnvironmentsDir, "dev.yaml")
	if err := os.WriteFile(env, []byte("kind: Environment\napiVersion: runnel/v1\nname: dev\nvariables: { A: two }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(env, later, later)
	p3, _ := loadProject(dir)
	if p3 == p1 || fmt.Sprint(p3.Environments["dev"].Variables["A"]) != "two" {
		t.Errorf("changed environment not read again: %v", p3.Environments["dev"].Variables)
	}
}
