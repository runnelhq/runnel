package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

// tree creates files below dir: "a.csv" → "content", "sub/b.txt" → … .
func tree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func present(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// run executes an action and returns its output as strings (for lists of paths).
func run(t *testing.T, def action.Definition, with map[string]any) ([]string, error) {
	t.Helper()
	res, _, err := actiontest.Execute(context.Background(), actiontest.New(t, def, with))
	var out []string
	switch o := res.Output.(type) {
	case []any:
		for _, v := range o {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		out = []string{o}
	}
	return out, err
}

func names(paths []string) string {
	n := make([]string, len(paths))
	for i, p := range paths {
		n[i] = filepath.Base(p)
	}
	return strings.Join(n, ",")
}

func TestMatch(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.csv": "", "B.CSV": "", "c.txt": "", "sub/d.csv": ""})
	got, err := match(filepath.Join(dir, "*.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := "a.csv"
	if runtime.GOOS == "windows" {
		want = "B.CSV,a.csv" // case-insensitive like Explorer
	}
	if names(got) != want {
		t.Errorf("match = %s, want %s", names(got), want)
	}
	if got, _ := match(filepath.Join(dir, "?.txt")); names(got) != "c.txt" {
		t.Errorf("? = %s", names(got))
	}
	if got, err := match(filepath.Join(dir, "missing", "*.csv")); err != nil || len(got) != 0 {
		t.Errorf("missing folder: %v %v", got, err)
	}
	if _, err := match(filepath.Join(dir, "[.csv")); err == nil || !strings.Contains(err.Error(), "invalid wildcard pattern") {
		t.Errorf("bad pattern: %v", err)
	}
	if _, err := match(""); err == nil {
		t.Error("empty path accepted")
	}
}

func TestCancelledRunStopsCopying(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"big.bin": strings.Repeat("x", 1<<20)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := actiontest.New(t, copyDefinition, map[string]any{"source": filepath.Join(dir, "big.bin"), "target": filepath.Join(dir, "out") + string(filepath.Separator)})
	if _, _, err := actiontest.Execute(ctx, a); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if present(filepath.Join(dir, "out", "big.bin")) {
		t.Error("file was copied")
	}
	// No temporary files are left behind when copying stops.
	info, _ := os.Stat(filepath.Join(dir, "big.bin"))
	if err := copyFile(ctx, filepath.Join(dir, "big.bin"), filepath.Join(dir, "c.bin"), info, false); !errors.Is(err, context.Canceled) {
		t.Errorf("copyFile: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}
