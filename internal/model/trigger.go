package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // time zones also on Windows, where Go finds no zone database

	"github.com/robfig/cron/v3"

	"github.com/runnelhq/runnel/internal/expr"
)

// KindTrigger is the kind of the files in triggers/.
const KindTrigger = "Trigger"

// TriggersDir is the folder of the trigger files in a project.
const TriggersDir = "triggers"

// Trigger types (KONZEPT, chapter 2.7). Only cron is supported yet (roadmap S37);
// file and interval follow with S38, http with S38a (ADR 0018).
const (
	TriggerCron     = "cron"
	TriggerFile     = "file"
	TriggerInterval = "interval"
	TriggerHTTP     = "http"
)

// Overlap modes of a trigger whose previous run is still running.
const (
	// OverlapSkip does not start a run while the previous one runs (the default).
	OverlapSkip = "skip"
	// OverlapAllow starts runs regardless of the previous ones.
	OverlapAllow = "allow"
	// OverlapQueue would start the run after the previous one; it needs the run queue
	// (backlog) and is not supported yet.
	OverlapQueue = "queue"
)

// Trigger is the content of a file in triggers/: it starts a workflow or a pipeline
// automatically, e.g. on a schedule (KONZEPT, chapter 5.3).
type Trigger struct {
	Kind        string `yaml:"kind" json:"kind"`
	APIVersion  string `yaml:"apiVersion" json:"apiVersion"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Type is cron (S37); file, interval and http follow.
	Type string `yaml:"type" json:"type"`
	// Target is the workflow or pipeline to start, relative to the project directory.
	Target string `yaml:"target" json:"target"`
	// Environment of the runs; empty: the project's default environment.
	Environment string `yaml:"environment,omitempty" json:"environment,omitempty"`
	// Params are the parameter values given to the target.
	Params map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
	// Enabled is false to keep the trigger in the project without running it.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// With holds the settings of the type.
	With TriggerSettings `yaml:"with,omitempty" json:"with,omitzero"`

	src      *source
	schedule cron.Schedule
	location *time.Location
}

// TriggerSettings are the settings of a trigger under with:, by type.
type TriggerSettings struct {
	// Schedule is a cron expression (minute hour day-of-month month day-of-week), e.g.
	// "0 2 * * 1-5", or @hourly, @daily, @weekly, @monthly, @yearly (cron).
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	// Timezone of the schedule, e.g. Europe/Berlin; empty: the time zone of the computer.
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	// Overlap decides what happens while the previous run of the trigger still runs:
	// skip (default) or allow.
	Overlap string `yaml:"overlap,omitempty" json:"overlap,omitempty"`
}

// IsEnabled reports whether the trigger starts runs.
func (t *Trigger) IsEnabled() bool { return t.Enabled == nil || *t.Enabled }

// OverlapMode returns the overlap mode, skip if not given.
func (t *Trigger) OverlapMode() string {
	if t.With.Overlap == "" {
		return OverlapSkip
	}
	return t.With.Overlap
}

// File returns the name of the trigger file as given to ParseTrigger.
func (t *Trigger) File() string { return t.src.name }

// Next returns the first time of the schedule after the given one (cron), in the
// time zone of the trigger; zero if the trigger has no valid schedule.
func (t *Trigger) Next(after time.Time) time.Time {
	if t.schedule == nil {
		return time.Time{}
	}
	return t.schedule.Next(after.In(t.location))
}

// ErrorAt creates an error located at a YAML path of the trigger file.
func (t *Trigger) ErrorAt(path, format string, args ...any) *Error {
	return t.src.errorf(path, format, args...)
}

// LoadTrigger reads and parses a trigger file.
func LoadTrigger(path string) (*Trigger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseTrigger(filepath.ToSlash(path), data)
}

// ParseTrigger parses a trigger from YAML and checks its fields; name is used in
// messages. References to other files (target, environment, parameters) are checked
// with the project (runs.CheckTrigger).
func ParseTrigger(name string, data []byte) (*Trigger, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	t := &Trigger{src: src}
	if err := src.decode(t); err != nil {
		return nil, err
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return t, nil
}

// cronParser reads the five fields of standard cron and the descriptors (@daily …).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func (t *Trigger) validate() error {
	s := t.src
	errs := checkHeader(s, t.Kind, KindTrigger, t.APIVersion, t.Name)
	if t.Name != "" && !idPattern.MatchString(t.Name) {
		errs = append(errs, s.errorf("$.name", "invalid trigger name %q (letters, digits, - and _, starting with a letter)", t.Name))
	}
	switch t.Type {
	case TriggerCron:
	case "":
		errs = append(errs, s.errorf("$", `missing required field "type" (expected cron)`))
	case TriggerFile, TriggerInterval:
		errs = append(errs, s.errorf("$.type", "trigger type %q is not supported yet (roadmap S38)", t.Type))
	case TriggerHTTP:
		errs = append(errs, s.errorf("$.type", "trigger type %q is not supported yet (roadmap S38a, ADR 0018)", t.Type))
	default:
		errs = append(errs, s.errorf("$.type", "unknown trigger type %q (expected cron)", t.Type))
	}
	switch {
	case t.Target == "":
		errs = append(errs, s.errorf("$", `missing required field "target" (a workflow or pipeline file, relative to the project)`))
	case !strings.HasSuffix(t.Target, ".workflow.yaml") && !strings.HasSuffix(t.Target, ".pipeline.yaml"):
		errs = append(errs, s.errorf("$.target", "target %q must be a workflow (*.workflow.yaml) or pipeline (*.pipeline.yaml) file", t.Target))
	case filepath.IsAbs(t.Target) || slices.Contains(strings.Split(filepath.ToSlash(t.Target), "/"), ".."):
		errs = append(errs, s.errorf("$.target", "target %q must be a path inside the project, relative to its directory", t.Target))
	}
	for _, key := range sortedKeys(t.Params) {
		if v, ok := t.Params[key].(string); ok && expr.Contains(v) {
			errs = append(errs, s.errorf(keyPath("$.params", key), "parameter %s: expressions {{ … }} are not supported in triggers; use a fixed value or ${…}", key))
		}
	}
	if t.Type == TriggerCron {
		errs = append(errs, t.checkCron()...)
	}
	return errs.errOrNil()
}

func (t *Trigger) checkCron() Errors {
	s, w := t.src, t.With
	var errs Errors
	t.location = time.Local
	if w.Timezone != "" {
		loc, err := time.LoadLocation(w.Timezone)
		if err != nil {
			errs = append(errs, s.errorf("$.with.timezone", "unknown time zone %q (e.g. Europe/Berlin or UTC)", w.Timezone))
		} else {
			t.location = loc
		}
	}
	switch spec := strings.TrimSpace(w.Schedule); {
	case spec == "":
		errs = append(errs, s.errorf("$.with", `missing required field "schedule" (a cron expression, e.g. "0 2 * * 1-5")`))
	case strings.HasPrefix(spec, "@every"):
		errs = append(errs, s.errorf("$.with.schedule", "@every is not a schedule; repeating at a fixed interval follows with type interval (roadmap S38)"))
	case strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ="):
		errs = append(errs, s.errorf("$.with.schedule", "set the time zone with timezone: instead of in the schedule"))
	default:
		schedule, err := cronParser.Parse(spec)
		if err != nil {
			errs = append(errs, s.errorf("$.with.schedule", "invalid schedule %q: %s (expected minute hour day-of-month month day-of-week, e.g. \"0 2 * * 1-5\")", spec, cronError(err)))
		} else {
			t.schedule = schedule
		}
	}
	switch w.Overlap {
	case "", OverlapSkip, OverlapAllow:
	case OverlapQueue:
		errs = append(errs, s.errorf("$.with.overlap", "overlap: queue is not supported yet (the run queue is in the backlog); use skip or allow"))
	default:
		errs = append(errs, s.errorf("$.with.overlap", "invalid overlap %q (expected skip or allow)", w.Overlap))
	}
	return errs
}

// cronError shortens the messages of the cron library.
func cronError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "failed to parse") {
		msg = msg[i+2:]
	}
	return msg
}

// LoadTriggers reads the trigger files of a project (triggers/*.yaml), sorted by file
// name. Files with problems are left out; their problems are returned as Errors, as are
// trigger names used more than once.
func LoadTriggers(projectDir string) ([]*Trigger, error) {
	files, err := filepath.Glob(filepath.Join(projectDir, TriggersDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var out []*Trigger
	var errs Errors
	names := map[string]string{}
	for _, file := range files {
		t, err := LoadTrigger(file)
		var es Errors
		switch {
		case errors.As(err, &es):
			errs = append(errs, es...)
			continue
		case err != nil:
			return nil, err
		}
		if first, dup := names[t.Name]; dup {
			errs = append(errs, t.ErrorAt("$.name", "trigger name %q is already used by %s", t.Name, filepath.Base(first)))
			continue
		}
		names[t.Name] = file
		out = append(out, t)
	}
	return out, errs.errOrNil()
}

// IsTriggerFile reports whether a path relative to the project is a trigger file.
func IsTriggerFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, TriggersDir+"/") && strings.HasSuffix(rel, ".yaml") && !strings.Contains(rel[len(TriggersDir)+1:], "/")
}

// String describes the schedule for messages, e.g. "0 2 * * 1-5 (Europe/Berlin)".
func (t *Trigger) String() string {
	return fmt.Sprintf("%s (%s)", t.With.Schedule, t.location)
}
