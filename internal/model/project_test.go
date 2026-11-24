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
