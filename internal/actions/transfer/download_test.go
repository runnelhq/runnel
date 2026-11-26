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
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
	"github.com/runnelhq/runnel/internal/paths"
)

func TestDownload(t *testing.T) {
	eachServer(t, testDownload)
}

func testDownload(t *testing.T, srv *testServer) {
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	writeFile(t, srv.root, "outgoing/a.csv", "a")
	writeFile(t, srv.root, "outgoing/b.csv", "bb")
	writeFile(t, srv.root, "outgoing/C.CSV", "upper case does not match")
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(srv.root, "outgoing", "a.csv"), old, old); err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(project, "inbox")
	res, logs, err := execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": "outgoing/*.csv", "target": "inbox/"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{filepath.Join(inbox, "a.csv"), filepath.Join(inbox, "b.csv")}; !reflect.DeepEqual(res.Output, want) {
		t.Errorf("output %v", res.Output)
	}
	if read(t, inbox, "a.csv") != "a" || read(t, inbox, "b.csv") != "bb" {
		t.Error("wrong files")
	}
	if info, err := os.Stat(filepath.Join(inbox, "a.csv")); srv.times && (err != nil || !info.ModTime().Equal(old)) {
		t.Errorf("modification time not kept: %v", err)
	}
	if logs[len(logs)-1] != "downloaded 2 files from 127.0.0.1:"+srv.port+" to inbox/" {
		t.Errorf("%v", logs)
	}

	// Conflicts are found before the first write.
	if err := os.Remove(filepath.Join(inbox, "a.csv")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, inbox, "b.csv", "kept")
	_, _, err = execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": "outgoing/*.csv", "target": "inbox"})
	if err == nil || !strings.Contains(err.Error(), "already exists (set ifExists: replace or skip)") {
		t.Fatalf("%v", err)
	}
	if read(t, inbox, "a.csv") != "" {
		t.Error("a.csv was downloaded before the conflict was found")
	}
	res, _, err = execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": "outgoing/*.csv", "target": "inbox", "ifExists": "skip"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{filepath.Join(inbox, "a.csv")}) || read(t, inbox, "b.csv") != "kept" {
		t.Errorf("skip: %v %v", res.Output, err)
	}
	res, _, err = execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": "outgoing/*.csv", "target": "inbox", "ifExists": "replace"})
	if err != nil || len(res.Output.([]any)) != 2 || read(t, inbox, "b.csv") != "bb" {
		t.Errorf("replace: %v %v", res.Output, err)
	}
	// A single file with a new name.
	res, _, err = execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": "outgoing/C.CSV", "target": "data/c.csv"})
	if err != nil || !reflect.DeepEqual(res.Output, []any{filepath.Join(project, "data", "c.csv")}) {
		t.Errorf("rename: %v %v", res.Output, err)
	}
	if got := tempFiles(t, project); len(got) != 0 {
		t.Errorf("temporary files left: %v", got)
	}
}

func TestDownloadFolder(t *testing.T) {
	eachServer(t, testDownloadFolder)
}

func testDownloadFolder(t *testing.T, srv *testServer) {
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	writeFile(t, srv.root, "export/x.csv", "x")
	writeFile(t, srv.root, "export/sub/y.csv", "y")
	res, _, err := execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": "export", "target": "copy"})
	want := []any{filepath.Join(project, "copy", "sub", "y.csv"), filepath.Join(project, "copy", "x.csv")}
	if err != nil || !reflect.DeepEqual(res.Output, want) {
		t.Fatalf("%v %v", res.Output, err)
	}
	if read(t, project, "copy/sub/y.csv") != "y" {
		t.Error("y.csv missing")
	}
}

func TestDownloadErrors(t *testing.T) {
	eachServer(t, testDownloadErrors)
}

func testDownloadErrors(t *testing.T, srv *testServer) {
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), project)
	writeFile(t, srv.root, "big.bin", strings.Repeat("x", 2048))
	cases := []struct {
		with map[string]any
		want string
	}{
		{map[string]any{"source": "outgoing/*.csv", "target": "in/"}, `nothing on the server matches "outgoing/*.csv"`},
		{map[string]any{"source": "missing.csv", "target": "in/"}, `nothing on the server matches "missing.csv"`},
		{map[string]any{"source": "big.bin", "target": "in/", "maxSize": "1KB"}, `cannot download "big.bin": it is larger than maxSize 1KB`},
		{map[string]any{"source": "[", "target": "in/"}, `invalid wildcard pattern "["`},
	}
	for _, c := range cases {
		if _, _, err := execute(ctx, t, downloadDefinition, srv.conn(), c.with); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.with, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "in", "big.bin")); !os.IsNotExist(err) {
		t.Error("the large file was written")
	}
	actiontest.ConfigError(t, downloadDefinition, map[string]any{"connection": "s", "source": "a", "target": "b", "maxSize": "huge"}, `maxSize: "huge" is not a size`)
	actiontest.ConfigError(t, downloadDefinition, map[string]any{"connection": "s", "target": "b"}, `missing required field "source"`)
}

// A download that fails halfway leaves neither the target nor a temporary file.
func TestDownloadNoHalfFile(t *testing.T) {
	eachServer(t, testDownloadNoHalfFile)
}

func testDownloadNoHalfFile(t *testing.T, srv *testServer) {
	writeFile(t, srv.root, "small.bin", "x")
	writeFile(t, srv.root, "big.bin", strings.Repeat("x", 1<<20))
	s, err := connect(context.Background(), resolved(srv.conn()))
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	small, _ := os.Stat(filepath.Join(srv.root, "small.bin"))
	local := filepath.Join(t.TempDir(), "big.bin")
	// The size on the server grew after the check: the limit stops the transfer.
	if err := s.get("big.bin", small, local, "", 1000); !errors.Is(err, errTooLarge) {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(local)); len(entries) != 0 {
		t.Errorf("files left: %v", entries)
	}
}

// Names from the server that are no safe local names are refused (no path traversal).
func TestDownloadUnsafeName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow \\ in names of files on the test server")
	}
	eachServer(t, testDownloadUnsafeName)
}

func testDownloadUnsafeName(t *testing.T, srv *testServer) {
	project := t.TempDir()
	ctx := paths.WithBase(context.Background(), filepath.Join(project, "p"))
	writeFile(t, srv.root, `out/..\evil.txt`, "evil")
	for _, source := range []string{"out/*.txt", "out"} {
		_, _, err := execute(ctx, t, downloadDefinition, srv.conn(), map[string]any{"source": source, "target": "in/"})
		if err == nil || !strings.Contains(err.Error(), `"..\\evil.txt" is not allowed as a local file name`) {
			t.Errorf("%s: %v", source, err)
		}
	}
	if entries, _ := os.ReadDir(project); len(entries) > 1 {
		t.Errorf("written outside: %v", entries)
	}
}
