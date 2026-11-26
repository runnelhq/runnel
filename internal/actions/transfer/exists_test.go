package transfer

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestExists(t *testing.T) {
	eachServer(t, testExists)
}

func testExists(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "out/orders-1.csv", "1")
	writeFile(t, srv.root, "out/orders-2.csv", "2")
	writeFile(t, srv.root, "out/orders-3/x.csv", "folder")
	cases := []struct {
		with map[string]any
		want []any
	}{
		{map[string]any{"path": "out/orders-*"}, []any{"out/orders-1.csv", "out/orders-2.csv", "out/orders-3"}},
		{map[string]any{"path": "out/orders-*", "include": "files"}, []any{"out/orders-1.csv", "out/orders-2.csv"}},
		{map[string]any{"path": "out/orders-*", "include": "folders"}, []any{"out/orders-3"}},
		{map[string]any{"path": "out/orders-1.csv"}, []any{"out/orders-1.csv"}},
	}
	for _, c := range cases {
		res, _, err := execute(context.Background(), t, existsDefinition, srv.conn(), c.with)
		if err != nil || !reflect.DeepEqual(res.Output, c.want) {
			t.Errorf("%v: %v %v", c.with, res.Output, err)
		}
	}
	fails := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "out/ORDERS-*"}, `no file or folder matches "out/ORDERS-*" on the server`},
		{map[string]any{"path": "out/orders-3", "include": "files"}, `no file matches "out/orders-3" on the server`},
		{map[string]any{"path": "missing/*.csv", "include": "folders"}, `no folder matches "missing/*.csv" on the server`},
		{map[string]any{"path": "out/*", "include": "${WHAT}"}, `invalid include "${WHAT}" (expected all, files or folders)`},
	}
	for _, c := range fails {
		res, _, err := execute(context.Background(), t, existsDefinition, srv.conn(), c.with)
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
		if out, ok := res.Output.([]any); c.want[0] == 'n' && (!ok || len(out) != 0) {
			t.Errorf("%v: output %v", c.with, res.Output)
		}
	}
	actiontest.ConfigError(t, existsDefinition, map[string]any{"connection": "s", "path": "a", "include": "links"}, `invalid include "links" (expected all, files or folders)`)
	// kind is include since R5b, as in transfer.list.
	actiontest.ConfigError(t, existsDefinition, map[string]any{"connection": "s", "path": "a", "kind": "file"}, `field "kind" was renamed before 1.0: use include`)
}

func TestExistsLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 10001 files")
	}
	srv := newTestServer(t)
	for i := range maxEntries + 1 {
		writeFile(t, srv.root, fmt.Sprintf("many/%05d.txt", i), "")
	}
	_, _, err := execute(context.Background(), t, existsDefinition, srv.conn(), map[string]any{"path": "many/*.txt"})
	if err == nil || !strings.Contains(err.Error(), `more than 10000 entries on the server match "many/*.txt"`) {
		t.Errorf("%v", err)
	}
}
