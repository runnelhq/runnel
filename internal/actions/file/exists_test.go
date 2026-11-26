package file

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestExists(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.csv": "1", "b.csv": "2", "sub/x.txt": ""})
	got, err := run(t, existsDefinition, map[string]any{"path": filepath.Join(dir, "*.csv")})
	if err != nil || names(got) != "a.csv,b.csv" {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := run(t, existsDefinition, map[string]any{"path": filepath.Join(dir, "sub"), "include": "folders"}); err != nil {
		t.Errorf("folder: %v", err)
	}
	_, err = run(t, existsDefinition, map[string]any{"path": filepath.Join(dir, "sub"), "include": "files"})
	if err == nil || !strings.Contains(err.Error(), "no file matches") {
		t.Errorf("folder as file: %v", err)
	}
	_, err = run(t, existsDefinition, map[string]any{"path": filepath.Join(dir, "*.xml")})
	if err == nil || !strings.Contains(err.Error(), "no file or folder matches") {
		t.Errorf("missing: %v", err)
	}
	if _, err := run(t, existsDefinition, map[string]any{"path": dir, "include": "${WHAT}"}); err == nil || !strings.Contains(err.Error(), `invalid include "${WHAT}"`) {
		t.Errorf("unresolved include: %v", err)
	}
}

func TestExistsConfig(t *testing.T) {
	actiontest.ConfigError(t, existsDefinition, nil, `missing required field "path"`)
	actiontest.ConfigError(t, existsDefinition, map[string]any{"path": "x", "type": "file"}, `unknown field "type"`)
	actiontest.ConfigError(t, existsDefinition, map[string]any{"path": "x", "include": "file"}, `invalid include "file" (expected all, files or folders)`)
	// kind is include since R5b, as in file.list.
	actiontest.ConfigError(t, existsDefinition, map[string]any{"path": "x", "kind": "file"}, `unknown field "kind"`)
}
