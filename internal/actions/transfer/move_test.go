package transfer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestMove(t *testing.T) {
	eachServer(t, testMove)
}

func testMove(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "out/a.csv", "a")
	writeFile(t, srv.root, "out/b.csv", "b")
	writeFile(t, srv.root, "out/notes.txt", "n")
	res, logs, err := execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "out/*.csv", "target": "out/archive/"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"out/archive/a.csv", "out/archive/b.csv"}) {
		t.Fatalf("%v %v", res.Output, err)
	}
	if read(t, srv.root, "out/archive/a.csv") != "a" || read(t, srv.root, "out/a.csv") != "" {
		t.Error("not moved")
	}
	if logs[len(logs)-1] != "moved 2 items to out/archive/ on 127.0.0.1:"+srv.port {
		t.Errorf("%v", logs)
	}
	// A single file gets a new name; into an existing folder it keeps its name.
	res, _, err = execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "out/notes.txt", "target": "out/readme.txt"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"out/readme.txt"}) || read(t, srv.root, "out/readme.txt") != "n" {
		t.Errorf("rename: %v %v", res.Output, err)
	}
	res, _, err = execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "out/readme.txt", "target": "out/archive"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"out/archive/readme.txt"}) {
		t.Errorf("into folder: %v %v", res.Output, err)
	}
	// A folder with its content.
	res, _, err = execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "out/archive", "target": "done/2026"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"done/2026"}) || read(t, srv.root, "done/2026/a.csv") != "a" {
		t.Errorf("folder: %v %v", res.Output, err)
	}
}

func TestMoveIfExists(t *testing.T) {
	eachServer(t, testMoveIfExists)
}

func testMoveIfExists(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "in/a.csv", "new a")
	writeFile(t, srv.root, "in/b.csv", "new b")
	writeFile(t, srv.root, "done/b.csv", "old b")
	// Conflicts are found before anything is moved.
	_, _, err := execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "in/*.csv", "target": "done"})
	if err == nil || !strings.Contains(err.Error(), `cannot move "in/b.csv": "done/b.csv" already exists on the server (set ifExists: replace or skip)`) {
		t.Fatalf("%v", err)
	}
	if read(t, srv.root, "in/a.csv") == "" {
		t.Error("a.csv was moved before the conflict was found")
	}
	res, _, err := execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "in/*.csv", "target": "done", "ifExists": "skip"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"done/a.csv"}) || read(t, srv.root, "done/b.csv") != "old b" || read(t, srv.root, "in/b.csv") != "new b" {
		t.Errorf("skip: %v %v", res.Output, err)
	}
	res, _, err = execute(context.Background(), t, moveDefinition, srv.conn(), map[string]any{"source": "in/b.csv", "target": "done/", "ifExists": "replace"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{"done/b.csv"}) || read(t, srv.root, "done/b.csv") != "new b" {
		t.Errorf("replace: %v %v", res.Output, err)
	}
	// Without posix-rename the existing file is removed first.
	writeFile(t, srv.root, "c.csv", "new c")
	writeFile(t, srv.root, "done/c.csv", "old c")
	s, err := connect(context.Background(), resolved(srv.conn()))
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if f, ok := s.fs.(*sftpFS); ok {
		f.posixRename = false // FTP has no other way
	}
	if err := s.move(movePair{from: "c.csv", to: "done/c.csv"}, "replace"); err != nil || read(t, srv.root, "done/c.csv") != "new c" {
		t.Errorf("without posix-rename: %v", err)
	}
}

func TestMoveErrors(t *testing.T) {
	eachServer(t, testMoveErrors)
}

func testMoveErrors(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "dir/a.csv", "a")
	writeFile(t, srv.root, "file.csv", "f")
	writeFile(t, srv.root, "other/dir/x.csv", "x")
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"source": "missing/*.csv", "target": "x/"}, `nothing on the server matches "missing/*.csv"`},
		{map[string]any{"source": "dir/a.csv", "target": "dir/"}, `"dir/a.csv" and "dir/a.csv" are the same`},
		{map[string]any{"source": "dir", "target": "dir/sub"}, `cannot move "dir" into itself ("dir/sub")`},
		// The message differs by the system of the server: a file is in the way.
		{map[string]any{"source": "file.csv", "target": "dir/a.csv/x"}, `"dir/a.csv`},
		{map[string]any{"source": "dir", "target": "other/dir"}, `"other/dir" already exists as a folder on the server`},
		{map[string]any{"source": "dir", "target": "file.csv"}, `cannot move "dir": a file "file.csv" exists on the server`},
		{map[string]any{"source": "file.csv", "target": " "}, "no target given"},
	}
	for _, c := range cases {
		if _, _, err := execute(context.Background(), t, moveDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	if read(t, srv.root, "dir/a.csv") != "a" || read(t, srv.root, "file.csv") != "f" {
		t.Error("moved despite an error")
	}
	actiontest.ConfigError(t, moveDefinition, map[string]any{"connection": "s", "source": "a"}, `missing required field "target"`)
	actiontest.ConfigError(t, moveDefinition, map[string]any{"connection": "s", "source": "a", "target": "b", "ifExists": "append"}, `invalid ifExists "append"`)
}
