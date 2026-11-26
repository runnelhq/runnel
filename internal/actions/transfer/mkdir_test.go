package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestMkdir(t *testing.T) {
	srv := newTestServer(t)
	res, logs, err := execute(context.Background(), t, mkdirDefinition, srv.conn(), map[string]any{"path": "archive/2026/11/"})
	if err != nil || res.Output != "archive/2026/11" {
		t.Fatalf("%v %v", res.Output, err)
	}
	if info, err := os.Stat(filepath.Join(srv.root, "archive", "2026", "11")); err != nil || !info.IsDir() {
		t.Errorf("not created: %v", err)
	}
	if logs[0] != "created folder archive/2026/11 on 127.0.0.1:"+srv.port {
		t.Errorf("%v", logs)
	}
	// An existing folder is fine.
	res, logs, err = execute(context.Background(), t, mkdirDefinition, srv.conn(), map[string]any{"path": "archive/2026"})
	if err != nil || res.Output != "archive/2026" || !strings.HasPrefix(logs[0], "folder archive/2026 exists already") {
		t.Errorf("%v %v %v", res.Output, logs, err)
	}
	writeFile(t, srv.root, "file.txt", "x")
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"path": "file.txt"}, `cannot create folder "file.txt": a file with this name exists on the server`},
		// The message differs by the system of the server: a file is in the way.
		{map[string]any{"path": "file.txt/sub"}, `"file.txt/sub" on the server`},
		{map[string]any{"path": " "}, "no path given"},
	}
	for _, c := range cases {
		if _, _, err := execute(context.Background(), t, mkdirDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	actiontest.ConfigError(t, mkdirDefinition, map[string]any{"connection": "s"}, `missing required field "path"`)
}
