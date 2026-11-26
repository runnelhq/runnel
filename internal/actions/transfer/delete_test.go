package transfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestDelete(t *testing.T) {
	eachServer(t, testDelete)
}

func testDelete(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "out/a.csv", "a")
	writeFile(t, srv.root, "out/b.csv", "b")
	writeFile(t, srv.root, "out/C.CSV", "upper case does not match")
	writeFile(t, srv.root, "out/sub/c.csv", "c")
	res, logs, err := execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out/*.csv"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"out/a.csv", "out/b.csv"}) {
		t.Fatalf("%v %v", res.Output, err)
	}
	if read(t, srv.root, "out/a.csv") != "" || read(t, srv.root, "out/C.CSV") == "" {
		t.Error("wrong files deleted")
	}
	if logs[len(logs)-1] != "deleted 2 items on 127.0.0.1:"+srv.port {
		t.Errorf("%v", logs)
	}
	// Nothing to delete is fine, unless ifMissing: fail.
	res, _, err = execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out/*.csv"})
	if err != nil || len(res.Output.([]any)) != 0 {
		t.Errorf("missing: %v %v", res.Output, err)
	}
	_, _, err = execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out/missing.csv", "ifMissing": "fail"})
	if err == nil || err.Error() != `nothing on the server matches "out/missing.csv"` {
		t.Errorf("ifMissing fail: %v", err)
	}
	// A folder only with recursive; it is checked before anything is deleted.
	_, _, err = execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out/*"})
	if err == nil || !strings.Contains(err.Error(), `cannot delete "out/sub": it is a folder on the server (set recursive: true`) {
		t.Fatalf("%v", err)
	}
	if read(t, srv.root, "out/C.CSV") == "" {
		t.Error("a file was deleted before the folder stopped the action")
	}
	res, _, err = execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out", "recursive": true})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"out"}) {
		t.Fatalf("recursive: %v %v", res.Output, err)
	}
	if _, err := os.Stat(filepath.Join(srv.root, "out")); !os.IsNotExist(err) {
		t.Error("the folder is still there")
	}
}

func TestDeleteErrors(t *testing.T) {
	eachServer(t, testDeleteErrors)
}

func testDeleteErrors(t *testing.T, srv *testServer) {
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": " "}, "no path given"},
		{map[string]any{"path": "out/["}, `invalid wildcard pattern "["`},
		{map[string]any{"path": "a", "timeout": "never"}, `timeout: "never" is not a duration`},
	}
	for _, c := range cases {
		if _, _, err := execute(context.Background(), t, deleteDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	actiontest.ConfigError(t, deleteDefinition, map[string]any{"connection": "s"}, `missing required field "path"`)
	actiontest.ConfigError(t, deleteDefinition, map[string]any{"connection": "s", "path": "a", "ifMissing": "empty"}, `invalid ifMissing "empty" (expected skip or fail)`)
	// A cancelled run ends the action.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := execute(ctx, t, deleteDefinition, srv.conn(), map[string]any{"path": "a"}); !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
}

// Links are deleted themselves, never what they point to.
func TestDeleteLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra rights on Windows")
	}
	srv := newTestServer(t)
	writeFile(t, srv.root, "keep/a.csv", "a")
	if err := os.MkdirAll(filepath.Join(srv.root, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(srv.root, "keep"), filepath.Join(srv.root, "out", "link")); err != nil {
		t.Fatal(err)
	}
	res, _, err := execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out", "recursive": true})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"out"}) {
		t.Fatalf("%v %v", res.Output, err)
	}
	if read(t, srv.root, "keep/a.csv") != "a" {
		t.Error("the target of the link was deleted")
	}
	// The link itself, without recursive: it is no folder.
	if err := os.Symlink(filepath.Join(srv.root, "keep"), filepath.Join(srv.root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "link"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(srv.root, "link")); !os.IsNotExist(err) || read(t, srv.root, "keep/a.csv") != "a" {
		t.Error("wrong entry deleted")
	}
}

// Names from the server that could lead elsewhere are refused (\ separates on Windows
// servers).
func TestDeleteUnsafeName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow \\ in names of files on the test server")
	}
	eachServer(t, testDeleteUnsafeName)
}

func testDeleteUnsafeName(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, `out/..\evil.txt`, "evil")
	_, _, err := execute(context.Background(), t, deleteDefinition, srv.conn(), map[string]any{"path": "out", "recursive": true})
	if err == nil || !strings.Contains(err.Error(), `the server sent the unsafe name "..\\evil.txt"`) {
		t.Errorf("%v", err)
	}
	if read(t, srv.root, `out/..\evil.txt`) == "" {
		t.Error("deleted")
	}
}

func TestRemoteName(t *testing.T) {
	for name, want := range map[string]bool{"a.csv": true, "..a": true, ".a": true, "": false, ".": false, "..": false, `..\x`: false, "a/b": false, `a\b`: false} {
		if got := remoteName(name); got != want {
			t.Errorf("remoteName(%q) = %v", name, got)
		}
	}
}
