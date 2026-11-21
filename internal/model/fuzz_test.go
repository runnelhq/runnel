package model

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseWorkflow checks that arbitrary input never makes parsing, validation or
// the variable check panic.
func FuzzParseWorkflow(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "workflow", "*", "*.workflow.yaml"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		w, err := ParseWorkflow("fuzz.workflow.yaml", data)
		if err != nil {
			return
		}
		_ = w.Validate()
		_ = w.CheckVariables([]VarContext{{Scope: Scope(w, nil, nil, nil)}})
	})
}

// FuzzParsePipeline checks that arbitrary input never makes parsing or validation of a
// pipeline panic.
func FuzzParsePipeline(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*", "pipelines", "*.pipeline.yaml"))
	broken, _ := filepath.Glob(filepath.Join("..", "..", "examples", "broken", "*.pipeline.yaml"))
	for _, file := range append(files, broken...) {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		p, err := ParsePipeline("fuzz.pipeline.yaml", data)
		if err != nil {
			return
		}
		_ = p.Validate()
	})
}

// FuzzParseTrigger checks that arbitrary trigger files (cron expressions, durations,
// HTTP paths, auth) never make parsing panic and that routes can be compared.
func FuzzParseTrigger(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*", "triggers", "*.yaml"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		t, err := ParseTrigger("triggers/fuzz.yaml", data)
		if err != nil {
			return
		}
		_ = t.Describe()
		_ = t.Segments()
		_ = routesConflict(t, t)
	})
}
