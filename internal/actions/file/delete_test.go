package file

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.tmp": "", "b.tmp": "", "keep.csv": "", "old/x.txt": ""})
	got, err := run(t, deleteDefinition, map[string]any{"path": filepath.Join(dir, "*.tmp")})
	if err != nil || names(got) != "a.tmp,b.tmp" || present(filepath.Join(dir, "a.tmp")) || !present(filepath.Join(dir, "keep.csv")) {
		t.Fatalf("got %v, %v", got, err)
	}
	// Nothing to delete is fine, unless ifMissing: fail is set.
	if got, err := run(t, deleteDefinition, map[string]any{"path": filepath.Join(dir, "*.tmp")}); err != nil || len(got) != 0 {
		t.Errorf("nothing to delete: %v %v", got, err)
	}
	if _, err := run(t, deleteDefinition, map[string]any{"path": filepath.Join(dir, "*.tmp"), "ifMissing": "fail"}); err == nil || !strings.Contains(err.Error(), "no file or folder matches") {
		t.Errorf("ifMissing: %v", err)
	}
	if err := configError(deleteDefinition, map[string]any{"path": "a", "mustExist": true}); err == nil ||
		err.Error() != `unknown field "mustExist" (renamed to ifMissing: mustExist: true is now ifMissing: fail)` {
		t.Errorf("mustExist: %v", err)
	}
	if err := configError(deleteDefinition, map[string]any{"path": "a", "ifMissing": "empty"}); err == nil || !strings.Contains(err.Error(), `invalid ifMissing "empty" (expected skip or fail)`) {
		t.Errorf("empty: %v", err)
	}
	// Folders only with recursive.
	_, err = run(t, deleteDefinition, map[string]any{"path": filepath.Join(dir, "old")})
	if err == nil || !strings.Contains(err.Error(), "set recursive: true") || !present(filepath.Join(dir, "old", "x.txt")) {
		t.Errorf("folder without recursive: %v", err)
	}
	tree(t, dir, map[string]string{"z.txt": ""}) // nothing is deleted when a match is a folder
	if _, err := run(t, deleteDefinition, map[string]any{"path": filepath.Join(dir, "*")}); err == nil || !present(filepath.Join(dir, "z.txt")) {
		t.Errorf("wildcard with a folder: %v", err)
	}
	if _, err := run(t, deleteDefinition, map[string]any{"path": filepath.Join(dir, "old"), "recursive": true}); err != nil || present(filepath.Join(dir, "old")) {
		t.Errorf("recursive: %v", err)
	}
	actiontest.ConfigError(t, deleteDefinition, nil, `missing required field "path"`)
}
