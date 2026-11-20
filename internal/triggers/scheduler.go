// Package triggers starts the runs of the triggers of a project at their times
// (roadmap S37): it reads the trigger files, works out the next time of each and starts
// the target when it is due.
package triggers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
)

// PollInterval is how often the scheduler looks for changed trigger files.
var PollInterval = 5 * time.Second

// Options configure a Scheduler.
type Options struct {
	// Dir is the project directory.
	Dir string
	// Start starts a run of the trigger's target and returns its id.
	Start func(t *model.Trigger) (string, error)
	// Running reports whether a run is still running (for overlap: skip).
	Running func(runID string) bool
	// Log receives messages for the server log, e.g. started and skipped runs.
	Log func(msg string)
	// Now returns the current time; defaults to time.Now (tests use a simulated clock).
	Now func() time.Time
}

// Scheduler starts the runs of the triggers of a project. Missed times (while the
// server was not running) are not caught up.
type Scheduler struct {
	opts Options

	mu       sync.Mutex
	entries  []*entry // in the order of the files
	problems model.Errors
	sig      string // state of the files the triggers were loaded from
}

type entry struct {
	t      *model.Trigger
	next   time.Time
	last   *Fired
	active []string // runs started by the trigger that may still run
}

// Fired is what happened at the last due time of a trigger.
type Fired struct {
	Time  time.Time `json:"time"`
	RunID string    `json:"runId,omitempty"`
	// Skipped is set when the previous run was still running (overlap: skip).
	Skipped bool   `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Info describes a trigger for the API and the command line.
type Info struct {
	Name     string    `json:"name"`
	File     string    `json:"file"`
	Type     string    `json:"type"`
	Target   string    `json:"target"`
	Schedule string    `json:"schedule,omitempty"`
	Timezone string    `json:"timezone,omitempty"`
	Enabled  bool      `json:"enabled"`
	Next     time.Time `json:"next,omitzero"`
	Last     *Fired    `json:"last,omitempty"`
}

// New creates a scheduler; call Reload before Tick, or Run.
func New(opts Options) *Scheduler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = func(string) {}
	}
	return &Scheduler{opts: opts}
}

// Reload reads the trigger files and the project again. Triggers with problems are not
// active; their problems are returned by Problems and logged. Unchanged triggers keep
// their state; all due times are worked out anew from now.
func (s *Scheduler) Reload() {
	sig := s.signature()
	triggers, problems := Load(s.opts.Dir)
	now := s.opts.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	old := map[string]*entry{}
	for _, e := range s.entries {
		old[e.t.Name] = e
	}
	s.entries = nil
	for _, t := range triggers {
		e := &entry{t: t}
		if prev := old[t.Name]; prev != nil {
			e.last, e.active = prev.last, prev.active
		}
		if t.IsEnabled() {
			e.next = t.Next(now)
		}
		s.entries = append(s.entries, e)
	}
	s.problems, s.sig = problems, sig
	for _, p := range problems {
		s.opts.Log(p.Error())
	}
	active := 0
	for _, e := range s.entries {
		if !e.next.IsZero() {
			active++
		}
	}
	if len(triggers) > 0 || len(problems) > 0 {
		s.opts.Log(fmt.Sprintf("triggers: %d active, %d disabled, %d with problems", active, len(triggers)-active, len(problemFiles(problems))))
	}
}

// Load reads the triggers of a project and checks them against the project; triggers
// with problems are left out and their problems returned.
func Load(dir string) ([]*model.Trigger, model.Errors) {
	var problems model.Errors
	collect := func(err error) {
		var es model.Errors
		if errors.As(err, &es) {
			problems = append(problems, es...)
		} else if err != nil {
			problems = append(problems, &model.Error{File: filepath.Join(dir, model.TriggersDir), Msg: err.Error()})
		}
	}
	p, err := model.LoadProject(dir)
	collect(err)
	triggers, err := model.LoadTriggers(dir)
	collect(err)
	var ok []*model.Trigger
	for _, t := range triggers {
		if errs := runs.CheckTrigger(t, p, dir); len(errs) > 0 {
			problems = append(problems, errs...)
			continue
		}
		ok = append(ok, t)
	}
	problems.Sort()
	return ok, problems
}

func problemFiles(problems model.Errors) map[string]bool {
	files := map[string]bool{}
	for _, p := range problems {
		files[p.File] = true
	}
	return files
}

// Tick starts the runs of the triggers that are due at now and returns the next time
// a trigger is due (zero if none is).
func (s *Scheduler) Tick(now time.Time) time.Time {
	s.mu.Lock()
	var due []*entry
	for _, e := range s.entries {
		if !e.next.IsZero() && !e.next.After(now) {
			due = append(due, e)
		}
	}
	s.mu.Unlock()

	// Runs are started without holding the lock: starting reads files.
	for _, e := range due {
		s.fire(e, now)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var next time.Time
	for _, e := range s.entries {
		if !e.next.IsZero() && (next.IsZero() || e.next.Before(next)) {
			next = e.next
		}
	}
	return next
}

func (s *Scheduler) fire(e *entry, now time.Time) {
	t := e.t
	s.mu.Lock()
	e.next = t.Next(now) // a time missed while sleeping counts once
	e.active = slices.DeleteFunc(e.active, func(id string) bool { return !s.opts.Running(id) })
	busy := len(e.active) > 0 && t.OverlapMode() == model.OverlapSkip
	s.mu.Unlock()

	f := &Fired{Time: now}
	if busy {
		f.Skipped = true
		s.opts.Log(fmt.Sprintf("trigger %q: skipped, the previous run %s is still running (overlap: skip)", t.Name, e.active[0]))
	} else if id, err := s.opts.Start(t); err != nil {
		f.Error = err.Error()
		s.opts.Log(fmt.Sprintf("trigger %q: %s was not started: %v", t.Name, t.Target, err))
	} else {
		f.RunID = id
		s.opts.Log(fmt.Sprintf("trigger %q: started %s, run %s", t.Name, t.Target, id))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	e.last = f
	if f.RunID != "" {
		e.active = append(e.active, f.RunID)
	}
}

// Run reloads the triggers and starts their runs until ctx is done. Changed trigger
// files (and project files) are picked up within PollInterval.
func (s *Scheduler) Run(ctx context.Context) {
	s.Reload()
	for {
		now := s.opts.Now()
		next := s.Tick(now)
		wait := PollInterval
		if !next.IsZero() {
			wait = min(wait, max(next.Sub(s.opts.Now()), 0))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if s.signature() != s.currentSig() {
			s.Reload()
		}
	}
}

func (s *Scheduler) currentSig() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sig
}

// signature describes the state of the files triggers depend on: the trigger files,
// project.yaml and the environments.
func (s *Scheduler) signature() string {
	var b strings.Builder
	add := func(pattern string) {
		files, _ := filepath.Glob(filepath.Join(s.opts.Dir, pattern))
		slices.Sort(files)
		for _, f := range files {
			if info, err := os.Stat(f); err == nil {
				fmt.Fprintf(&b, "%s %d %d\n", f, info.Size(), info.ModTime().UnixNano())
			}
		}
	}
	add(model.ProjectFile)
	add(filepath.Join(model.EnvironmentsDir, "*.yaml"))
	add(filepath.Join(model.TriggersDir, "*.yaml"))
	return b.String()
}

// List returns the triggers with their next time and last result.
func (s *Scheduler) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, 0, len(s.entries))
	for _, e := range s.entries {
		info := InfoOf(e.t, e.next, e.last)
		if rel, err := filepath.Rel(s.opts.Dir, e.t.File()); err == nil {
			info.File = filepath.ToSlash(rel)
		}
		out = append(out, info)
	}
	return out
}

// Problems returns the problems of the trigger files found by the last Reload.
func (s *Scheduler) Problems() model.Errors {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.problems)
}

// InfoOf describes a trigger.
func InfoOf(t *model.Trigger, next time.Time, last *Fired) Info {
	tz := t.With.Timezone
	if tz == "" && t.Type == model.TriggerCron {
		tz = "Local"
	}
	return Info{Name: t.Name, File: filepath.ToSlash(t.File()), Type: t.Type, Target: t.Target, Schedule: t.With.Schedule,
		Timezone: tz, Enabled: t.IsEnabled(), Next: next, Last: last}
}
