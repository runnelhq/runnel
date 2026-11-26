package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProject(t *testing.T) {
	dir := filepath.Join("testdata", "project", "ok")
	p, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "ok" || p.DefaultEnvironment != "dev" || p.Variables["TARGET"] != "World" {
		t.Errorf("unexpected project: %+v", p)
	}
	if got := strings.Join(p.EnvironmentNames(), ","); got != "dev,prod" {
		t.Errorf("environments = %s, want dev,prod", got)
	}

	env, err := p.SelectEnvironment("")
	if err != nil || env == nil || env.Name != "dev" {
		t.Errorf("default environment: got (%v, %v)", env, err)
	}
	if env, err := p.SelectEnvironment("prod"); err != nil || env.Name != "prod" {
		t.Errorf("prod: got (%v, %v)", env, err)
	}
	if _, err := p.SelectEnvironment("staging"); err == nil || !strings.Contains(err.Error(), "available: dev, prod") {
		t.Errorf("unknown environment: got %v", err)
	}
}

func TestLoadProjectInvalid(t *testing.T) {
	_, err := LoadProject(filepath.Join("testdata", "project", "invalid"))
	if _, ok := errors.AsType[Errors](err); !ok {
		t.Fatalf("want Errors, got %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "project", "invalid.golden"), []byte(err.Error()+"\n"))
}

func TestLoadProjectMissing(t *testing.T) {
	if _, err := LoadProject(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want os.ErrNotExist, got %v", err)
	}
}

func TestFindProjectDir(t *testing.T) {
	want, err := filepath.Abs(filepath.Join("testdata", "project", "ok"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := FindProjectDir(filepath.Join("testdata", "project", "ok", "workflows"))
	if err != nil || !ok || got != want {
		t.Errorf("FindProjectDir = (%q, %v, %v), want %q", got, ok, err, want)
	}
	if _, ok, err := FindProjectDir(t.TempDir()); ok || err != nil {
		t.Errorf("outside a project: ok=%v err=%v", ok, err)
	}
}

func TestScopeAndCheckVariables(t *testing.T) {
	dir := filepath.Join("testdata", "project", "ok")
	p, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := LoadWorkflow(filepath.Join(dir, "workflows", "vars.workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Precedence: parameters > environment > project > workflow parameter defaults.
	scope := Scope(w, p, p.Environments["dev"], map[string]string{"IMPORT_DATE": "2026-01-31"})
	for name, want := range map[string]string{
		"TARGET":      "Dev",                   // environment overrides project
		"INBOX":       `D:\data\inbox`,         // project, expanded
		"IMPORT_DATE": "2026-01-31",            // parameter overrides default
		"API_URL":     "http://localhost:8080", // environment only
	} {
		if got, err := scope.Expand("${" + name + "}"); err != nil || got != want {
			t.Errorf("dev: ${%s} = (%q, %v), want %q", name, got, err, want)
		}
	}
	prod := Scope(w, p, p.Environments["prod"], nil)
	if got, _ := prod.Expand("${INBOX}"); got != `\\server\share\inbox` {
		t.Errorf("prod: ${INBOX} = %q", got)
	}
	if got, _ := prod.Expand("${IMPORT_DATE}"); !strings.HasPrefix(got, "{{") {
		t.Errorf("prod: ${IMPORT_DATE} should be the parameter default, got %q", got)
	}

	contexts := []VarContext{
		{Environment: "dev", Scope: Scope(w, p, p.Environments["dev"], nil)},
		{Environment: "prod", Scope: prod},
	}
	compareGolden(t, filepath.Join("testdata", "project", "ok-variables.golden"),
		[]byte(w.CheckVariables(contexts).Error()+"\n"))
}

// settings.history limits the run history; 0 or missing means the defaults (S26c).
func TestHistorySettings(t *testing.T) {
	const head = "kind: Project\napiVersion: runnel/v1\nname: p\n"
	p, err := ReadProject("project.yaml", []byte(head+"settings:\n  history:\n    keepDays: 7\n    maxRuns: 500\n    cli: errors\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d, r := p.Settings.History.Retention(); d != 7 || r != 500 || p.Settings.History.CLI != HistoryErrors {
		t.Errorf("%d %d %+v", d, r, p.Settings.History)
	}
	if d, r := (HistorySettings{}).Retention(); d != DefaultHistoryDays || r != DefaultHistoryRuns {
		t.Errorf("defaults %d %d", d, r)
	}
	_, err = ReadProject("project.yaml", []byte(head+"settings:\n  history:\n    keepDays: -1\n    maxRuns: 2000000\n"), nil)
	for _, want := range []string{"project.yaml:6:15: keepDays must be between 1 and 3650", "project.yaml:7:14: maxRuns must be between 1 and 1000000"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v, want %q", err, want)
		}
	}
}

// The default join of the project is settings.defaultJoin (R5b); the old block workflow
// is an error that names the new place.
func TestDefaultJoinSettings(t *testing.T) {
	const head = "kind: Project\napiVersion: runnel/v1\nname: p\n"
	p, err := ReadProject("project.yaml", []byte(head+"settings:\n  defaultJoin: first\n"), nil)
	if err != nil || p.Settings.DefaultJoin != JoinFirst {
		t.Fatalf("%v %+v", err, p)
	}
	_, err = ReadProject("project.yaml", []byte(head+"workflow:\n  defaultJoin: first\n"), nil)
	if err == nil || !strings.Contains(err.Error(), `project.yaml:4:1: field "workflow" was renamed before 1.0: use settings.defaultJoin`) {
		t.Errorf("old block: %v", err)
	}
}

// settings.onError names a workflow of the project (ADR 0027); none only in workflows
// and pipelines.
func TestOnErrorSettings(t *testing.T) {
	const head = "kind: Project\napiVersion: runnel/v1\nname: p\nsettings:\n  onError: "
	p, err := ReadProject("project.yaml", []byte(head+"workflows/on-error.workflow.yaml\n"), nil)
	if err != nil || OnErrorTarget(p, "") != "workflows/on-error.workflow.yaml" || OnErrorTarget(p, "none") != "" ||
		OnErrorTarget(p, "./workflows/b.workflow.yaml") != "workflows/b.workflow.yaml" {
		t.Fatalf("%+v %v", p.Settings, err)
	}
	// An invalid setting (a project with errors) names no workflow.
	if got := OnErrorTarget(&Project{Settings: ProjectSettings{OnError: "../x.workflow.yaml"}}, ""); got != "" {
		t.Errorf("invalid setting: %q", got)
	}
	for value, want := range map[string]string{
		"none":                               "project.yaml:5:12: onError: none is only for workflows and pipelines",
		"/abs/x.workflow.yaml":               "project.yaml:5:12: onError \"/abs/x.workflow.yaml\" must be a path inside the project",
		"../x.workflow.yaml":                 "must be a path inside the project",
		"pipelines/a.pipeline.yaml":          "must be a workflow file (*.workflow.yaml), not a pipeline",
		"workflows/${NAME}.workflow.yaml":    "variables and expressions are not allowed here",
		"workflows/{{ x }}.workflow.yaml":    "variables and expressions are not allowed here",
		"workflows/../../x.workflow.yaml":    "must be a path inside the project",
		`C:\flows\x.workflow.yaml`:           "must be a path inside the project",
		"workflows\\..\\..\\x.workflow.yaml": "must be a path inside the project",
	} {
		_, err := ReadProject("project.yaml", []byte(head+"'"+value+"'\n"), nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", value, err, want)
		}
	}

	_, err = ParseWorkflow("a.workflow.yaml", []byte("kind: Workflow\napiVersion: runnel/v1\nname: a\nsettings:\n  onError: x.pipeline.yaml\n"+
		"actions:\n  - { id: start, type: core.start }\n"))
	if err == nil || !strings.Contains(err.Error(), "a.workflow.yaml:5:12: onError \"x.pipeline.yaml\" must be a workflow file") {
		t.Errorf("workflow: %v", err)
	}
	pl, err := ParsePipeline("a.pipeline.yaml", []byte("kind: Pipeline\napiVersion: runnel/v1\nname: a\nsettings:\n  onError: none\n"))
	if err != nil {
		t.Fatal(err)
	}
	if errs := pl.Validate(); strings.Contains(errs.Error(), "onError") || pl.Settings.OnError != OnErrorNone {
		t.Errorf("pipeline: %v", errs)
	}
}
