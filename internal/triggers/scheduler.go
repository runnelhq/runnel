// Package triggers starts the runs of the triggers of a project: at the times of a
// cron schedule (roadmap S37), at a fixed interval and for new files in a folder (S38).
// It reads the trigger files, works out when each is due and starts the target.
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
	// Start starts a run of the trigger's target and returns its id. params are set in
	// addition to those of the trigger (the path of a file for file triggers, the values
	// of a request for http triggers); client is the address of the caller (http).
	Start func(t *model.Trigger, params map[string]string, client string) (string, error)
	// Running reports whether a run is still running (for overlap: skip).
	Running func(runID string) bool
	// Log receives messages for the server log, e.g. started and skipped runs.
	Log func(msg string)
	// Warn receives problems for the server log: problems of trigger files, runs that
	// were not started, folders that cannot be read; defaults to Log.
	Warn func(msg string)
	// Now returns the current time; defaults to time.Now (tests use a simulated clock).
	Now func() time.Time
	// Listen is the address of the listener of http triggers (S38a), e.g.
	// 127.0.0.1:7301; it is opened once an http trigger is enabled. Empty: none.
	Listen string
}

// Scheduler starts the runs of the triggers of a project. Missed times (while the
// server was not running) are not caught up.
type Scheduler struct {
	opts Options

	mu       sync.Mutex
	entries  []*entry // in the order of the files
	problems model.Errors
	sig      string // state of the files the triggers were loaded from

	listener listener // http triggers
}

type entry struct {
	t      *model.Trigger
	next   time.Time
	last   *Fired
	active []string // runs started by the trigger that may still run
	files  *watch   // file triggers
	// startMu makes counting the running runs and starting one atomic (http).
	startMu sync.Mutex
}

// Fired is what happened at the last due time of a trigger.
type Fired struct {
	Time  time.Time `json:"time"`
	RunID string    `json:"runId,omitempty"`
	// Files are the files the run was started for (file triggers).
	Files []string `json:"files,omitempty"`
	// Skipped is set when the previous run was still running (overlap: skip).
	Skipped bool   `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Info describes a trigger for the API and the command line.
type Info struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Type   string `json:"type"`
	Target string `json:"target"`
	// Schedule tells when the trigger starts runs: the cron expression, "every 15m" or
	// "new *.csv in inbox".
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
	if opts.Warn == nil {
		opts.Warn = opts.Log
	}
	return &Scheduler{opts: opts}
}

// Reload reads the trigger files and the project again. Triggers with problems are not
// active; their problems are returned by Problems and logged. Triggers keep their last
// result and running runs; cron times are worked out anew from now, an unchanged
// interval keeps its next time and a file trigger watching the same files keeps the
// files it has processed.
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
		prev := old[t.Name]
		if prev != nil {
			e.last, e.active = prev.last, prev.active
		}
		if t.IsEnabled() {
			switch {
			case t.Type == model.TriggerFile && prev != nil && prev.files != nil && sameWatch(prev.t, t):
				e.files, e.next = prev.files, prev.next
			case t.Type == model.TriggerFile:
				e.files, e.next = newWatch(t, s.opts.Dir), now
			case t.Type == model.TriggerInterval && prev != nil && !prev.next.IsZero() && prev.t.Type == t.Type && prev.t.EveryValue() == t.EveryValue():
				e.next = prev.next
			default:
				e.next = t.Next(now) // an interval runs first after one interval
			}
		}
		s.entries = append(s.entries, e)
	}
	s.problems, s.sig = problems, sig
	for _, p := range problems {
		s.opts.Warn(p.Error())
	}
	active := 0
	for _, e := range s.entries {
		if e.t.IsEnabled() {
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
	if t.Type == model.TriggerFile {
		e.next = now.Add(t.PollInterval())
	} else {
		e.next = t.Next(now) // a time missed while sleeping counts once
	}
	s.mu.Unlock()
	if t.Type == model.TriggerFile {
		s.checkFolder(e, now)
		return
	}
	s.start(e, now, nil, nil)
}

// start starts a run of the trigger unless overlap: skip prevents it and reports
// whether it was started. params are set in addition to those of the trigger; files
// are the files the run is for (file triggers). A file trigger logs a skip or an
// error only once while it repeats at each check.
func (s *Scheduler) start(e *entry, now time.Time, params map[string]string, files []string) bool {
	t := e.t
	s.mu.Lock()
	e.active = slices.DeleteFunc(e.active, func(id string) bool { return !s.opts.Running(id) })
	busy := len(e.active) > 0 && t.OverlapMode() == model.OverlapSkip
	var running string
	if busy {
		running = e.active[0]
	}
	repeat := func(f *Fired) bool {
		return t.Type == model.TriggerFile && f.RunID == "" && e.last != nil && e.last.RunID == "" &&
			e.last.Skipped == f.Skipped && e.last.Error == f.Error
	}
	s.mu.Unlock()

	f := &Fired{Time: now}
	what := t.Target
	switch len(files) {
	case 0:
	case 1:
		what += " for " + filepath.Base(files[0])
	default:
		what += fmt.Sprintf(" for %d files", len(files))
	}
	f.Files = files
	var msg string
	if busy {
		f.Skipped = true
		msg = fmt.Sprintf("trigger %q: skipped, the previous run %s is still running (overlap: skip)", t.Name, running)
	} else if id, err := s.opts.Start(t, params, ""); err != nil {
		f.Error = err.Error()
		msg = fmt.Sprintf("trigger %q: %s was not started: %v", t.Name, what, err)
	} else {
		f.RunID = id
		msg = fmt.Sprintf("trigger %q: started %s, run %s", t.Name, what, id)
	}

	s.mu.Lock()
	quiet := repeat(f)
	e.last = f
	if f.RunID != "" {
		e.active = append(e.active, f.RunID)
	}
	s.mu.Unlock()
	switch {
	case quiet:
	case f.Error != "":
		s.opts.Warn(msg)
	default:
		s.opts.Log(msg)
	}
	return f.RunID != ""
}

// Run reloads the triggers and starts their runs until ctx is done. Changed trigger
// files (and project files) are picked up within PollInterval.
func (s *Scheduler) Run(ctx context.Context) {
	s.Reload()
	defer s.listener.close()
	for {
		s.listen()
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
	schedule := t.With.Schedule
	if t.Type != model.TriggerCron {
		schedule = t.Describe()
	}
	return Info{Name: t.Name, File: filepath.ToSlash(t.File()), Type: t.Type, Target: t.Target, Schedule: schedule,
		Timezone: tz, Enabled: t.IsEnabled(), Next: next, Last: last}
}
