package transfer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestList(t *testing.T) {
	eachServer(t, testList)
}

func testList(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "out/b.csv", "bb")
	writeFile(t, srv.root, "out/a.csv", "a")
	writeFile(t, srv.root, "out/notes.txt", "n")
	writeFile(t, srv.root, "out/sub/c.csv", "ccc")
	paths := func(out any) string {
		var p []string
		for _, e := range out.([]any) {
			p = append(p, e.(map[string]any)["path"].(string))
		}
		return strings.Join(p, " ")
	}
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"folder": "out"}, "out/a.csv out/b.csv out/notes.txt"},
		{map[string]any{"folder": "out", "pattern": "*.csv"}, "out/a.csv out/b.csv"},
		{map[string]any{"folder": "out", "pattern": "*.csv", "recursive": true}, "out/a.csv out/b.csv out/sub/c.csv"},
		{map[string]any{"folder": "out", "include": "folders"}, "out/sub"},
		{map[string]any{"folder": "out", "include": "all", "recursive": true}, "out/a.csv out/b.csv out/notes.txt out/sub out/sub/c.csv"},
		{map[string]any{"folder": ".", "include": "folders"}, "out"},
		{map[string]any{"folder": "out", "pattern": "*.CSV"}, ""},
	}
	for _, c := range cases {
		res, _, err := execute(context.Background(), t, listDefinition, srv.conn(), c.with)
		if err != nil || paths(res.Output) != c.want {
			t.Errorf("%v: %q %v, want %q", c.with, paths(res.Output), err, c.want)
		}
	}
	res, logs, err := execute(context.Background(), t, listDefinition, srv.conn(), map[string]any{"folder": "out", "pattern": "b.csv"})
	if err != nil {
		t.Fatal(err)
	}
	e := res.Output.([]any)[0].(map[string]any)
	if e["name"] != "b.csv" || e["size"] != int64(2) || e["folder"] != false || e["modified"].(time.Time).IsZero() {
		t.Errorf("%v", e)
	}
	if logs[0] != "found 1 entry in out on 127.0.0.1:"+srv.port {
		t.Errorf("%v", logs)
	}
}

func TestListErrors(t *testing.T) {
	eachServer(t, testListErrors)
}

func testListErrors(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "file.txt", "x")
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"folder": "missing"}, `folder "missing" does not exist on the server`},
		{map[string]any{"folder": "file.txt"}, `folder "file.txt" does not exist on the server`},
		{map[string]any{"folder": ".", "pattern": "["}, `invalid wildcard pattern "["`},
		{map[string]any{"folder": ".", "include": "links"}, `invalid include "links"`},
		{map[string]any{"folder": " "}, "no folder given"},
	}
	for _, c := range cases {
		if _, _, err := execute(context.Background(), t, listDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	actiontest.ConfigError(t, listDefinition, map[string]any{"connection": "s"}, `missing required field "folder"`)
	// Without connections (no project).
	a := actiontest.New(t, listDefinition, map[string]any{"connection": "s", "folder": "."})
	if _, _, err := actiontest.Execute(context.Background(), a); err == nil || !strings.Contains(err.Error(), "connections are not available") {
		t.Errorf("%v", err)
	}
}

func TestListLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 10001 files")
	}
	srv := newTestServer(t)
	for i := range maxEntries + 1 {
		writeFile(t, srv.root, fmt.Sprintf("many/%05d.txt", i), "")
	}
	if _, _, err := execute(context.Background(), t, listDefinition, srv.conn(), map[string]any{"folder": "many"}); err == nil || !strings.Contains(err.Error(), `"many" has more than 10000 entries`) {
		t.Errorf("%v", err)
	}
}
