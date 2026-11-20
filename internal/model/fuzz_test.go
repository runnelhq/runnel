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
