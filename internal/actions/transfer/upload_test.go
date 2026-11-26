package transfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
	"github.com/runnelhq/runnel/internal/paths"
)

func TestUpload(t *testing.T) {
	srv := newTestServer(t)
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	writeFile(t, project, "outbox/a.csv", "a")
	writeFile(t, project, "outbox/b.csv", "bb")
	writeFile(t, project, "outbox/c.txt", "c")
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(project, "outbox", "a.csv"), old, old); err != nil {
		t.Fatal(err)
	}
	res, logs, err := execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "outbox/*.csv", "target": "incoming/"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{"incoming/a.csv", "incoming/b.csv"}; !reflect.DeepEqual(res.Output, want) {
		t.Errorf("output %v", res.Output)
	}
	if read(t, srv.root, "incoming/a.csv") != "a" || read(t, srv.root, "incoming/b.csv") != "bb" || read(t, srv.root, "incoming/c.txt") != "" {
		t.Error("wrong files on the server")
	}
	if info, err := os.Stat(filepath.Join(srv.root, "incoming", "a.csv")); err != nil || !info.ModTime().Equal(old) {
		t.Errorf("modification time not kept: %v %v", info, err)
	}
	if logs[len(logs)-1] != "uploaded 2 files to incoming/ on 127.0.0.1:"+srv.port {
		t.Errorf("%v", logs)
	}
	if got := tempFiles(t, srv.root); len(got) != 0 {
		t.Errorf("temporary files left: %v", got)
	}

	// Conflicts are found before the first write.
	writeFile(t, project, "outbox/b.csv", "new")
	writeFile(t, srv.root, "incoming/b.csv", "kept")
	if err := os.Remove(filepath.Join(srv.root, "incoming", "a.csv")); err != nil {
		t.Fatal(err)
	}
	_, _, err = execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "outbox/*.csv", "target": "incoming"})
	if err == nil || !strings.Contains(err.Error(), `"incoming/b.csv" already exists on the server (set ifExists: replace or skip)`) {
		t.Fatalf("%v", err)
	}
	if read(t, srv.root, "incoming/a.csv") != "" {
		t.Error("a.csv was uploaded before the conflict was found")
	}
	res, _, err = execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "outbox/*.csv", "target": "incoming", "ifExists": "skip"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"incoming/a.csv"}) || read(t, srv.root, "incoming/b.csv") != "kept" {
		t.Errorf("skip: %v %v", res.Output, err)
	}
	res, _, err = execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "outbox/*.csv", "target": "incoming", "ifExists": "replace"})
	if err != nil || len(res.Output.([]any)) != 2 || read(t, srv.root, "incoming/b.csv") != "new" {
		t.Errorf("replace: %v %v", res.Output, err)
	}

	// A single file gets a new name; an absolute local path works as well.
	res, _, err = execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": filepath.Join(project, "outbox", "c.txt"), "target": "renamed.txt"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"renamed.txt"}) || read(t, srv.root, "renamed.txt") != "c" {
		t.Errorf("rename: %v %v", res.Output, err)
	}
	// Into an existing folder without a final /.
	if err := os.MkdirAll(filepath.Join(srv.root, "box"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, _, err = execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "outbox/c.txt", "target": "box"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"box/c.txt"}) {
		t.Errorf("into folder: %v %v", res.Output, err)
	}
}

func TestUploadFolder(t *testing.T) {
	srv := newTestServer(t)
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	writeFile(t, project, "data/x.csv", "x")
	writeFile(t, project, "data/sub/y.csv", "y")
	if err := os.MkdirAll(filepath.Join(project, "data", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, _, err := execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "data", "target": "backup"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"backup/sub/y.csv", "backup/x.csv"}) {
		t.Fatalf("%v %v", res.Output, err)
	}
	if read(t, srv.root, "backup/sub/y.csv") != "y" {
		t.Error("y.csv missing")
	}
	if info, err := os.Stat(filepath.Join(srv.root, "backup", "empty")); err != nil || !info.IsDir() {
		t.Error("the empty folder was not created")
	}
	// A file where a folder would go.
	writeFile(t, srv.root, "file", "f")
	if _, _, err := execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "data", "target": "file"}); err == nil || !strings.Contains(err.Error(), `a file "file" exists on the server`) {
		t.Errorf("%v", err)
	}
}

func TestUploadErrors(t *testing.T) {
	srv := newTestServer(t)
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"source": "missing/*.csv", "target": "in/"}, `no file or folder matches "missing/*.csv"`},
		{map[string]any{"source": "x", "target": "in/", "timeout": "soon"}, `timeout: "soon" is not a duration`},
	}
	for _, c := range cases {
		if _, _, err := execute(ctx, t, uploadDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	actiontest.ConfigError(t, uploadDefinition, map[string]any{"connection": "s", "source": "a"}, `missing required field "target"`)
	actiontest.ConfigError(t, uploadDefinition, map[string]any{"connection": "s", "source": "a", "target": "b", "ifExists": "maybe"}, `invalid ifExists "maybe"`)
}

// A failed upload leaves no half file under the name of the target.
func TestUploadTimeout(t *testing.T) {
	srv := newTestServer(t)
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	writeFile(t, project, "big.bin", strings.Repeat("x", 8<<20))
	_, _, err := execute(ctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "big.bin", "target": "big.bin", "timeout": "1ms"})
	if err == nil || !strings.Contains(err.Error(), "did not finish within 1ms (timeout)") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(srv.root, "big.bin")); !os.IsNotExist(err) {
		t.Error("a file under the name of the target exists")
	}
	// A cancelled run ends the action.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := execute(cctx, t, uploadDefinition, srv.conn(), map[string]any{"source": "big.bin", "target": "big.bin"}); !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
}

// Servers without posix-rename get an existing file removed before the rename.
func TestRenameWithoutPosixRename(t *testing.T) {
	srv := newTestServer(t)
	local := writeFile(t, t.TempDir(), "a.csv", "new")
	writeFile(t, srv.root, "a.csv", "old")
	s, err := connect(context.Background(), resolved(srv.conn()))
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	s.posixRename = false
	info, _ := os.Stat(local)
	if err := s.put(local, info, "a.csv", "replace"); err != nil {
		t.Fatal(err)
	}
	if read(t, srv.root, "a.csv") != "new" || len(tempFiles(t, srv.root)) != 0 {
		t.Error("not replaced")
	}
}
