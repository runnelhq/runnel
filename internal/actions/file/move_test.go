package file

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestMove(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.csv": "A", "in/b.csv": "B", "in/sub/x.txt": "X", "archive/b.csv": "old"})
	archive := filepath.Join(dir, "archive")

	_, err := run(t, moveDefinition, map[string]any{"source": filepath.Join(dir, "in", "*.csv"), "target": archive})
	if err == nil || !strings.Contains(err.Error(), "already exists (set overwrite: true") {
		t.Fatalf("err = %v", err)
	}
	if !present(filepath.Join(dir, "in", "a.csv")) {
		t.Fatal("a.csv was moved although b.csv conflicts")
	}
	got, err := run(t, moveDefinition, map[string]any{"source": filepath.Join(dir, "in", "*.csv"), "target": archive, "overwrite": true})
	if err != nil || names(got) != "a.csv,b.csv" {
		t.Fatalf("got %v, %v", got, err)
	}
	if present(filepath.Join(dir, "in", "a.csv")) || read(t, filepath.Join(archive, "b.csv")) != "B" {
		t.Error("files not moved")
	}

	// A folder is moved (renamed) as a whole.
	if _, err := run(t, moveDefinition, map[string]any{"source": filepath.Join(dir, "in", "sub"), "target": filepath.Join(dir, "moved")}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "moved", "x.txt")) != "X" || present(filepath.Join(dir, "in", "sub")) {
		t.Error("folder not moved")
	}
	if _, err := run(t, moveDefinition, map[string]any{"source": filepath.Join(dir, "moved"), "target": filepath.Join(dir, "moved", "x")}); err == nil || !strings.Contains(err.Error(), "into itself") {
		t.Errorf("into itself: %v", err)
	}
	actiontest.ConfigError(t, moveDefinition, map[string]any{"target": "x"}, `missing required field "source"`)
}
