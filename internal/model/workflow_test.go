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
