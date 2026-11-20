package triggers

import (
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
	running map[string]bool
	logs    []string
	fail    error
}

func (f *fake) start(t *model.Trigger) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	id := fmt.Sprintf("run-%d", len(f.started)+1)
	f.started = append(f.started, t.Name)
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
		"workflows/job.workflow.yaml": "kind: Workflow\napiVersion: runnel/v1\nname: job\nactions:\n  - { id: start, type: core.start }\n",
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
