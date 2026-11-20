package model

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPipelineGolden parses every file in testdata/pipeline/{valid,invalid} and compares
// the result (parsed pipeline as JSON, or the error messages) with a .golden file.
func TestPipelineGolden(t *testing.T) {
	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", "pipeline", dir, "*.pipeline.yaml"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no test files in %s: %v", dir, err)
		}
		for _, file := range files {
			name := filepath.Base(file)
			t.Run(dir+"/"+name, func(t *testing.T) {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				p, err := ParsePipeline(name, data)
				var got []byte
				if dir == "valid" {
					if err != nil {
						t.Fatalf("unexpected error:\n%v", err)
					}
					if got, err = json.MarshalIndent(p, "", "  "); err != nil {
						t.Fatal(err)
					}
					if diags := p.Validate(); len(diags) > 0 {
						t.Errorf("expected no problems, got:\n%v", diags)
					}
				} else {
					var errs Errors
					if !errors.As(err, &errs) {
						t.Fatalf("error is %T (%v), want Errors", err, err)
					}
					got = []byte(err.Error())
				}
				compareGolden(t, strings.TrimSuffix(file, ".pipeline.yaml")+".golden", append(got, '\n'))
			})
		}
	}
}

// TestPipelineValidateGolden runs the graph validation on every file in testdata/pipeline/graph.
func TestPipelineValidateGolden(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "pipeline", "graph", "*.pipeline.yaml"))
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			p, err := LoadPipeline(file)
			if err != nil {
				t.Fatalf("parse error:\n%v", err)
			}
			got := "no problems"
			if diags := p.Validate(); len(diags) > 0 {
				got = diags.Error()
			}
			compareGolden(t, strings.TrimSuffix(file, ".pipeline.yaml")+".golden", []byte(got+"\n"))
		})
	}
}

func TestLoadPipelineMissingFile(t *testing.T) {
	if _, err := LoadPipeline(filepath.Join(t.TempDir(), "none.pipeline.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v", err)
	}
}
