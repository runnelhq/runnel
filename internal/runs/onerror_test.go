package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
)

const failBody = `
actions:
  - { id: start, type: core.start }
  - { id: boom, type: core.abort, with: { message: broken } }
links:
  - { from: start, to: boom }
`

// onErrorProject writes a project with settings.onError and returns its folder.
func onErrorProject(t *testing.T, settings string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(model.ProjectFile, "kind: Project\napiVersion: runnel/v1\nname: p\nsettings:\n"+settings)
	write("workflows/on-error.workflow.yaml", "kind: Workflow\napiVersion: runnel/v1\nname: on-error\n"+
		"parameters:\n  - { name: FAILED_RUN }\n  - { name: FAILED_ACTION }\n  - { name: FAILED_ERROR }\n  - { name: OTHER, default: x }\n"+
		"actions:\n  - { id: start, type: core.start }\n")
	return dir
}

func TestOnErrorTarget(t *testing.T) {
	project := &model.Project{Settings: model.ProjectSettings{OnError: "workflows/on-error.workflow.yaml"}}
	for _, c := range []struct {
		trigger, own, path string
		no                 bool
		project            *model.Project
		want               string
	}{
		{"cron:a", "", "workflows/a.workflow.yaml", false, project, "workflows/on-error.workflow.yaml"},
		{"cli", "workflows/other.workflow.yaml", "workflows/a.workflow.yaml", false, project, "workflows/other.workflow.yaml"},
		{"cron:a", "none", "workflows/a.workflow.yaml", false, project, ""},
		{"cron:a", "", "workflows/a.workflow.yaml", false, nil, ""},
		{"", "", "workflows/a.workflow.yaml", false, project, ""},           // editor
		{TriggerError, "", "workflows/a.workflow.yaml", false, project, ""}, // no error workflow for error workflows
		{"cli", "", "workflows/a.workflow.yaml", true, project, ""},         // --no-on-error
		{"cron:a", "./workflows/a.workflow.yaml", "workflows/a.workflow.yaml", false, project, // names itself: ignored
			"workflows/on-error.workflow.yaml"},
	} {
		if got := onErrorOf(c.trigger, c.no, c.project, c.own, c.path); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}

// A failed run with a trigger reports its failure with the FAILED_… parameters; runs of
// the editor, of error workflows, with none and successful runs do not.
func TestOnFailed(t *testing.T) {
	dir := onErrorProject(t, "  onError: workflows/on-error.workflow.yaml\n")
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	got := make(chan Failure, 10)
	m.OnFailed(func(f Failure) { got <- f })
	start := func(trigger, body string, history string) Run {
		t.Helper()
		s := spec(body)
		s.ProjectDir, s.Trigger, s.History = dir, trigger, history
		s.Params = map[string]string{"DELAY": "1ms", "SECRET_PARAM": "x"}
		run, _, err := m.Start("workflows/a.workflow.yaml", s)
		if err != nil {
			t.Fatal(err)
		}
		m.Wait(context.Background(), run.ID) // a run without history is gone once it has ended
		return run
	}
	run := start("cron:nightly", failBody, HistoryNone) // also without history
	var f Failure
	select {
	case f = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no failure reported")
	}
	if f.Target != "workflows/on-error.workflow.yaml" || f.Action != "boom" || f.Run.ID != run.ID || f.Run.Params != nil {
		t.Errorf("failure %+v", f)
	}
	params := f.Params([]model.Parameter{{Name: "FAILED_RUN"}, {Name: "FAILED_ACTION"}, {Name: "FAILED_ERROR"},
		{Name: "FAILED_TRIGGER"}, {Name: "FAILED_FINISHED"}, {Name: "OTHER"}})
	if len(params) != 5 || params["FAILED_RUN"] != run.ID || params["FAILED_ACTION"] != "boom" || params["FAILED_TRIGGER"] != "cron:nightly" ||
		!strings.Contains(params["FAILED_ERROR"], "broken") || params["FAILED_FINISHED"] == "" {
		t.Errorf("params %v", params)
	}
	if _, err := time.Parse(time.RFC3339, params["FAILED_FINISHED"]); err != nil {
		t.Errorf("FAILED_FINISHED: %v", err)
	}

	start("", failBody, "")                                             // editor
	start(TriggerError, failBody, "")                                   // error workflow
	start("cron:nightly", "settings: { onError: none }\n"+failBody, "") // turned off
	start("cron:nightly", waitBody, "")                                 // succeeded
	_ = m.Drain(context.Background())
	if len(got) != 0 {
		t.Errorf("unexpected failure %+v", <-got)
	}
}

// Error workflows neither count towards nor are limited by settings.maxConcurrentRuns.
func TestErrorRunNotLimited(t *testing.T) {
	dir := onErrorProject(t, "  maxConcurrentRuns: 1\n")
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	s := spec(waitBody)
	s.ProjectDir, s.Params = dir, map[string]string{"DELAY": "1h"}
	if _, _, err := m.Start("workflows/a.workflow.yaml", s); err != nil {
		t.Fatal(err)
	}
	e := s
	e.Trigger, e.Parent = TriggerError, "01PARENT"
	run, _, err := m.Start("workflows/b.workflow.yaml", e)
	if err != nil || run.Parent != "01PARENT" {
		t.Fatalf("error run: %+v %v", run, err)
	}
	if _, _, err := m.Start("workflows/c.workflow.yaml", s); !errors.As(err, new(*LimitError)) || !strings.Contains(err.Error(), "1 run already") {
		t.Errorf("third run: %v", err)
	}
}

func TestStartFailure(t *testing.T) {
	dir := onErrorProject(t, "  onError: workflows/on-error.workflow.yaml\n")
	data := []byte("kind: Workflow\napiVersion: runnel/v1\nname: a\n" + failBody)
	if _, ok := StartFailure(dir, "workflows/a.workflow.yaml", data, "cron:a", "", false, &LimitError{"full"}); ok {
		t.Error("limit reported")
	}
	if _, ok := StartFailure(dir, "workflows/a.workflow.yaml", data, "cron:a", "", false, ErrShutdown); ok {
		t.Error("shutdown reported")
	}
	if _, ok := StartFailure(dir, "workflows/a.workflow.yaml", data, "", "", false, errors.New("x")); ok {
		t.Error("editor run reported")
	}
	f, ok := StartFailure(dir, "workflows/a.workflow.yaml", data, "cron:a", "prod", false, errors.New("secret TOKEN is not set"))
	if !ok || f.Run.ID != "" || f.Run.Name != "a" || f.Run.Environment != "prod" || f.Target != "workflows/on-error.workflow.yaml" ||
		f.Run.Error != "secret TOKEN is not set" || f.Run.FinishedAt == nil {
		t.Fatalf("%+v %v", f, ok)
	}
	spec := ErrorSpec(f, dir, filepath.Join(dir, "workflows", "on-error.workflow.yaml"), []byte("kind: Workflow\napiVersion: runnel/v1\nname: e\n"+
		"parameters:\n  - { name: FAILED_RUN }\n  - { name: FAILED_PATH }\nactions:\n  - { id: start, type: core.start }\n"))
	if spec.Trigger != TriggerError || spec.Parent != "" || spec.Env != "prod" || len(spec.Params) != 2 ||
		spec.Params["FAILED_RUN"] != "" || spec.Params["FAILED_PATH"] != "workflows/a.workflow.yaml" {
		t.Errorf("spec %+v", spec)
	}
}

// The queue runs one error workflow at a time, keeps MaxOnErrorQueue waiting and drops
// more with a warning; Close drops the waiting ones.
func TestErrorQueue(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var ran []string
	var warnings []string
	running, most := 0, 0
	q := NewErrorQueue(func(f Failure) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		<-release
		mu.Lock()
		running--
		ran = append(ran, f.Run.ID)
		mu.Unlock()
	}, func(msg string) {
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	})
	for i := range MaxOnErrorQueue + 2 {
		q.Add(Failure{Run: Run{ID: string(rune('A' + i)), Path: "workflows/a.workflow.yaml"}, Target: "workflows/e.workflow.yaml"})
	}
	mu.Lock()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "on-error queue full: workflows/e.workflow.yaml not started for run V of workflows/a.workflow.yaml") {
		t.Errorf("warnings %q", warnings)
	}
	mu.Unlock()
	for range MaxOnErrorQueue + 1 {
		release <- struct{}{}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(ran)
		mu.Unlock()
		if n == MaxOnErrorQueue+1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	if len(ran) != MaxOnErrorQueue+1 || most != 1 || ran[0] != "A" || ran[MaxOnErrorQueue] != "U" {
		t.Errorf("ran %v, at most %d at a time", ran, most)
	}
	mu.Unlock()

	// Close: the running one ends, the waiting ones are dropped.
	q.Add(Failure{Run: Run{ID: "X"}, Target: "e"})
	q.Add(Failure{Run: Run{ID: "Y"}, Target: "e"})
	q.Close()
	q.Add(Failure{Run: Run{ID: "Z"}, Target: "e"})
	release <- struct{}{}
	mu.Lock()
	defer mu.Unlock()
	if len(warnings) != 3 || !strings.Contains(warnings[1], "shutting down, e not started for run Y") || !strings.Contains(warnings[2], "for run Z") {
		t.Errorf("warnings %q", warnings)
	}
}

func TestCheckOnError(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		file := filepath.Join(dir, "workflows", name)
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return file
	}
	head := "kind: Workflow\napiVersion: runnel/v1\nname: w\nactions:\n  - { id: start, type: core.start }\n"
	write("ok.workflow.yaml", head+"parameters:\n  - { name: FAILED_RUN, type: string, required: true }\n  - { name: X, default: a }\n")
	write("extra.workflow.yaml", head+"parameters:\n  - { name: TO, required: true }\n")
	write("typed.workflow.yaml", head+"parameters:\n  - { name: FAILED_STARTED, type: int }\n")
	self := write("self.workflow.yaml", head)
	at := func(path, format string, args ...any) *model.Error {
		return &model.Error{Path: path, Msg: fmt.Sprintf(format, args...)}
	}
	warn := func(path, format string, args ...any) *model.Error {
		return &model.Error{Path: path, Severity: model.SeverityWarning, Msg: fmt.Sprintf(format, args...)}
	}
	for _, c := range []struct{ value, self, want string }{
		{"workflows/ok.workflow.yaml", "", ""},
		{"none", self, ""},
		{"workflows/missing.workflow.yaml", "", "error workflow workflows/missing.workflow.yaml not found"},
		{"workflows/extra.workflow.yaml", "", "required parameter TO is not given; an error workflow only gets FAILED_RUN"},
		{"workflows/typed.workflow.yaml", "", "parameter FAILED_STARTED must be of type string, not int"},
		{"workflows/self.workflow.yaml", self, "warning: onError names this workflow itself; it is ignored here"},
	} {
		got := ""
		for _, e := range CheckOnError(at, warn, c.value, dir, c.self) {
			if e.Path != "$.settings.onError" {
				t.Errorf("%s: path %s", c.value, e.Path)
			}
			if e.Severity == model.SeverityWarning {
				got += "warning: "
			}
			got += e.Msg
		}
		if c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.value, got, c.want)
		}
	}
}
