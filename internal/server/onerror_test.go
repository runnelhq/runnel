package server

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/history"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/triggers"
)

const failWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: fail
parameters:
  - { name: WHO, required: true }
actions:
  - { id: start, type: core.start }
  - { id: boom, type: core.abort, with: { message: "broken for ${WHO}" } }
links:
  - { from: start, to: boom }
`

const reportWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: report
parameters:
  - { name: FAILED_RUN }
  - { name: FAILED_ACTION }
  - { name: FAILED_ERROR }
  - { name: FAILED_TRIGGER }
actions:
  - { id: start, type: core.start }
  - { id: log, type: core.log, with: { message: "run=${FAILED_RUN} action=${FAILED_ACTION} trigger=${FAILED_TRIGGER} error=${FAILED_ERROR} in ${TARGET}" } }
links:
  - { from: start, to: log }
`

// errorRuns waits until there are n runs of error workflows and returns them, newest first.
func errorRuns(t *testing.T, s *Server, n int) []runs.Run {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		list, err := s.runs.Find(context.Background(), history.Filter{Trigger: runs.TriggerError})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) >= n || time.Now().After(deadline) {
			if len(list) != n {
				t.Fatalf("%d error runs, want %d: %+v", len(list), n, list)
			}
			return list
		}
	}
}

// A failed run of a trigger starts the error workflow of the project (ADR 0027): with
// trigger error, the failed run as parent and the same environment. A run that fails
// before it starts is reported without run id, also when settings.maxConcurrentRuns is
// reached. Runs of the editor are not reported.
func TestOnError(t *testing.T) {
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\n"+
		"settings:\n  maxConcurrentRuns: 1\n  onError: workflows/report.workflow.yaml\n")
	writeFile(t, filepath.Join(dir, "workflows", "fail.workflow.yaml"), failWorkflow)
	writeFile(t, filepath.Join(dir, "workflows", "report.workflow.yaml"), reportWorkflow)
	trigger := func(params string) *model.Trigger {
		t.Helper()
		tr, err := model.ParseTrigger("triggers/nightly.yaml", []byte("kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\n"+
			"target: workflows/fail.workflow.yaml\nenvironment: dev\n"+params+"with: { schedule: \"0 3 * * *\" }\n"))
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}

	// Runs of the editor and "Run target now": no error workflow.
	run := startRun(t, s, `{"path": "workflows/fail.workflow.yaml", "params": {"WHO": "me"}}`)
	waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	writeFile(t, filepath.Join(dir, "triggers", "nightly.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\n"+
		"target: workflows/fail.workflow.yaml\nenvironment: dev\nparams: { WHO: me }\nwith: { schedule: \"0 3 * * *\" }\n")
	run = startRun(t, s, `{"trigger": "triggers/nightly.yaml"}`)
	waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })

	// A trigger run fails.
	id, err := s.startTrigger(trigger("params: { WHO: you }\n"), triggers.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	e := errorRuns(t, s, 1)[0]
	d := waitRun(t, s, e.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || d.Parent != id || d.Trigger != "error" || d.Environment != "dev" || d.Path != "workflows/report.workflow.yaml" {
		t.Errorf("error run %+v", d.Run)
	}
	if want := "run=" + id + " action=boom trigger=cron:nightly error=broken for you in dev"; !strings.Contains(logText(d), want) {
		t.Errorf("log %q, want %q", logText(d), want)
	}

	// The project is full; the trigger fails before its run starts (missing parameter):
	// reported without run id, and the error workflow is not limited.
	long := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "params": {"DELAY": "1h"}}`)
	if _, err := s.startTrigger(trigger(""), triggers.StartRequest{}); err == nil || !strings.Contains(err.Error(), "WHO") {
		t.Fatalf("start: %v", err)
	}
	e = errorRuns(t, s, 2)[0]
	d = waitRun(t, s, e.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	if d.Status != "succeeded" || d.Parent != "" || !strings.Contains(logText(d), "run= action= trigger=cron:nightly error=missing required parameter(s) [WHO]") {
		t.Errorf("error run %+v, log %q", d.Run, logText(d))
	}
	// A run beyond the limit is not reported.
	if _, err := s.startTrigger(trigger("params: { WHO: you }\n"), triggers.StartRequest{}); err == nil {
		t.Fatal("limit not reached")
	}
	expect(t, do(t, s, "POST", "/api/v1/runs/"+long.ID+"/cancel", ""), http.StatusAccepted, "cancelling")
	waitRun(t, s, long.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	time.Sleep(50 * time.Millisecond)
	errorRuns(t, s, 2)
}

// A failing error workflow is logged and starts no further error workflow.
func TestOnErrorFails(t *testing.T) {
	dir, s := newRunProject(t)
	var warnings []string
	done := make(chan struct{}, 1)
	s.warn = func(msg string) {
		if strings.HasPrefix(msg, "on error:") {
			warnings = append(warnings, msg)
			done <- struct{}{}
		}
	}
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\nsettings:\n  onError: workflows/fail.workflow.yaml\n")
	writeFile(t, filepath.Join(dir, "workflows", "fail.workflow.yaml"), strings.Replace(failWorkflow, "required: true", "default: x", 1))
	tr, err := model.ParseTrigger("triggers/t.yaml", []byte("kind: Trigger\napiVersion: runnel/v1\nname: t\ntype: interval\n"+
		"target: workflows/fail.workflow.yaml\nwith: { every: 1h }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.startTrigger(tr, triggers.StartRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no warning")
	}
	_ = s.Drain(context.Background())
	if len(warnings) != 1 || !strings.Contains(warnings[0], "on error: workflows/fail.workflow.yaml (run ") || !strings.Contains(warnings[0], "failed: broken for x") {
		t.Errorf("warnings %q", warnings)
	}
	errorRuns(t, s, 1)
}
