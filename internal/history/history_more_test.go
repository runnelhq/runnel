package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func putRunning(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.PutRun(context.Background(), Run{ID: id, Path: "p", Kind: "workflow", Status: "running", Started: time.Now(), Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
}

// DefaultBase follows the folders of the operating system.
func TestDefaultBaseFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		local := filepath.Join(t.TempDir(), "Local")
		t.Setenv("LOCALAPPDATA", local)
		if base, err := DefaultBase(); err != nil || base != filepath.Join(local, "runnel", "history") {
			t.Errorf("LOCALAPPDATA: %s %v", base, err)
		}
		return
	}
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", state)
	if base, err := DefaultBase(); err != nil || base != filepath.Join(state, "runnel", "history") {
		t.Errorf("XDG_STATE_HOME: %s %v", base, err)
	}
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	if base, err := DefaultBase(); err != nil || base != filepath.Join(home, ".local", "state", "runnel", "history") {
		t.Errorf("HOME: %s %v", base, err)
	}
	t.Setenv("HOME", "")
	if base, err := DefaultBase(); err == nil {
		t.Errorf("without HOME: %s, want an error", base)
	}
}

// ProjectDir resolves relative folders and names the root of a drive "project".
func TestProjectDirSpecial(t *testing.T) {
	base := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := ProjectDir(base, ".")
	if err != nil {
		t.Fatal(err)
	}
	if abs, _ := ProjectDir(base, wd); rel != abs {
		t.Errorf("relative %s, absolute %s", rel, abs)
	}
	root := string(filepath.Separator)
	if v := filepath.VolumeName(wd); v != "" {
		root = v + root
	}
	d, err := ProjectDir(base, root)
	if err != nil || !strings.HasPrefix(filepath.Base(d), "project-") {
		t.Errorf("root %s: %s %v", root, d, err)
	}
	if runtime.GOOS == "windows" {
		upper, _ := ProjectDir(base, `C:\Data\Orders`)
		lower, _ := ProjectDir(base, `c:\data\orders`)
		if filepath.Base(upper)[len("Orders-"):] != filepath.Base(lower)[len("orders-"):] {
			t.Errorf("case on Windows: %s %s", upper, lower)
		}
	}
}

// Open reports a folder it cannot create and a file that is no history, and leaves the
// file as it is.
func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(file, "history")); err == nil || !strings.Contains(err.Error(), "cannot create the history folder") {
		t.Errorf("folder below a file: %v", err)
	}

	broken := filepath.Join(dir, "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	junk := []byte(strings.Repeat("this is not a database\n", 200))
	if err := os.WriteFile(filepath.Join(broken, FileName), junk, 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(context.Background(), broken); err == nil {
		_ = s.Close()
		t.Error("broken file: no error")
	} else if !strings.Contains(err.Error(), "cannot open the history "+filepath.Join(broken, FileName)) {
		t.Errorf("broken file: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(broken, FileName)); string(b) != string(junk) {
		t.Error("broken file was changed")
	}
}

// Open creates the file with the current version and names it in Path.
func TestOpenNew(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	s := open(t, dir)
	if s.Path() != filepath.Join(dir, FileName) {
		t.Errorf("path %s", s.Path())
	}
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != len(migrations) {
		t.Errorf("version %d %v", v, err)
	}
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal mode %q %v", mode, err)
	}
}

// Processes that start at the same time (service, server, runnel run) all open an
// existing history and see each other's runs.
func TestOpenConcurrently(t *testing.T) {
	dir := t.TempDir()
	_ = open(t, dir).Close()
	const n = 4
	stores := make([]*Store, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { stores[i], errs[i] = Open(context.Background(), dir) })
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Errorf("open %d: %v", i, errs[i])
			continue
		}
		t.Cleanup(func() { _ = stores[i].Close() })
	}
	if t.Failed() {
		return
	}
	putRunning(t, stores[0], "r")
	if list, err := stores[n-1].List(context.Background(), 10); err != nil || len(list) != 1 {
		t.Errorf("list %v %v", list, err)
	}
}

// Open marks the runs of a stopped process as failed, at their last sign of life.
func TestOpenMarksStale(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	putRunning(t, s, "r")
	past := time.Now().Add(-StaleAfter - time.Minute)
	if _, err := s.db.Exec(`UPDATE runs SET heartbeat = ?`, ms(past)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s = open(t, dir)
	var status, msg string
	var finished int64
	if err := s.db.QueryRow(`SELECT status, error, finished FROM runs WHERE id = 'r'`).Scan(&status, &msg, &finished); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || msg != StaleError || finished != ms(past) {
		t.Errorf("%s %q %d", status, msg, finished)
	}
}

// Update writes all changes of fn or none.
func TestUpdate(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	boom := errors.New("boom")
	err := s.Update(ctx, func(tx *Tx) error {
		if err := tx.PutRun(ctx, Run{ID: "r", Path: "p", Kind: "workflow", Status: "running", Started: time.Now(), Data: []byte(`{}`)}); err != nil {
			return err
		}
		if err := tx.AddLog(ctx, "r", []LogLine{{Seq: 1, Time: time.Now(), Level: "info", Message: "x"}}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if _, ok, err := s.Get(ctx, "r"); ok || err != nil {
		t.Errorf("rolled back run is there: %v %v", ok, err)
	}
	err = s.Update(ctx, func(tx *Tx) error {
		for _, id := range []string{"a", "b"} {
			if err := tx.PutRun(ctx, Run{ID: id, Path: "p", Kind: "pipeline", Status: "running", Started: time.Now(), Data: []byte(`{}`)}); err != nil {
				return err
			}
			if err := tx.PutNode(ctx, id, Node{Index: 0, ID: "read", Data: []byte(`{}`)}); err != nil {
				return err
			}
		}
		if err := tx.Beat(ctx, []string{"a", "b"}); err != nil {
			return err
		}
		return tx.Delete(ctx, "b")
	})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(ctx, 10); err != nil || len(list) != 1 || list[0].ID != "a" || list[0].Finished != nil {
		t.Errorf("list %+v %v", list, err)
	}
	var nodes int
	_ = s.db.QueryRow(`SELECT count(*) FROM nodes`).Scan(&nodes)
	if nodes != 1 {
		t.Errorf("%d nodes, want 1", nodes)
	}
}

// Actions and log lines need their run: writing them for a run that does not exist
// (e.g. deleted by Prune) is an error, not a line without run.
func TestNeedsRun(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	if err := s.PutNode(ctx, "none", Node{Index: 0, ID: "a", Data: []byte(`{}`)}); err == nil {
		t.Error("node without run: no error")
	}
	if err := s.AddLog(ctx, "none", []LogLine{{Seq: 1, Time: time.Now(), Level: "info", Message: "x"}}); err == nil {
		t.Error("log without run: no error")
	}
	if err := s.AddLog(ctx, "none", nil); err != nil {
		t.Errorf("no lines: %v", err)
	}
}

// The log keeps the newest MaxLogLines lines over several calls; a line written again
// with its number replaces the old one.
func TestLogOverCalls(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	putRunning(t, s, "r")
	add := func(from, to int64, msg string) {
		t.Helper()
		var lines []LogLine
		for i := from; i <= to; i++ {
			lines = append(lines, LogLine{Seq: i, Time: time.Now(), Level: "info", Message: msg})
		}
		if err := s.AddLog(ctx, "r", lines); err != nil {
			t.Fatal(err)
		}
	}
	add(1, MaxLogLines, "first")
	add(MaxLogLines, MaxLogLines, "again")
	add(MaxLogLines+1, MaxLogLines+3, "second")
	d, _, err := s.Get(ctx, "r")
	if err != nil || len(d.Log) != MaxLogLines || d.Log[0].Seq != 4 || !d.LogTruncated {
		t.Fatalf("%d lines, first %d, truncated %v, %v", len(d.Log), d.Log[0].Seq, d.LogTruncated, err)
	}
	if l := d.Log[MaxLogLines-4]; l.Seq != MaxLogLines || l.Message != "again" {
		t.Errorf("replaced line %+v", l)
	}
}

func TestCut(t *testing.T) {
	note := " … (cut)"
	for _, tt := range []struct {
		s    string
		n    int
		want string
	}{
		{"abc", 3, "abc"},
		{"", 3, ""},
		{strings.Repeat("ä", 10), 20, strings.Repeat("ä", 10)},
		{strings.Repeat("a", 20), 19, strings.Repeat("a", 19-len(note)) + note},
		{strings.Repeat("€", 10), 20, strings.Repeat("€", 3) + note},
		{strings.Repeat("ä", 11), 20, strings.Repeat("ä", 5) + note},
	} {
		got := cut(tt.s, tt.n)
		if got != tt.want || len(got) > tt.n {
			t.Errorf("cut(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

// Beat only renews running runs; an ended run stays as it is.
func TestBeatEnded(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	end := time.Now()
	if err := s.PutRun(ctx, Run{ID: "done", Path: "p", Kind: "workflow", Status: "succeeded", Started: end, Finished: &end, Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE runs SET heartbeat = 1`); err != nil {
		t.Fatal(err)
	}
	if err := s.Beat(ctx, []string{"done", "unknown"}); err != nil {
		t.Fatal(err)
	}
	var hb int64
	if err := s.db.QueryRow(`SELECT heartbeat FROM runs`).Scan(&hb); err != nil || hb != 1 {
		t.Errorf("heartbeat %d %v", hb, err)
	}
}

// Prune keeps everything within the limits and stops when the context ends.
func TestPruneLimits(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	now := time.Now()
	for _, id := range []string{"01", "02", "03"} {
		if err := s.PutRun(ctx, Run{ID: id, Path: "p", Kind: "workflow", Status: "succeeded", Started: now, Data: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Prune(ctx, 30, 3, now); err != nil || n != 0 {
		t.Errorf("within limits: %d %v", n, err)
	}
	if n, err := s.Prune(ctx, 30, 1, now); err != nil || n != 2 {
		t.Errorf("maxRuns 1: %d %v", n, err)
	}
	if list, _ := s.List(ctx, 10); len(list) != 1 || list[0].ID != "03" {
		t.Errorf("left %+v", list)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Prune(canceled, 0, 1, now.Add(time.Hour)); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	if list, _ := s.List(ctx, 10); len(list) != 1 {
		t.Errorf("canceled prune removed runs: %+v", list)
	}
}

// A closed history reports errors instead of panicking.
func TestClosed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, 10); err == nil {
		t.Error("List: no error")
	}
	if _, _, err := s.Get(ctx, "r"); err == nil {
		t.Error("Get: no error")
	}
	if err := s.PutRun(ctx, Run{ID: "r", Data: []byte(`{}`)}); err == nil {
		t.Error("PutRun: no error")
	}
	if _, err := s.Prune(ctx, 1, 1, time.Now()); err == nil {
		t.Error("Prune: no error")
	}
}

// A damaged history (a table missing, a value of the wrong type) reports errors instead
// of a run with part of its data.
func TestDamaged(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	for _, id := range []string{"a", "b"} {
		putRunning(t, s, id)
		if err := s.PutNode(ctx, id, Node{Index: 0, ID: "read", Data: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE nodes SET idx = 'x' WHERE run_id = 'a'`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get(ctx, "a"); err == nil || ok {
		t.Errorf("node of the wrong type: %v %v", ok, err)
	}
	if _, err := s.db.Exec(`DROP TABLE log`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get(ctx, "b"); err == nil || ok {
		t.Errorf("log missing: %v %v", ok, err)
	}
	if _, err := s.db.Exec(`UPDATE runs SET started = 'x' WHERE id = 'b'`); err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(ctx, 10); err == nil {
		t.Errorf("run of the wrong type: %+v", list)
	}
}
