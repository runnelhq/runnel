package runs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// settings.maxConcurrentRuns of the workflow and of the project reject further starts
// while their runs are running (S37); the queue is in the backlog.
func TestStartLimits(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	limited := spec("settings: { maxConcurrentRuns: 1 }\n" + waitBody)
	limited.Params = map[string]string{"DELAY": "1h"}
	first, _, err := m.Start("workflows/a.workflow.yaml", limited)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = m.Start("workflows/a.workflow.yaml", limited)
	var limit *LimitError
	if !errors.As(err, &limit) || !strings.Contains(err.Error(), "workflows/a.workflow.yaml is already running 1 run (settings.maxConcurrentRuns: 1)") {
		t.Fatalf("second start: %v", err)
	}
	// Another file is not limited by it.
	other := spec(waitBody)
	other.Params = map[string]string{"DELAY": "1h"}
	if _, _, err := m.Start("workflows/b.workflow.yaml", other); err != nil {
		t.Fatalf("other workflow: %v", err)
	}
	// Once the run has ended, the workflow can run again.
	m.Cancel(first.ID)
	waitFor(t, m, first.ID, finished)
	if _, _, err := m.Start("workflows/a.workflow.yaml", limited); err != nil {
		t.Errorf("after the end: %v", err)
	}

	// The limit of the project counts all runs.
	if err := m.checkLimits("workflows/c.workflow.yaml", limits{project: 2}); err == nil ||
		!strings.Contains(err.Error(), "2 runs already running in the project (settings.maxConcurrentRuns in project.yaml: 2)") {
		t.Errorf("project limit: %v", err)
	}
	if err := m.checkLimits("workflows/c.workflow.yaml", limits{project: 3}); err != nil {
		t.Errorf("below the project limit: %v", err)
	}
}

func TestStartRecordsTrigger(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "1ms"}
	s.Trigger = "cron:nightly"
	run, _, err := m.Start("workflows/a.workflow.yaml", s)
	if err != nil || run.Trigger != "cron:nightly" {
		t.Fatalf("%+v %v", run, err)
	}
}

// Drain rejects new runs and waits for the running ones without cancelling them (S41).
func TestDrain(t *testing.T) {
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	short := spec(waitBody)
	short.Params = map[string]string{"DELAY": "200ms"}
	run, _, err := m.Start("workflows/a.workflow.yaml", short)
	if err != nil {
		t.Fatal(err)
	}
	if m.Running() != 1 {
		t.Fatalf("running %d", m.Running())
	}
	if err := m.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, _ := get(m, run.ID); d.Status != "succeeded" {
		t.Errorf("drained run: %s", d.Status)
	}
	if _, _, err := m.Start("workflows/a.workflow.yaml", short); !errors.Is(err, ErrShutdown) {
		t.Errorf("start after drain: %v", err)
	}
}
