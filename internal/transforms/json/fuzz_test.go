package json

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

// FuzzInput checks that arbitrary file contents never make json.input or "Detect
// fields" panic: broken JSON, deep nesting, byte order marks, both formats.
func FuzzInput(f *testing.F) {
	f.Add([]byte(`[{"a": 1, "b": {"c": [1, 2.5, null]}}]`), false)
	f.Add([]byte("{\"a\":1}\n{\"a\":\n"), true)
	f.Add([]byte("\xEF\xBB\xBF{\"data\": {\"items\": [{}]}}"), false)
	f.Fuzz(func(t *testing.T, data []byte, nd bool) {
		path := filepath.Join(t.TempDir(), "in.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		with := map[string]any{"path": path, "format": map[bool]string{true: "ndjson", false: "json"}[nd]}
		tr := transformtest.New(t, inputDefinition, with)
		_, _, _, _ = transformtest.RunWithErrorLink(context.Background(), tr)
		_, _ = detectFields(context.Background(), with, 100)
	})
}
