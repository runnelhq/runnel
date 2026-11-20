package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestCopyFilesIntoFolder(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.csv": "A", "in/b.csv": "B", "in/c.txt": "C"})
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	_ = os.Chtimes(filepath.Join(dir, "in", "a.csv"), old, old)

	out := filepath.Join(dir, "out")
	got, err := run(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "in", "*.csv"), "target": out})
	if err != nil || names(got) != "a.csv,b.csv" {
		t.Fatalf("got %v, %v", got, err)
	}
	if read(t, filepath.Join(out, "a.csv")) != "A" || present(filepath.Join(out, "c.txt")) {
		t.Error("wrong files copied")
	}
	if info, _ := os.Stat(filepath.Join(out, "a.csv")); !info.ModTime().Equal(old) {
		t.Errorf("modification time %v, want %v", info.ModTime(), old)
	}

	// Existing files are only replaced with overwrite.
	tree(t, dir, map[string]string{"in/a.csv": "A2"})
	_, err = run(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "in", "a.csv"), "target": out})
	if err == nil || !strings.Contains(err.Error(), "already exists (set overwrite: true") {
		t.Errorf("err = %v", err)
	}
	if _, err := run(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "in", "a.csv"), "target": out, "overwrite": true}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(out, "a.csv")) != "A2" {
		t.Error("not overwritten")
	}
}

func TestCopyRenamesSingleFileAndCopiesFolders(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.csv": "A", "in/sub/deep/x.txt": "X"})
	target := filepath.Join(dir, "new", "renamed.csv")
	if got, err := run(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "in", "a.csv"), "target": target}); err != nil || got[0] != target {
		t.Fatalf("got %v, %v", got, err)
	}
	if read(t, target) != "A" {
		t.Error("content")
	}
	// A target ending with a separator is a folder.
	folder := filepath.Join(dir, "box") + string(filepath.Separator)
	if _, err := run(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "in", "a.csv"), "target": folder}); err != nil {
		t.Fatal(err)
	}
	if !present(filepath.Join(dir, "box", "a.csv")) {
		t.Error("not copied into the folder")
	}
	// A folder is copied with its content.
	got, err := run(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "in"), "target": filepath.Join(dir, "copy")})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if read(t, filepath.Join(dir, "copy", "sub", "deep", "x.txt")) != "X" {
		t.Error("folder content")
	}
}

func TestCopyErrors(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.csv": "A"})
	for _, tt := range []struct {
		source, target, want string
	}{
		{filepath.Join(dir, "in", "*.xml"), dir, "no file or folder matches"},
		{filepath.Join(dir, "in", "a.csv"), "", "no target given"},
		{filepath.Join(dir, "in", "a.csv"), filepath.Join(dir, "in", "a.csv"), "are the same"},
		{filepath.Join(dir, "in"), filepath.Join(dir, "in", "inner"), "into itself"},
	} {
		if _, err := run(t, copyDefinition, map[string]any{"source": tt.source, "target": tt.target}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s → %s: %v, want containing %q", tt.source, tt.target, err, tt.want)
		}
	}
	actiontest.ConfigError(t, copyDefinition, map[string]any{"source": "x"}, `missing required field "target"`)
}
