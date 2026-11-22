package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transforms/transformtest"
)

func tree(t testing.TB, dir string, files map[string]string) {
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

func names(rs []record.Record) string {
	var out []string
	for _, r := range rs {
		v, _ := r.Get("path")
		out = append(out, filepath.ToSlash(v.(string)))
	}
	return strings.Join(out, ",")
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"in/a.csv": "AAA", "in/b.txt": "B", "in/sub/c.csv": "CC"})
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	_ = os.Chtimes(filepath.Join(dir, "in", "a.csv"), old, old)
	in := filepath.Join(dir, "in")
	slash := filepath.ToSlash(in)

	rs, _, err := transformtest.Run(context.Background(), transformtest.New(t, listDefinition, map[string]any{"folder": in, "pattern": "*.csv"}))
	if err != nil || names(rs) != slash+"/a.csv" {
		t.Fatalf("%s, %v", names(rs), err)
	}
	r := rs[0]
	for field, want := range map[string]any{"name": "a.csv", "extension": ".csv", "size": int64(3), "folder": false} {
		if v, _ := r.Get(field); v != want {
			t.Errorf("%s = %v, want %v", field, v, want)
		}
	}
	if v, _ := r.Get("modified"); !v.(time.Time).Equal(old) {
		t.Errorf("modified = %v", v)
	}

	rs, _, err = transformtest.Run(context.Background(), transformtest.New(t, listDefinition, map[string]any{"folder": in, "pattern": "*.csv", "recursive": true}))
	if err != nil || names(rs) != slash+"/a.csv,"+slash+"/sub/c.csv" {
		t.Errorf("recursive: %s, %v", names(rs), err)
	}
	rs, _, err = transformtest.Run(context.Background(), transformtest.New(t, listDefinition, map[string]any{"folder": in, "include": "folders"}))
	if err != nil || names(rs) != slash+"/sub" {
		t.Errorf("folders: %s, %v", names(rs), err)
	}
	if v, _ := rs[0].Get("extension"); v != "" {
		t.Errorf("extension of a folder: %q", v)
	}
	if _, _, err := transformtest.Run(context.Background(), transformtest.New(t, listDefinition, map[string]any{"folder": filepath.Join(dir, "none")})); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing folder: %v", err)
	}
}

func TestListConfig(t *testing.T) {
	for with, want := range map[string]map[string]any{
		"invalid wildcard pattern": {"folder": "in", "pattern": "[a"},
		`invalid include "links"`:  {"folder": "in", "include": "links"},
		`missing required field`:   {},
	} {
		if _, err := listDefinition.New(want); err == nil || !strings.Contains(err.Error(), with) {
			t.Errorf("%v: %v, want %q", want, err, with)
		}
	}
}

func BenchmarkList(b *testing.B) {
	dir := b.TempDir()
	files := map[string]string{}
	for i := range 500 {
		files[fmt.Sprintf("d%d/f%d.csv", i%10, i)] = "x"
	}
	tree(b, dir, files)
	tr, err := listDefinition.New(map[string]any{"folder": dir, "recursive": true})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for range b.N {
		if _, _, err := transformtest.Run(context.Background(), tr); err != nil {
			b.Fatal(err)
		}
	}
}
