package triggers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/model"
)

// fake records the runs the scheduler starts and decides which are still running.
type fake struct {
	mu      sync.Mutex
	started []string // trigger names
	params  []map[string]string
	clients []string
	running map[string]bool
	logs    []string
	fail    error
}

func (f *fake) start(t *model.Trigger, params map[string]string, client string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	id := fmt.Sprintf("run-%d", len(f.started)+1)
	f.started = append(f.started, t.Name)
	f.params = append(f.params, params)
	f.clients = append(f.clients, client)
	f.running[id] = true
	return id, nil
}

func (f *fake) isRunning(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[id]
}

func (f *fake) log(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, msg)
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func setup(t *testing.T, triggers map[string]string) (string, *fake, *Scheduler, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"project.yaml":                "kind: Project\napiVersion: runnel/v1\nname: p\n",
		"workflows/job.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: job\nparameters:\n  - { name: FILE }\n  - { name: FILES, type: json }\nactions:\n  - { id: start, type: core.start }\n",
	}
	for name, content := range triggers {
		files["triggers/"+name] = content
	}
	for name, content := range files {
		write(t, dir, name, content)
	}
	f := &fake{running: map[string]bool{}}
	now := time.Date(2026, 11, 20, 10, 0, 30, 0, time.UTC)
	s := New(Options{Dir: dir, Start: f.start, Running: f.isRunning, Log: f.log, Now: func() time.Time { return now }})
	s.Reload()
	return dir, f, s, &now
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cronTrigger(name, schedule, extra string) string {
	return fmt.Sprintf("kind: Trigger\napiVersion: runnel/v1\nname: %s\ntype: cron\ntarget: workflows/job.workflow.yaml\n%swith:\n  schedule: %q\n  timezone: UTC\n%s",
		name, "", schedule, extra)
}

// Runs start when they are due, once per due time; the clock is simulated.
func TestSchedulerStartsDueRuns(t *testing.T) {
	_, f, s, now := setup(t, map[string]string{"every.yaml": cronTrigger("every", "*/15 * * * *", "")})
	next := s.Tick(*now)
	if f.count() != 0 || !next.Equal(time.Date(2026, 11, 20, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("before: %d runs, next %v", f.count(), next)
	}
	// Due: one run; the next time is a quarter of an hour later.
	*now = next
	next = s.Tick(*now)
	if f.count() != 1 || !next.Equal(time.Date(2026, 11, 20, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("at 10:15: %d runs, next %v", f.count(), next)
	}
	// Waking late (e.g. after sleep of the computer): missed times count once.
	f.running = map[string]bool{}
	*now = time.Date(2026, 11, 20, 11, 20, 0, 0, time.UTC)
	next = s.Tick(*now)
	if f.count() != 2 || !next.Equal(time.Date(2026, 11, 20, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("late: %d runs, next %v", f.count(), next)
	}
	list := s.List()
	if len(list) != 1 || list[0].Last == nil || list[0].Last.RunID != "run-2" || list[0].File != "triggers/every.yaml" || !list[0].Next.Equal(next) {
		t.Errorf("list %+v", list)
	}
}

// overlap: skip does not start a run while the previous one runs; allow does.
func TestSchedulerOverlap(t *testing.T) {
	_, f, s, now := setup(t, map[string]string{
		"skip.yaml":  cronTrigger("skip", "* * * * *", ""),
		"allow.yaml": cronTrigger("allow", "* * * * *", "  overlap: allow\n"),
	})
	*now = time.Date(2026, 11, 20, 10, 1, 0, 0, time.UTC)
	s.Tick(*now)
	*now = time.Date(2026, 11, 20, 10, 2, 0, 0, time.UTC)
	s.Tick(*now) // both runs of 10:01 still run
	if got := strings.Join(f.started, ","); got != "allow,skip,allow" {
		t.Errorf("started %s", got)
	}
	var skip Info
	for _, i := range s.List() {
		if i.Name == "skip" {
			skip = i
		}
	}
	if skip.Last == nil || !skip.Last.Skipped || !strings.Contains(strings.Join(f.logs, "\n"), `trigger "skip": skipped, the previous run run-2 is still running (overlap: skip)`) {
		t.Errorf("skip: %+v, logs %v", skip.Last, f.logs)
	}
	// Once it has ended, the next time starts a run again.
	f.running = map[string]bool{}
	*now = time.Date(2026, 11, 20, 10, 3, 0, 0, time.UTC)
	s.Tick(*now)
	if f.count() != 5 {
		t.Errorf("after the end: %v", f.started)
	}
}

// Disabled triggers and triggers with problems do not start runs; a failed start is recorded.
func TestSchedulerDisabledAndProblems(t *testing.T) {
	_, f, s, now := setup(t, map[string]string{
		"off.yaml":     cronTrigger("off", "* * * * *", "") + "enabled: false\n",
		"missing.yaml": strings.Replace(cronTrigger("missing", "* * * * *", ""), "workflows/job.workflow.yaml", "workflows/none.workflow.yaml", 1),
		"on.yaml":      cronTrigger("on", "* * * * *", ""),
	})
	if p := s.Problems(); len(p) != 1 || !strings.Contains(p.Error(), "target workflows/none.workflow.yaml not found") {
		t.Errorf("problems %v", p)
	}
	if !strings.Contains(strings.Join(f.logs, "\n"), "triggers: 1 active, 1 disabled, 1 with problems") {
		t.Errorf("logs %v", f.logs)
	}
	f.fail = fmt.Errorf("limit reached")
	*now = time.Date(2026, 11, 20, 10, 1, 0, 0, time.UTC)
	s.Tick(*now)
	list := s.List()
	if len(list) != 2 || list[0].Name != "off" || list[0].Enabled || !list[0].Next.IsZero() || list[1].Last == nil || list[1].Last.Error != "limit reached" {
		t.Errorf("list %+v", list)
	}
}

// Changed trigger files are picked up: a new schedule applies from now on.
func TestSchedulerReload(t *testing.T) {
	dir, f, s, now := setup(t, map[string]string{"t.yaml": cronTrigger("t", "0 12 * * *", "")})
	sig := s.currentSig()
	write(t, dir, "triggers/t.yaml", cronTrigger("t", "0 11 * * *", ""))
	// The modification time may not change within the resolution of the file system.
	if err := os.Chtimes(filepath.Join(dir, "triggers/t.yaml"), time.Now().Add(time.Minute), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s.signature() == sig {
		t.Fatal("the change was not noticed")
	}
	s.Reload()
	if next := s.Tick(*now); !next.Equal(time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC)) || f.count() != 0 {
		t.Errorf("next %v", next)
	}
}

func intervalTrigger(name, every, extra string) string {
	return fmt.Sprintf("kind: Trigger\napiVersion: runnel/v1\nname: %s\ntype: interval\ntarget: workflows/job.workflow.yaml\nwith:\n  every: %s\n%s", name, every, extra)
}

func fileTrigger(name, extra string) string {
	return fmt.Sprintf("kind: Trigger\napiVersion: runnel/v1\nname: %s\ntype: file\ntarget: workflows/job.workflow.yaml\nwith:\n  path: inbox\n  pattern: \"*.csv\"\n  param: FILE\n%s", name, extra)
}

// An interval trigger runs first one interval after the start, then at each interval;
// reloading an unchanged trigger keeps its next time.
func TestSchedulerInterval(t *testing.T) {
	start := time.Date(2026, 11, 20, 10, 0, 30, 0, time.UTC)
	dir, f, s, now := setup(t, map[string]string{"poll.yaml": intervalTrigger("poll", "10m", "")})
	if next := s.Tick(*now); f.count() != 0 || !next.Equal(start.Add(10*time.Minute)) {
		t.Fatalf("at the start: %d runs, next %v", f.count(), next)
	}
	*now = start.Add(10 * time.Minute)
	if next := s.Tick(*now); f.count() != 1 || !next.Equal(start.Add(20*time.Minute)) {
		t.Fatalf("after one interval: %d runs, next %v", f.count(), next)
	}
	// Another trigger file changes: the interval keeps its time.
	write(t, dir, "triggers/other.yaml", cronTrigger("other", "0 0 1 1 *", ""))
	*now = start.Add(15 * time.Minute)
	s.Reload()
	if next := s.Tick(*now); !next.Equal(start.Add(20 * time.Minute)) {
		t.Errorf("after reload: next %v", next)
	}
	if list := s.List(); list[1].Schedule != "every 10m" || list[1].Timezone != "" {
		t.Errorf("info %+v", list[1])
	}
}

// check advances the clock by the poll interval and runs the due checks.
func check(s *Scheduler, now *time.Time) {
	*now = now.Add(model.DefaultFileInterval)
	s.Tick(*now)
}

// A file trigger starts one run per new file once the file is unchanged over two
// checks; the path is given in the parameter. Changed files count again.
func TestSchedulerFileEach(t *testing.T) {
	dir, f, s, now := setup(t, map[string]string{"in.yaml": fileTrigger("in", "  overlap: allow\n")})
	inbox := filepath.Join(dir, "inbox")
	write(t, dir, "inbox/b.csv", "1")
	write(t, dir, "inbox/A.CSV", "1")
	write(t, dir, "inbox/c.txt", "1")
	s.Tick(*now) // files there at the start are processed, but first seen
	if f.count() != 0 {
		t.Fatalf("first check: %v", f.started)
	}
	check(s, now)
	if f.count() != 2 || f.params[0]["FILE"] != filepath.Join(inbox, "A.CSV") || f.params[1]["FILE"] != filepath.Join(inbox, "b.csv") {
		t.Fatalf("second check: %v", f.params)
	}
	check(s, now)
	if f.count() != 2 {
		t.Fatalf("processed again: %v", f.params)
	}
	// A file still being written waits until it is unchanged.
	write(t, dir, "inbox/d.csv", "1")
	check(s, now)
	write(t, dir, "inbox/d.csv", "12")
	check(s, now)
	if f.count() != 2 {
		t.Fatalf("growing file: %v", f.params)
	}
	check(s, now)
	if f.count() != 3 || f.params[2]["FILE"] != filepath.Join(inbox, "d.csv") {
		t.Fatalf("written file: %v", f.params)
	}
	// A changed file counts as new.
	write(t, dir, "inbox/b.csv", "22")
	check(s, now)
	check(s, now)
	if f.count() != 4 || f.params[3]["FILE"] != filepath.Join(inbox, "b.csv") {
		t.Errorf("changed file: %v", f.params)
	}
	last := s.List()[0].Last
	if last == nil || last.RunID != "run-4" || len(last.Files) != 1 || !strings.Contains(strings.Join(f.logs, "\n"), `trigger "in": started workflows/job.workflow.yaml for b.csv, run run-4`) {
		t.Errorf("last %+v, logs %v", last, f.logs)
	}
}

// overlap (skip by default): while a run runs, ready files wait and are started one
// after the other; a rejected start is tried again at the next check, logged once.
func TestSchedulerFileOverlapAndErrors(t *testing.T) {
	dir, f, s, now := setup(t, map[string]string{"in.yaml": fileTrigger("in", "")})
	write(t, dir, "inbox/a.csv", "1")
	write(t, dir, "inbox/b.csv", "1")
	s.Tick(*now)
	check(s, now)
	if f.count() != 1 {
		t.Fatalf("one at a time: %v", f.started)
	}
	check(s, now)
	if f.count() != 1 || !s.List()[0].Last.Skipped {
		t.Fatalf("while running: %v", f.started)
	}
	f.running = map[string]bool{}
	f.fail = fmt.Errorf("limit reached")
	check(s, now)
	check(s, now)
	if n := strings.Count(strings.Join(f.logs, "\n"), "limit reached"); n != 1 {
		t.Errorf("logged %d times: %v", n, f.logs)
	}
	f.fail = nil
	check(s, now)
	if f.count() != 2 || f.params[1]["FILE"] != filepath.Join(dir, "inbox", "b.csv") {
		t.Errorf("after the error: %v", f.params)
	}
}

// mode: batch starts one run for all ready files with their paths as a JSON list;
// existing: ignore leaves the files there at the start alone.
func TestSchedulerFileBatchIgnore(t *testing.T) {
	dir, f, s, now := setup(t, map[string]string{"in.yaml": strings.Replace(fileTrigger("in", "  mode: batch\n  existing: ignore\n"), "param: FILE", "param: FILES", 1)})
	write(t, dir, "inbox/old.csv", "1")
	s.Tick(*now)
	check(s, now)
	if f.count() != 0 {
		t.Fatalf("existing file processed: %v", f.params)
	}
	write(t, dir, "inbox/x.csv", "1")
	write(t, dir, "inbox/y.csv", "1")
	check(s, now)
	check(s, now)
	var files []string
	if f.count() != 1 || json.Unmarshal([]byte(f.params[0]["FILES"]), &files) != nil || len(files) != 2 || filepath.Base(files[1]) != "y.csv" {
		t.Fatalf("batch: %v", f.params)
	}
	if !strings.Contains(strings.Join(f.logs, "\n"), "for 2 files, run run-1") {
		t.Errorf("logs %v", f.logs)
	}
}

// A missing folder is logged once and watched again once it is there; reloading the
// unchanged trigger keeps the processed files.
func TestSchedulerFileFolderAndReload(t *testing.T) {
	dir, f, s, now := setup(t, map[string]string{"in.yaml": fileTrigger("in", "")})
	s.Tick(*now)
	check(s, now)
	if n := strings.Count(strings.Join(f.logs, "\n"), "inbox not found; trying again every 10s"); n != 1 {
		t.Errorf("logged %d times: %v", n, f.logs)
	}
	write(t, dir, "inbox/a.csv", "1")
	check(s, now)
	check(s, now)
	if f.count() != 1 || !strings.Contains(strings.Join(f.logs, "\n"), "can be read again") {
		t.Fatalf("after creating the folder: %v %v", f.started, f.logs)
	}
	write(t, dir, "triggers/other.yaml", cronTrigger("other", "0 0 1 1 *", ""))
	s.Reload()
	check(s, now)
	check(s, now)
	if f.count() != 1 {
		t.Errorf("processed again after reload: %v", f.params)
	}
	if info := s.List()[0]; info.Schedule != "new *.csv in inbox" || info.Next.IsZero() {
		t.Errorf("info %+v", info)
	}
}
