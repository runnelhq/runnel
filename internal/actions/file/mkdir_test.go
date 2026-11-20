package file

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestMkdir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c")
	for range 2 { // an existing folder is fine
		got, err := run(t, mkdirDefinition, map[string]any{"path": path})
		if err != nil || len(got) != 1 || got[0] != path || !isFolder(path) {
			t.Fatalf("got %v, %v", got, err)
		}
	}
	tree(t, dir, map[string]string{"file": ""})
	if _, err := run(t, mkdirDefinition, map[string]any{"path": filepath.Join(dir, "file")}); err == nil || !strings.Contains(err.Error(), "a file with this name exists") {
		t.Errorf("err = %v", err)
	}
	if _, err := run(t, mkdirDefinition, map[string]any{"path": ""}); err == nil || !strings.Contains(err.Error(), "no path given") {
		t.Errorf("empty: %v", err)
	}
	actiontest.ConfigError(t, mkdirDefinition, nil, `missing required field "path"`)
}
