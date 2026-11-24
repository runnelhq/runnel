package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestProjectDir(t *testing.T) {
	base := t.TempDir()
	a, err := ProjectDir(base, filepath.Join(base, "projects", "orders"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ProjectDir(base, filepath.Join(base, "other", "orders"))
	again, _ := ProjectDir(base, filepath.Join(base, "projects", "x", "..", "orders"))
	if filepath.Dir(a) != base || !strings.HasPrefix(filepath.Base(a), "orders-") || len(filepath.Base(a)) != len("orders-")+8 {
		t.Errorf("dir %s", a)
	}
	if a == b || a != again {
		t.Errorf("same name, other folder: %s %s; same folder: %s", a, b, again)
	}
}

// A run with its actions and log is kept and read back; the file survives a reopen.
func TestPutGetList(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s := open(t, dir)
	start := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	for i, id := range []string{"01A", "01B", "01C"} {
		r := Run{ID: id, Path: "workflows/import.workflow.yaml", Kind: "workflow", Trigger: "cron:nightly", Status: "running",
			Started: start.Add(time.Duration(i) * time.Minute), FileHash: "abc", Data: []byte(`{"id":"` + id + `"}`)}
		if err := s.PutRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutNode(ctx, "01B", Node{Index: 0, ID: "read", Data: []byte(`{"id":"read","status":"succeeded"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutNode(ctx, "01B", Node{Index: 0, ID: "read", Data: []byte(`{"id":"read","status":"failed"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLog(ctx, "01B", []LogLine{{Seq: 1, Time: start, Level: "info", ActionID: "read", Message: "read 3 rows"},
		{Seq: 2, Time: end, Level: "error", Message: "Grüße ✓"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRun(ctx, Run{ID: "01B", Path: "workflows/import.workflow.yaml", Kind: "workflow", Status: "failed",
		Started: start, Finished: &end, Error: "boom", Data: []byte(`{"id":"01B","status":"failed"}`)}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s = open(t, dir)
	list, err := s.List(ctx, 2)
	if err != nil || len(list) != 2 || list[0].ID != "01C" || list[1].ID != "01B" || list[0].Trigger != "cron:nightly" {
		t.Fatalf("list %+v %v", list, err)
	}
	d, ok, err := s.Get(ctx, "01B")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if d.Run.Status != "failed" || d.Run.Error != "boom" || d.Run.Finished == nil || !d.Run.Finished.Equal(end) || !d.Run.Started.Equal(start) {
		t.Errorf("run %+v", d.Run)
	}
	if len(d.Nodes) != 1 || string(d.Nodes[0].Data) != `{"id":"read","status":"failed"}` {
		t.Errorf("nodes %+v", d.Nodes)
	}
	if len(d.Log) != 2 || d.Log[1].Message != "Grüße ✓" || d.Log[0].ActionID != "read" || d.LogTruncated {
		t.Errorf("log %+v", d.Log)
	}
	if _, ok, err := s.Get(ctx, "nope"); ok || err != nil {
		t.Errorf("missing: %v %v", ok, err)
	}
	if err := s.Delete(ctx, "01B"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM log`).Scan(&n)
	if _, ok, _ := s.Get(ctx, "01B"); ok || n != 0 {
		t.Errorf("deleted run still there, %d log lines", n)
	}
}

// The log keeps the newest MaxLogLines lines; long lines are cut; too much data is refused.
func TestLimits(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	if err := s.PutRun(ctx, Run{ID: "r", Path: "p", Kind: "workflow", Status: "running", Started: time.Now(), Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var lines []LogLine
	for i := 1; i <= MaxLogLines+10; i++ {
		lines = append(lines, LogLine{Seq: int64(i), Time: time.Now(), Level: "info", Message: fmt.Sprint(i)})
	}
	lines[len(lines)-1].Message = strings.Repeat("ä", MaxLineBytes)
	if err := s.AddLog(ctx, "r", lines); err != nil {
		t.Fatal(err)
	}
	d, _, err := s.Get(ctx, "r")
	if err != nil || len(d.Log) != MaxLogLines || d.Log[0].Message != "11" || !d.LogTruncated {
		t.Fatalf("%d lines, first %q, truncated %v, %v", len(d.Log), d.Log[0].Message, d.LogTruncated, err)
	}
	if last := d.Log[len(d.Log)-1].Message; len(last) > MaxLineBytes || !strings.HasSuffix(last, "… (cut)") || !strings.HasPrefix(last, "ää") {
		t.Errorf("long line: %d bytes, %q…", len(last), last[:10])
	}
	big := Run{ID: "big", Path: "p", Kind: "workflow", Status: "running", Started: time.Now(), Data: make([]byte, MaxDataBytes+1)}
	if err := s.PutRun(ctx, big); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("big: %v", err)
	}
}

// A running run without sign of life becomes failed; one with a recent one stays.
func TestMarkStale(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	for _, id := range []string{"old", "live", "done"} {
		status := "running"
		if id == "done" {
			status = "succeeded"
		}
		if err := s.PutRun(ctx, Run{ID: id, Path: "p", Kind: "workflow", Status: status, Started: time.Now(), Data: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-StaleAfter - time.Minute)
	if _, err := s.db.Exec(`UPDATE runs SET heartbeat = ? WHERE id IN ('old', 'done')`, ms(past)); err != nil {
		t.Fatal(err)
	}
	if err := s.Beat(ctx, []string{"live"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Run{}
	for _, r := range list {
		got[r.ID] = r
	}
	if r := got["old"]; r.Status != "failed" || r.Error != StaleError || r.Finished == nil || r.Finished.UnixMilli() != ms(past) {
		t.Errorf("old: %+v", r)
	}
	if got["live"].Status != "running" || got["done"].Status != "succeeded" {
		t.Errorf("live %s, done %s", got["live"].Status, got["done"].Status)
	}
}

// Two stores on the same file (server and runnel run) write at the same time.
func TestTwoProcesses(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	a, b := open(t, dir), open(t, dir)
	errs := make(chan error, 2)
	for i, s := range []*Store{a, b} {
		go func() {
			for j := range 50 {
				id := fmt.Sprintf("%d-%02d", i, j)
				err := s.PutRun(ctx, Run{ID: id, Path: "p", Kind: "workflow", Status: "running", Started: time.Now(), Data: []byte(`{}`)})
				if err == nil {
					err = s.AddLog(ctx, id, []LogLine{{Seq: 1, Time: time.Now(), Level: "info", Message: "x"}})
				}
				if err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if list, err := a.List(ctx, 1000); err != nil || len(list) != 100 {
		t.Errorf("%d runs, %v", len(list), err)
	}
}

// A file of a newer runnel is not changed.
func TestNewerVersion(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(migrations)+1)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := Open(context.Background(), dir); !errors.Is(err, ErrNewer) {
		t.Errorf("newer: %v", err)
	}
	db, _ := sql.Open("sqlite", filepath.Join(dir, FileName))
	defer func() { _ = db.Close() }()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != len(migrations)+1 {
		t.Errorf("version %d %v", v, err)
	}
}

func TestDefaultBase(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	t.Setenv("LOCALAPPDATA", `C:\Users\a\AppData\Local`)
	base, err := DefaultBase()
	if err != nil || !strings.HasSuffix(base, filepath.Join("runnel", "history")) {
		t.Errorf("%s %v", base, err)
	}
}

// Find filters by status, path and trigger and pages with before (S26b).
func TestFind(t *testing.T) {
	ctx := context.Background()
	s := open(t, t.TempDir())
	for _, r := range []Run{
		{ID: "01", Path: "a", Trigger: "cli", Status: "succeeded"},
		{ID: "02", Path: "b", Trigger: "cron:n", Status: "failed"},
		{ID: "03", Path: "a", Trigger: "cron:n", Status: "failed"},
		{ID: "04", Path: "a", Trigger: "cli", Status: "failed"},
	} {
		r.Kind, r.Started, r.Data = "workflow", time.Now(), []byte(`{}`)
		if err := s.PutRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(f Filter) string {
		list, err := s.Find(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range list {
			out = append(out, r.ID)
		}
		return strings.Join(out, ",")
	}
	for _, tt := range []struct {
		f    Filter
		want string
	}{
		{Filter{}, "04,03,02,01"},
		{Filter{Status: "failed"}, "04,03,02"},
		{Filter{Path: "a", Status: "failed"}, "04,03"},
		{Filter{Trigger: "cron:n"}, "03,02"},
		{Filter{Limit: 2}, "04,03"},
		{Filter{Limit: 2, Before: "03"}, "02,01"},
		{Filter{Path: "x"}, ""},
	} {
		if got := ids(tt.f); got != tt.want {
			t.Errorf("%+v: %s, want %s", tt.f, got, tt.want)
		}
	}
}
