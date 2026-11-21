package server

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestExpressions(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "project.yaml"),
		"kind: Project\napiVersion: runnel/v1\nname: demo\ndefaultEnvironment: dev\nvariables:\n  TARGET: world\n  LIMIT: \"10\"\n")
	var res expressionsResponse
	rec := do(t, s, "GET", "/api/v1/expressions", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	if len(res.Roots) != 8 || len(res.Functions) == 0 || res.Functions[0].Signature == "" {
		t.Errorf("roots %d, functions %v", len(res.Roots), res.Functions)
	}
	// The default environment overrides the project.
	want := []variableDoc{{"LIMIT", "10", "project.yaml"}, {"TARGET", "dev", "environments/dev.yaml"}}
	if len(res.Variables) != 2 || res.Variables[0] != want[0] || res.Variables[1] != want[1] {
		t.Errorf("variables %+v, want %+v", res.Variables, want)
	}

	// A broken project still gives the reference, without variables.
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\nname: [")
	rec = do(t, s, "GET", "/api/v1/expressions", "")
	res = expressionsResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != 200 || len(res.Functions) == 0 || len(res.Variables) != 0 {
		t.Errorf("broken project: %d %s", rec.Code, rec.Body)
	}
}
