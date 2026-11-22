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

// FuzzPaste: pasted text (E16) never makes the editor panic, and what it accepts leaves a
// readable file.
func FuzzPaste(f *testing.F) {
	f.Add("transforms:\n  - id: a\n    type: core.dummy\nlinks:\n  - { from: a, to: a }\nlayout:\n  a: { x: 1, y: 2 }\n")
	f.Add("actions:\n  - id: x\n    type: core.log\n")
	f.Add("transforms:\n  - { id: a, type: core.switch, with: { cases: [ { to: a } ] } }\n")
	pipeline := []byte("kind: Pipeline\napiVersion: runnel/v1\nname: p\ntransforms:\n  - id: a\n    type: core.dummy\n")
	f.Fuzz(func(t *testing.T, text string) {
		out, _, err := PastePipeline("p.pipeline.yaml", pipeline, Paste{Text: text})
		if err == nil {
			if p, _ := ReadPipeline("p.pipeline.yaml", out); p == nil {
				t.Fatalf("accepted %q, but the result cannot be read:\n%s", text, out)
			}
		}
	})
}
