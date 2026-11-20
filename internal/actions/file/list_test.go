package file

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func listOf(t *testing.T, with map[string]any) ([]map[string]any, error) {
	t.Helper()
	res, _, err := actiontest.Execute(context.Background(), actiontest.New(t, listDefinition, with))
	var out []map[string]any
	if l, ok := res.Output.([]any); ok {
		for _, e := range l {
			out = append(out, e.(map[string]any))
		}
	}
	return out, err
}

func entryNames(entries []map[string]any) string {
	var n []string
	for _, e := range entries {
		n = append(n, e["name"].(string))
	}
	return strings.Join(n, ",")
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.csv": "12345", "b.txt": "", "sub/c.csv": "", "sub/deep/d.csv": ""})

	got, err := listOf(t, map[string]any{"folder": dir})
	if err != nil || entryNames(got) != "a.csv,b.txt" {
		t.Fatalf("got %s, %v", entryNames(got), err)
	}
	a := got[0]
	if a["path"] != filepath.Join(dir, "a.csv") || a["size"] != int64(5) || a["folder"] != false {
		t.Errorf("entry %v", a)
	}
	if m, ok := a["modified"].(time.Time); !ok || time.Since(m) > time.Minute {
		t.Errorf("modified %v", a["modified"])
	}

	got, _ = listOf(t, map[string]any{"folder": dir, "pattern": "*.csv", "recursive": true})
	if entryNames(got) != "a.csv,c.csv,d.csv" {
		t.Errorf("recursive: %s", entryNames(got))
	}
	got, _ = listOf(t, map[string]any{"folder": dir, "include": "folders", "recursive": true})
	if entryNames(got) != "sub,deep" {
		t.Errorf("folders: %s", entryNames(got))
	}
	got, _ = listOf(t, map[string]any{"folder": dir, "include": "all"})
	if entryNames(got) != "a.csv,b.txt,sub" {
		t.Errorf("all: %s", entryNames(got))
	}
	got, err = listOf(t, map[string]any{"folder": dir, "pattern": "*.xml"})
	if err != nil || len(got) != 0 {
		t.Errorf("empty list: %v %v", got, err)
	}
}

func TestListErrors(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"folder": filepath.Join(dir, "missing")}, "does not exist"},
		{map[string]any{"folder": dir, "pattern": "["}, "invalid wildcard pattern"},
		{map[string]any{"folder": dir, "include": "links"}, "invalid include"},
		{map[string]any{"folder": ""}, "no folder given"},
	} {
		if _, err := listOf(t, tt.with); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want containing %q", tt.with, err, tt.want)
		}
	}
	actiontest.ConfigError(t, listDefinition, nil, `missing required field "folder"`)
}
