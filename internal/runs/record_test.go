package runs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/history"
)

// withHistory returns a manager that keeps its runs in a history in dir.
func withHistory(t *testing.T, dir string) (*Manager, *history.Store, *[]string) {
	t.Helper()
	store, err := history.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	m := NewManager()
	m.SetHistory(store, func(msg string) { warnings = append(warnings, msg) })
	t.Cleanup(func() {
		_ = m.Shutdown(context.Background())
		m.CloseHistory()
		_ = store.Close()
	})
	return m, store, &warnings
}

// stop ends a manager like a server that stops: its runs are written to the history.
func stop(t *testing.T, m *Manager, store *history.Store) {
	t.Helper()
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.CloseHistory()
	_ = store.Close()
}

// A run is kept in the history with its actions and log; after a restart the API reads
// it from there, as it was shown before (ADR 0026).
func TestRunsInHistory(t *testing.T) {
	dir := t.TempDir()
	m, store, warnings := withHistory(t, dir)
	s := spec(waitBody)
	s.Params, s.Trigger = map[string]string{"DELAY": "5ms"}, "cron:nightly"
	run, _, err := m.Start("workflows/test.workflow.yaml", s)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := m.Wait(context.Background(), run.ID)
	stop(t, m, store)

	m2, _, _ := withHistory(t, dir)
	list := list(m2)
	if len(list) != 1 || list[0].ID != run.ID || list[0].Status != workflow.Succeeded || list[0].Trigger != "cron:nightly" ||
		list[0].Params["DELAY"] != "5ms" || list[0].FinishedAt == nil || list[0].Name != "test" {
		t.Fatalf("list %+v", list)
	}
	after, ok := get(m2, run.ID)
	if !ok {
		t.Fatal("not in the history")
	}
	if len(after.Actions) != len(before.Actions) || len(after.Log) != len(before.Log) || len(after.Log) == 0 {
		t.Fatalf("actions %d/%d, log %d/%d", len(after.Actions), len(before.Actions), len(after.Log), len(before.Log))
	}
	for i, a := range before.Actions {
		b := after.Actions[i]
		if a.ID != b.ID || a.Status != b.Status || a.Runs != b.Runs || a.Error != b.Error {
			t.Errorf("action %d: %+v, want %+v", i, b, a)
		}
	}
	for i, l := range before.Log {
		if after.Log[i].Message != l.Message || after.Log[i].Level != l.Level || after.Log[i].ActionID != l.ActionID {
			t.Errorf("log %d: %+v, want %+v", i, after.Log[i], l)
		}
	}
	if len(*warnings) != 0 {
		t.Errorf("warnings %v", *warnings)
	}
}

// history: errors keeps only failed runs, none keeps nothing; as in memory (S38b2).
func TestHistoryModes(t *testing.T) {
	dir := t.TempDir()
	m, store, _ := withHistory(t, dir)
	failing := spec("actions:\n  - { id: start, type: core.start }\n  - { id: stop, type: core.abort }\nlinks:\n  - { from: start, to: stop }\n")
	ids := map[string]string{}
	for _, c := range []struct {
		name, history string
		s             Spec
	}{
		{"ok-errors", HistoryErrors, spec(waitBody)},
		{"failed-errors", HistoryErrors, failing},
		{"failed-none", HistoryNone, failing},
		{"ok-all", "", spec(waitBody)},
	} {
		c.s.Params, c.s.History = map[string]string{"DELAY": "1ms"}, c.history
		run, _, err := m.Start("workflows/"+c.name+".workflow.yaml", c.s)
		if err != nil {
			t.Fatal(err)
		}
		m.Wait(context.Background(), run.ID)
		ids[c.name] = run.ID
	}
	stop(t, m, store)
	m2, _, _ := withHistory(t, dir)
	kept := map[string]bool{}
	for _, r := range list(m2) {
		kept[r.ID] = true
	}
	for name, want := range map[string]bool{"ok-errors": false, "failed-errors": true, "failed-none": false, "ok-all": true} {
		if kept[ids[name]] != want {
			t.Errorf("%s: kept %v, want %v", name, kept[ids[name]], want)
		}
	}
}

// A run of a process that stopped while it ran is failed after StaleAfter; the API
// shows its actions without "running".
func TestStaleRun(t *testing.T) {
	dir := t.TempDir()
	store, err := history.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	r := Run{ID: "01KSTALE", Path: "workflows/a.workflow.yaml", Kind: "workflow", Name: "a", Status: StatusRunning, StartedAt: time.Now().Add(-time.Hour)}
	rec := newHistoryWriter(store, nil)
	rec.send(recOp{kind: recRun, run: r})
	rec.send(recOp{kind: recNode, run: Run{ID: r.ID}, index: 0, node: ActionState{ID: "wait", Type: "core.wait", Status: StatusRunning, Runs: 1}})
	rec.close()
	_ = store.Close()
	// Its sign of life is old: the process ended without ending the run.
	db, err := history.Open(context.Background(), dir) // marks nothing yet: the run just wrote
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkStale(context.Background(), time.Now().Add(history.StaleAfter+time.Minute)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	m, _, _ := withHistory(t, dir)
	d, ok := get(m, r.ID)
	if !ok || d.Status != workflow.Failed || d.Error != history.StaleError || d.FinishedAt == nil || len(d.Actions) != 1 || d.Actions[0].Status != workflow.Failed {
		t.Errorf("%v %+v %+v", ok, d.Run, d.Actions)
	}
}

// Secrets never reach the history file: log, errors and outputs are masked before
// (ADR 0022, ADR 0026).
func TestHistoryMasksSecrets(t *testing.T) {
	t.Setenv("RUNNEL_TEST_PW", "Pa55word!")
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\n")
	dir := t.TempDir()
	m, store, _ := withHistory(t, dir)
	for _, user := range []string{"ada", "fail"} {
		wf := strings.Replace(loginWorkflow, "${USER}", user, 1)
		run, _, err := m.Start("workflows/login.workflow.yaml", Spec{Name: "workflows/login.workflow.yaml", Data: []byte(wf), ProjectDir: project,
			Registry: secretRegistry()})
		if err != nil {
			t.Fatal(err)
		}
		m.Wait(context.Background(), run.ID)
	}
	stop(t, m, store)
	for _, name := range []string{history.FileName, history.FileName + "-wal"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("Pa55word!")) {
			t.Errorf("%s holds the secret", name)
		}
	}
	m2, _, _ := withHistory(t, dir)
	runs := list(m2)
	if len(runs) != 2 || runs[1].Outputs["SESSION"] != "s-***" || !strings.Contains(runs[0].Error, "wrong password ***") {
		t.Errorf("runs %+v", runs)
	}
}

// A history that cannot be written does not hold up or fail a run; it is reported once.
func TestHistoryWriteErrors(t *testing.T) {
	dir := t.TempDir()
	m, store, warnings := withHistory(t, dir)
	_ = store.Close() // every write fails
	for range 2 {
		s := spec(waitBody)
		s.Params = map[string]string{"DELAY": "1ms"}
		run, _, err := m.Start("workflows/test.workflow.yaml", s)
		if err != nil {
			t.Fatal(err)
		}
		if d, _ := m.Wait(context.Background(), run.ID); d.Status != workflow.Succeeded {
			t.Fatalf("run %s", d.Status)
		}
	}
	m.CloseHistory()
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "cannot write the run history") {
		t.Errorf("warnings %v", *warnings)
	}
	if _, err := m.Runs(context.Background()); err == nil {
		t.Error("reading a closed history reports no error")
	}
}

func TestMergeRuns(t *testing.T) {
	mem := []Run{{ID: "05"}, {ID: "03"}}
	stored := []Run{{ID: "04"}, {ID: "03"}, {ID: "02"}, {ID: "01"}}
	got := mergeRuns(mem, stored, 4)
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "05,04,03,02" {
		t.Errorf("%v", ids)
	}
}

// Many short runs at once (e.g. an http trigger under load) are all kept, with their
// log, and nothing is reported.
func TestHistoryManyRuns(t *testing.T) {
	dir := t.TempDir()
	m, store, warnings := withHistory(t, dir)
	body := "actions:\n  - { id: start, type: core.start }\n  - { id: log, type: core.log, with: { message: \"line {{ params.N }}\" } }\n" +
		"links:\n  - { from: start, to: log }\nparameters:\n  - { name: N }\n"
	const n = 300
	ids := make(chan string, n)
	for i := range n {
		go func() {
			s := spec(body)
			s.Params = map[string]string{"N": fmt.Sprint(i)}
			run, _, err := m.Start("workflows/many.workflow.yaml", s)
			if err != nil {
				ids <- ""
				return
			}
			m.Wait(context.Background(), run.ID)
			ids <- run.ID
		}()
	}
	var all []string
	for range n {
		if id := <-ids; id != "" {
			all = append(all, id)
		}
	}
	stop(t, m, store)
	if len(*warnings) != 0 {
		t.Fatalf("warnings %v", *warnings)
	}
	m2, _, _ := withHistory(t, dir)
	for _, id := range all {
		d, ok := get(m2, id)
		if !ok || d.Status != workflow.Succeeded || len(d.Actions) != 2 || !strings.Contains(fmt.Sprint(d.Log), "line ") {
			t.Fatalf("%s: %v %+v %+v", id, ok, d.Run, d.Log)
		}
	}
	if len(all) != n {
		t.Errorf("%d of %d runs started", len(all), n)
	}
}

// The history is written in the background and may still say "running" for a run that
// has just ended in memory: a filter by status follows the state in memory, so that the
// run is neither counted as running (the count in the header of the editor) nor missing.
func TestFindPrefersMemoryOverLaggingHistory(t *testing.T) {
	m, store, _ := withHistory(t, t.TempDir())
	s := spec(waitBody)
	s.Params = map[string]string{"DELAY": "0s"}
	run, _, err := m.Start("workflows/test.workflow.yaml", s)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := m.Wait(context.Background(), run.ID); d.Status != workflow.Succeeded {
		t.Fatalf("run %+v", d.Run)
	}
	// Wait until the writer has stored the end, then put the run back as running: what
	// a reader sees while the writer is behind.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if h, ok, _ := store.Get(context.Background(), run.ID); ok && h.Run.Status == string(workflow.Succeeded) {
			stale := h.Run
			stale.Status, stale.Finished = string(StatusRunning), nil
			if err := store.PutRun(context.Background(), stale); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the end of the run was not written")
		}
	}
	if got, err := m.Find(context.Background(), history.Filter{Status: string(StatusRunning)}); err != nil || len(got) != 0 {
		t.Errorf("running: %v %+v", err, got)
	}
	if got, err := m.Find(context.Background(), history.Filter{Status: string(workflow.Succeeded)}); err != nil || len(got) != 1 || got[0].ID != run.ID {
		t.Errorf("succeeded: %v %+v", err, got)
	}
}
