package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/runnelhq/runnel/internal/actions"
)

// The variables of a project for the list "Variables" of the editor (E18a): where each is
// set and where it is used.
func TestVariables(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\ndefaultEnvironment: dev\n"+
		"variables:\n  BASE: D:\\data\n  INBOX: ${BASE}\\inbox\n")
	writeFile(t, filepath.Join(dir, "environments/prod.yaml"), "kind: Environment\napiVersion: runnel/v1\nname: prod\nvariables:\n  BASE: \\\\server\\data\n")
	writeFile(t, filepath.Join(dir, "workflows/vars.workflow.yaml"), `kind: Workflow
apiVersion: runnel/v1
name: vars
parameters:
  - name: DAY
actions:
  - id: start
    type: core.start
  - id: log
    type: core.log
    with:
      message: "${INBOX} ${API_URL} ${DAY} ${env:TEMP} ${REGION:-eu}"
links:
  - { from: start, to: log }
`)
	writeFile(t, filepath.Join(dir, ".hidden/x.yaml"), "a: ${HIDDEN}\n")
	writeFile(t, filepath.Join(dir, "broken.yaml"), "a: [\n")

	rec := do(t, s, "GET", "/api/v1/variables", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got variablesInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Environments, ",") != "dev,prod" || got.DefaultEnvironment != "dev" || got.Truncated {
		t.Errorf("header %+v", got)
	}
	vars := map[string]variableInfo{}
	var names []string
	for _, v := range got.Variables {
		vars[v.Name] = v
		names = append(names, v.Name)
	}
	// DAY is a parameter; no ${env:…}, nothing from hidden folders or broken files.
	if strings.Join(names, ",") != "API_URL,BASE,DAY,INBOX,REGION,TARGET" {
		t.Fatalf("names %v", names)
	}
	base := vars["BASE"]
	if base.Project == nil || base.Project.Value != `D:\data` || base.Project.File != "project.yaml" || base.Project.Line != 6 ||
		base.Environments["prod"] == nil || base.Environments["prod"].Value != `\\server\data` || base.Environments["prod"].File != "environments/prod.yaml" ||
		base.Environments["dev"] != nil {
		t.Errorf("BASE %+v", base)
	}
	// A variable used in the value of another counts as a use.
	if u := base.Uses; len(u) != 1 || u[0] != (variableUse{File: "project.yaml", Line: 7, Column: 10}) {
		t.Errorf("uses of BASE %+v", u)
	}
	api := vars["API_URL"]
	if api.Project != nil || len(api.Environments) != 0 || len(api.Uses) != 1 || api.Uses[0].File != "workflows/vars.workflow.yaml" || api.Uses[0].Line != 12 || api.Uses[0].Parameter {
		t.Errorf("API_URL %+v", api)
	}
	if day := vars["DAY"]; len(day.Uses) != 1 || !day.Uses[0].Parameter {
		t.Errorf("DAY %+v", day)
	}
	// ${REGION:-eu} has a default (K1): the editor does not report it as missing.
	if r := vars["REGION"]; len(r.Uses) != 1 || !r.Uses[0].Default || r.Uses[0].Parameter {
		t.Errorf("REGION %+v", r)
	}
	// TARGET: set in dev only, used by the two workflows of the test project.
	if target := vars["TARGET"]; len(target.Uses) != 2 || target.Uses[0].File != "workflows/Archive.workflow.yaml" ||
		target.Environments["dev"] == nil || target.Environments["dev"].Line != 5 {
		t.Errorf("TARGET %+v", target)
	}
	expect(t, do(t, s, "GET", "/api/v1/variables", "", "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
}

// A project without environments: the list of environments is empty, not null, so that
// the editor can show project.yaml (examples/expressions).
func TestVariablesWithoutEnvironments(t *testing.T) {
	dir, s := newProject(t)
	if err := os.RemoveAll(filepath.Join(dir, "environments")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\nvariables:\n  LIMIT: \"10\"\n")
	rec := do(t, s, "GET", "/api/v1/variables", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if got := string(raw["environments"]); got != "[]" {
		t.Errorf("environments = %s, want []", got)
	}
}

func TestVariablesLimits(t *testing.T) {
	_, s := newProject(t)
	defer func(n int) { maxVariableFiles = n }(maxVariableFiles)
	maxVariableFiles = 1
	var got variablesInfo
	if err := json.Unmarshal(do(t, s, "GET", "/api/v1/variables", "").Body.Bytes(), &got); err != nil || !got.Truncated {
		t.Errorf("truncated: %+v, %v", got, err)
	}
}

func BenchmarkVariables(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "demo")
	writeFile(b, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\nvariables:\n  BASE: D:\\data\n")
	for i := range 200 {
		writeFile(b, filepath.Join(dir, "workflows", fmt.Sprintf("w%d.workflow.yaml", i)), workflowYAML)
	}
	s, err := New(Options{Dir: dir, Registry: actions.Builtin(), UI: fstest.MapFS{}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for range b.N {
		if rec := do(b, s, "GET", "/api/v1/variables", ""); rec.Code != http.StatusOK {
			b.Fatal(rec.Code)
		}
	}
}
