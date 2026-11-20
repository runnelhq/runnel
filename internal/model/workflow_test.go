package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "update golden files")

// TestWorkflowGolden parses every file in testdata/workflow/{valid,invalid} and compares
// the result (parsed workflow as JSON, or the error messages) with a .golden file.
// Run "go test ./internal/model -update" to regenerate the golden files after an intended change.
func TestWorkflowGolden(t *testing.T) {
	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", "workflow", dir, "*.workflow.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no test files in %s", dir)
		}
		for _, file := range files {
			name := filepath.Base(file)
			t.Run(dir+"/"+name, func(t *testing.T) {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				w, err := ParseWorkflow(name, data)

				var got []byte
				if dir == "valid" {
					if err != nil {
						t.Fatalf("unexpected error:\n%v", err)
					}
					got, err = json.MarshalIndent(w, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
				} else {
					if err == nil {
						t.Fatal("expected an error, got none")
					}
					var errs Errors
					if !errors.As(err, &errs) {
						t.Fatalf("error is %T, want Errors", err)
					}
					got = []byte(err.Error())
				}
				compareGolden(t, strings.TrimSuffix(file, ".workflow.yaml")+".golden", append(got, '\n'))
			})
		}
	}
}

// TestWorkflowValidateGolden runs the graph validation on every file in testdata/workflow/graph
// and compares the diagnostics with a .golden file. Files in testdata/workflow/valid must have none.
func TestWorkflowValidateGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "workflow", "graph", "*.workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			w, err := ParseWorkflow(name, data)
			if err != nil {
				t.Fatalf("parse error:\n%v", err)
			}
			got := "no problems"
			if diags := w.Validate(); len(diags) > 0 {
				got = diags.Error()
			}
			compareGolden(t, strings.TrimSuffix(file, ".workflow.yaml")+".golden", []byte(got+"\n"))
		})
	}

	valid, _ := filepath.Glob(filepath.Join("testdata", "workflow", "valid", "*.workflow.yaml"))
	for _, file := range valid {
		w, err := LoadWorkflow(file)
		if err != nil {
			t.Fatal(err)
		}
		if diags := w.Validate(); len(diags) > 0 {
			t.Errorf("%s: expected no problems, got:\n%v", file, diags)
		}
	}
}

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"load", "load", 0},
		{"laod", "load", 2},
		{"lod", "load", 1},
		{"archive", "arch", 3},
		{"über", "uber", 1},
	}
	for _, tt := range tests {
		if got := editDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestErrorsHasErrors(t *testing.T) {
	warn := &Error{Severity: SeverityWarning, Msg: "w"}
	if (Errors{warn}).HasErrors() {
		t.Error("only warnings: HasErrors() = true")
	}
	if !(Errors{warn, {Msg: "e"}}).HasErrors() {
		t.Error("with error: HasErrors() = false")
	}
}

func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("result differs from %s (run with -update if intended)\n--- got:\n%s\n--- want:\n%s", path, got, want)
	}
}

func TestLoadWorkflow(t *testing.T) {
	w, err := LoadWorkflow(filepath.Join("testdata", "workflow", "valid", "minimal.workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "minimal" || len(w.Actions) != 1 {
		t.Errorf("unexpected workflow: %+v", w)
	}

	_, err = LoadWorkflow(filepath.Join("testdata", "workflow", "invalid", "unknown-field.workflow.yaml"))
	if err == nil || !strings.HasPrefix(err.Error(), "testdata/workflow/invalid/unknown-field.workflow.yaml:6:5: ") {
		t.Errorf("error should start with the file path and position, got: %v", err)
	}

	if _, err := LoadWorkflow(filepath.Join("testdata", "does-not-exist.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want os.ErrNotExist, got %v", err)
	}
}

func TestLinkCondition(t *testing.T) {
	tests := []struct {
		when     string
		wantKind ConditionKind
		wantExpr string
	}{
		{"", OnSuccess, ""},
		{"success", OnSuccess, ""},
		{"failure", OnFailure, ""},
		{"always", Always, ""},
		{"{{ result.rows > 0 }}", OnExpression, "result.rows > 0"},
		{"  {{result.rows>0}}  ", OnExpression, "result.rows>0"},
		{"{{ }}", "", ""},
		{"{{ a", "", ""},
		{"Success", "", ""},
		{"result.rows > 0", "", ""},
	}
	for _, tt := range tests {
		kind, expr := Link{When: tt.when}.Condition()
		if kind != tt.wantKind || expr != tt.wantExpr {
			t.Errorf("Condition(%q) = (%q, %q), want (%q, %q)", tt.when, kind, expr, tt.wantKind, tt.wantExpr)
		}
	}
}

func TestDurationValue(t *testing.T) {
	tests := []struct {
		in      Duration
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"30s", 30 * time.Second, false},
		{"1h30m", 90 * time.Minute, false},
		{"30", 0, true},
		{"2x", 0, true},
		{"-1m", 0, true},
	}
	for _, tt := range tests {
		got, err := tt.in.Value()
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("Duration(%q).Value() = (%v, %v), want (%v, err=%v)", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestParentPath(t *testing.T) {
	for in, want := range map[string]string{
		"$.actions[1].retry.attempts": "$.actions[1].retry",
		"$.actions[1]":                "$.actions",
		"$.actions":                   "$",
		"$":                           "",
	} {
		if got := parentPath(in); got != want {
			t.Errorf("parentPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateExpressions(t *testing.T) {
	src := `kind: Workflow
apiVersion: runnel/v1
name: x
parameters:
  - { name: DAY, default: "{{ formatDate(today(), 'yyyy-MM-dd') }}" }
  - { name: BAD, default: "{{ today( }}" }
outputs:
  - { name: ROWS, value: "{{ results.load.rows }}" }
  - { name: OOPS, value: "{{ reslt.rows }}" }
actions:
  - id: start
    type: core.start
  - id: load
    type: core.log
    with:
      message: "rows: {{ result.rows }}, day {{ params.DAY }}"
      list: ["{{ 1 + }}"]
links:
  - { from: start, to: load, when: "{{ vars.X == 'y' }}" }
  - { from: load, to: start, loop: true, when: "{{ addDays() }}" }
`
	_, err := ParseWorkflow("x.workflow.yaml", []byte(src))
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("err = %v", err)
	}
	var got []string
	for _, e := range errs {
		got = append(got, e.Error())
	}
	want := []string{
		`x.workflow.yaml:6:27: parameter "BAD": default value: expression {{ today( }}: unexpected token EOF`,
		`x.workflow.yaml:9:26: output "OOPS": expression {{ reslt.rows }}: unknown name reslt`,
		`x.workflow.yaml:17:14: action "load": expression {{ 1 + }}: unexpected token EOF`,
		`x.workflow.yaml:20:48: link #2: expression {{ addDays() }}: not enough arguments to call addDays`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d problems:\n%s", len(got), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("problem %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}
