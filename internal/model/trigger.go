package model

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
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

// Trigger types (KONZEPT, chapter 2.7): cron (roadmap S37), file and interval (S38),
// http (S38a, ADR 0018).
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

// Modes of a file trigger.
const (
	// FileEach starts one run per new file (the default).
	FileEach = "each"
	// FileBatch starts one run for all new files of a check.
	FileBatch = "batch"
)

// Handling of the files already in the folder when the server starts (file triggers).
const (
	// ExistingProcess treats them as new (the default).
	ExistingProcess = "process"
	// ExistingIgnore only starts runs for files that appear or change later.
	ExistingIgnore = "ignore"
)

// Defaults and limits of file and interval triggers.
const (
	DefaultFileInterval = 10 * time.Second
	MinTriggerInterval  = time.Second
)

// Trigger is the content of a file in triggers/: it starts a workflow or a pipeline
// automatically, e.g. on a schedule (KONZEPT, chapter 5.3).
type Trigger struct {
	Kind        string `yaml:"kind" json:"kind" doc:"Type of the file" schema:"required,enum=Trigger"`
	APIVersion  string `yaml:"apiVersion" json:"apiVersion" doc:"Version of the file format" schema:"required,enum=runnel/v1"`
	Name        string `yaml:"name" json:"name" doc:"Name of the trigger" schema:"required,example=nightly-import"`
	Description string `yaml:"description,omitempty" json:"description,omitempty" doc:"What the trigger starts and why"`
	// Type is cron, interval, file or http.
	Type string `yaml:"type" json:"type" doc:"When the trigger starts runs: cron (schedule), interval (fixed time between runs), file (new files in a folder) or http (a call to an address)" schema:"required,enum=cron|interval|file|http"`
	// Target is the workflow or pipeline to start, relative to the project directory.
	Target string `yaml:"target" json:"target" doc:"Workflow or pipeline to start, relative to the project folder" schema:"required,example=workflows/import.workflow.yaml"`
	// Environment of the runs; empty: the project's default environment.
	Environment string `yaml:"environment,omitempty" json:"environment,omitempty" doc:"Environment of the runs; empty: the default environment of the project" schema:"example=prod"`
	// Params are the parameter values given to the target.
	Params map[string]any `yaml:"params,omitempty" json:"params,omitempty" doc:"Values for the parameters of the target, by name"`
	// Enabled is false to keep the trigger in the project without running it.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty" doc:"false keeps the trigger in the project without starting runs" schema:"default=true"`
	// With holds the settings of the type.
	With TriggerSettings `yaml:"with,omitempty" json:"with,omitzero" doc:"Settings of the trigger type"`

	src      *source
	schedule cron.Schedule
	location *time.Location
}

// TriggerSettings are the settings of a trigger under with:, by type.
type TriggerSettings struct {
	// Schedule is a cron expression (minute hour day-of-month month day-of-week), e.g.
	// "0 2 * * 1-5", or @hourly, @daily, @weekly, @monthly, @yearly (cron).
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty" doc:"cron: when to run, as minute hour day-of-month month day-of-week, e.g. 0 2 * * 1-5, or @hourly, @daily, @weekly, @monthly" schema:"example=0 2 * * 1-5"`
	// Timezone of the schedule, e.g. Europe/Berlin; empty: the time zone of the computer.
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty" doc:"cron: time zone of the schedule, e.g. Europe/Berlin; empty: the time zone of the computer" schema:"example=Europe/Berlin"`
	// Overlap decides what happens while the previous run of the trigger still runs:
	// skip (default) or allow.
	Overlap string `yaml:"overlap,omitempty" json:"overlap,omitempty" doc:"cron, interval, file: what happens while the previous run still runs: skip it or allow another run" schema:"enum=skip|allow,default=skip"`

	// Every is the time between two runs (interval), e.g. 15m.
	Every Duration `yaml:"every,omitempty" json:"every,omitempty" doc:"interval: time between two runs, e.g. 15m" schema:"format=runnel-duration,example=15m"`

	// Path is the folder to watch (file), relative to the project or absolute (also UNC).
	Path string `yaml:"path,omitempty" json:"path,omitempty" doc:"file: folder to watch, relative to the project folder or absolute (also UNC); http: path of the address with placeholders, e.g. /api/customers/{id}" schema:"example=inbox"`
	// Pattern selects files by name, e.g. "*.csv" (file; default "*"); case is ignored.
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty" doc:"file: names of the files, e.g. *.csv; upper and lower case are ignored" schema:"default=*"`
	// Interval is the time between two looks into the folder (file; default 10s).
	Interval Duration `yaml:"interval,omitempty" json:"interval,omitempty" doc:"file: time between two looks into the folder" schema:"format=runnel-duration,default=10s"`
	// Param is the parameter of the target that receives the path of the file (each) or
	// the paths of the files as a JSON list (batch).
	Param string `yaml:"param,omitempty" json:"param,omitempty" doc:"file: parameter of the target that gets the path of the file (each) or the paths as a JSON list (batch)" schema:"example=FILE"`
	// Mode is each (one run per file, default) or batch (one run for all new files).
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" doc:"file: one run per new file (each) or one run for all new files of a look (batch)" schema:"enum=each|batch,default=each"`
	// Existing is process (default) or ignore: files already there when the server starts.
	Existing string `yaml:"existing,omitempty" json:"existing,omitempty" doc:"file: what happens to files already in the folder when the server starts: process them or ignore them" schema:"enum=process|ignore,default=process"`

	// Method is the HTTP method (http): GET, POST, PUT, PATCH or DELETE. Path (above) is
	// the URL path with placeholders, e.g. /api/customers/{id}.
	Method string `yaml:"method,omitempty" json:"method,omitempty" doc:"http: HTTP method of the calls (required for http)" schema:"enum=GET|POST|PUT|PATCH|DELETE"`
	// Auth is none or { token: ${env:NAME} } (http).
	Auth any `yaml:"auth,omitempty" json:"auth,omitempty" doc:"http: none, or a token the caller sends as Authorization: Bearer, e.g. { token: ${env:IMPORT_TOKEN} }"`
	// Response is accepted (default: 202 with the run id at once) or run (the caller
	// waits for the answer of the run, at most timeout).
	Response string `yaml:"response,omitempty" json:"response,omitempty" doc:"http: answer at once with 202 and the run id (accepted), or wait for the answer of the run (run)" schema:"enum=accepted|run,default=accepted"`
	// MaxConcurrent limits the running runs of the trigger (http; 0: no own limit).
	MaxConcurrent int `yaml:"maxConcurrent,omitempty" json:"maxConcurrent,omitempty" doc:"http: most runs of this trigger at the same time; 0: no own limit" schema:"default=0"`
	// Timeout is how long the caller waits with response: run (default 30s). History
	// is all, errors or none: which runs are kept when they have ended.
	Timeout Duration `yaml:"timeout,omitempty" json:"timeout,omitempty" doc:"http with response: run: how long the caller waits for the answer" schema:"format=runnel-duration,default=30s"`
	History string   `yaml:"history,omitempty" json:"history,omitempty" doc:"http: which ended runs are kept in the list of runs: all, errors or none; empty: all, with response: run only errors" schema:"enum=all|errors|none"`
}

// EveryValue returns the time between two runs of an interval trigger.
func (t *Trigger) EveryValue() time.Duration {
	d, _ := t.With.Every.Value()
	return d
}

// PollInterval returns the time between two looks into the folder of a file trigger.
func (t *Trigger) PollInterval() time.Duration {
	d, _ := t.With.Interval.Value()
	if d == 0 {
		return DefaultFileInterval
	}
	return d
}

// FileMode returns each or batch.
func (t *Trigger) FileMode() string {
	if t.With.Mode == "" {
		return FileEach
	}
	return t.With.Mode
}

// FilePattern returns the pattern of a file trigger, * if not given.
func (t *Trigger) FilePattern() string {
	if t.With.Pattern == "" {
		return "*"
	}
	return t.With.Pattern
}

// Folder returns the folder of a file trigger: absolute, or relative to projectDir.
func (t *Trigger) Folder(projectDir string) string {
	p := filepath.FromSlash(t.With.Path)
	if filepath.IsAbs(p) || strings.HasPrefix(t.With.Path, `\\`) {
		return p
	}
	return filepath.Join(projectDir, p)
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

// Next returns the first time of the schedule after the given one: the next time of
// the cron schedule in the time zone of the trigger, or one interval later (interval).
// It is zero for file triggers (they start runs when files arrive) and for triggers
// without a valid schedule.
func (t *Trigger) Next(after time.Time) time.Time {
	switch {
	case t.Type == TriggerInterval && t.EveryValue() > 0:
		return after.Add(t.EveryValue())
	case t.schedule == nil:
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
// with the project (triggers.CheckTrigger).
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
	case TriggerCron, TriggerFile, TriggerInterval, TriggerHTTP:
	case "":
		errs = append(errs, s.errorf("$", `missing required field "type" (expected cron, interval, file or http)`))
	default:
		errs = append(errs, s.errorf("$.type", "unknown trigger type %q (expected cron, interval, file or http)", t.Type))
	}
	switch {
	case t.Target == "":
		errs = append(errs, s.errorf("$", `missing required field "target" (a workflow or pipeline file, relative to the project)`))
	case !strings.HasSuffix(t.Target, ".workflow.yaml") && !strings.HasSuffix(t.Target, ".pipeline.yaml"):
		errs = append(errs, s.errorf("$.target", "target %q must be a workflow (*.workflow.yaml) or pipeline (*.pipeline.yaml) file", t.Target))
	case filepath.IsAbs(t.Target) || slices.Contains(strings.Split(filepath.ToSlash(t.Target), "/"), ".."):
		errs = append(errs, s.errorf("$.target", "target %q must be a path inside the project, relative to its directory", t.Target))
	}
	for _, key := range slices.Sorted(maps.Keys(t.Params)) {
		if v, ok := t.Params[key].(string); ok && expr.Contains(v) {
			errs = append(errs, s.errorf(keyPath("$.params", key), "parameter %s: expressions {{ … }} are not supported in triggers; use a fixed value or ${…}", key))
		}
	}
	switch t.Type {
	case TriggerCron:
		errs = append(errs, t.checkCron()...)
	case TriggerInterval:
		errs = append(errs, t.checkInterval()...)
	case TriggerFile:
		errs = append(errs, t.checkFile()...)
	case TriggerHTTP:
		errs = append(errs, t.checkHTTP()...)
	}
	if t.Type == TriggerCron || t.Type == TriggerInterval || t.Type == TriggerFile {
		errs = append(errs, t.checkOverlap()...)
	}
	if fieldsOf[t.Type] != nil {
		errs = append(errs, t.unusedFields()...)
	}
	return errs.errOrNil()
}

// fieldsOf are the fields under with: each type uses.
var fieldsOf = map[string][]string{
	TriggerCron:     {"schedule", "timezone", "overlap"},
	TriggerInterval: {"every", "overlap"},
	TriggerFile:     {"path", "pattern", "interval", "param", "mode", "existing", "overlap"},
	TriggerHTTP:     {"method", "path", "auth", "response", "timeout", "maxConcurrent", "history"},
}

// unusedFields reports fields under with: that the type does not use, e.g. schedule in
// an interval trigger, so that a mistake does not go unnoticed.
func (t *Trigger) unusedFields() Errors {
	set := map[string]bool{
		"schedule": t.With.Schedule != "", "timezone": t.With.Timezone != "", "overlap": t.With.Overlap != "",
		"every": t.With.Every != "", "path": t.With.Path != "", "pattern": t.With.Pattern != "",
		"interval": t.With.Interval != "", "param": t.With.Param != "", "mode": t.With.Mode != "", "existing": t.With.Existing != "",
		"method": t.With.Method != "", "auth": t.With.Auth != nil, "response": t.With.Response != "", "maxConcurrent": t.With.MaxConcurrent != 0,
		"timeout": t.With.Timeout != "", "history": t.With.History != "",
	}
	var errs Errors
	for _, f := range slices.Sorted(maps.Keys(set)) {
		if set[f] && !slices.Contains(fieldsOf[t.Type], f) {
			errs = append(errs, t.src.errorf("$.with."+f, "%s is not used by %s triggers (fields: %s)", f, t.Type, strings.Join(fieldsOf[t.Type], ", ")))
		}
	}
	return errs
}

func (t *Trigger) checkOverlap() Errors {
	switch t.With.Overlap {
	case "", OverlapSkip, OverlapAllow:
		return nil
	case OverlapQueue:
		return Errors{t.src.errorf("$.with.overlap", "overlap: queue is not supported yet (the run queue is in the backlog); use skip or allow")}
	}
	return Errors{t.src.errorf("$.with.overlap", "invalid overlap %q (expected skip or allow)", t.With.Overlap)}
}

// checkDuration checks a duration of with: that must be at least MinTriggerInterval.
func (t *Trigger) checkDuration(field string, d Duration, required bool) Errors {
	if d == "" {
		if required {
			return Errors{t.src.errorf("$.with", "missing required field %q (e.g. 15m)", field)}
		}
		return nil
	}
	v, err := d.Value()
	if err != nil {
		return Errors{t.src.errorf("$.with."+field, "%s: %v", field, err)}
	}
	if v < MinTriggerInterval {
		return Errors{t.src.errorf("$.with."+field, "%s must be at least %s", field, MinTriggerInterval)}
	}
	return nil
}

func (t *Trigger) checkInterval() Errors {
	return t.checkDuration("every", t.With.Every, true)
}

func (t *Trigger) checkFile() Errors {
	s, w := t.src, t.With
	var errs Errors
	if strings.TrimSpace(w.Path) == "" {
		errs = append(errs, s.errorf("$.with", `missing required field "path" (the folder to watch, relative to the project or absolute)`))
	}
	if _, err := filepath.Match(strings.ToLower(t.FilePattern()), ""); err != nil {
		errs = append(errs, s.errorf("$.with.pattern", "invalid pattern %q (e.g. *.csv or orders-*.txt)", w.Pattern))
	} else if strings.ContainsAny(w.Pattern, `/\`) {
		errs = append(errs, s.errorf("$.with.pattern", "pattern %q matches file names only; put the folder into path", w.Pattern))
	}
	errs = append(errs, t.checkDuration("interval", w.Interval, false)...)
	if w.Param == "" {
		errs = append(errs, s.errorf("$.with", `missing required field "param" (the parameter of the target that gets the path of the file)`))
	}
	switch w.Mode {
	case "", FileEach, FileBatch:
	default:
		errs = append(errs, s.errorf("$.with.mode", "invalid mode %q (expected each or batch)", w.Mode))
	}
	switch w.Existing {
	case "", ExistingProcess, ExistingIgnore:
	default:
		errs = append(errs, s.errorf("$.with.existing", "invalid existing %q (expected process or ignore)", w.Existing))
	}
	return errs
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

// LoadTriggers reads the trigger files of a project (triggers/**/*.yaml, also in
// subfolders), sorted by path. Files with problems are left out; their problems are
// returned as Errors, as are trigger names used more than once.
func LoadTriggers(projectDir string) ([]*Trigger, error) {
	files, err := TriggerFiles(projectDir)
	if err != nil {
		return nil, err
	}
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
			errs = append(errs, t.ErrorAt("$.name", "trigger name %q is already used by %s", t.Name, TriggerFileName(projectDir, first)))
			continue
		}
		names[t.Name] = file
		out = append(out, t)
	}
	out, conflicts := checkRoutes(out)
	errs = append(errs, conflicts...)
	return out, errs.errOrNil()
}

// TriggerFiles returns the trigger files of a project: the *.yaml files in triggers/ and
// its subfolders, sorted by path. Hidden files and folders (".…") are left out; a
// project without triggers/ has none.
func TriggerFiles(projectDir string) ([]string, error) {
	root := filepath.Join(projectDir, TriggersDir)
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			if p == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return err
		case p != root && strings.HasPrefix(d.Name(), "."):
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		case !d.IsDir() && strings.HasSuffix(d.Name(), ".yaml"):
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

// TriggerFileName is the path of a trigger file relative to triggers/ for messages,
// e.g. "nightly/import.yaml".
func TriggerFileName(projectDir, file string) string {
	if rel, err := filepath.Rel(filepath.Join(projectDir, TriggersDir), file); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(file)
}

// TriggerProjectDir returns the project folder of a trigger file: the folder above the
// triggers/ folder the file is in (also in a subfolder of it) that has a project.yaml.
// ok is false for other files.
func TriggerProjectDir(file string) (dir string, ok bool) {
	if !strings.HasSuffix(file, ".yaml") {
		return "", false
	}
	for d := filepath.Dir(file); ; {
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		if filepath.Base(d) == TriggersDir {
			if _, err := os.Stat(filepath.Join(parent, ProjectFile)); err == nil {
				return parent, true
			}
		}
		d = parent
	}
}

// IsTriggerFile reports whether a path relative to the project is a trigger file
// (triggers/**/*.yaml, not hidden).
func IsTriggerFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, TriggersDir+"/") || !strings.HasSuffix(rel, ".yaml") {
		return false
	}
	for part := range strings.SplitSeq(rel[len(TriggersDir)+1:], "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// Describe tells when a trigger starts runs, e.g. "0 2 * * 1-5 (Europe/Berlin)",
// "every 15m" or "new *.csv in inbox".
func (t *Trigger) Describe() string {
	switch t.Type {
	case TriggerCron:
		return fmt.Sprintf("%s (%s)", t.With.Schedule, t.location)
	case TriggerInterval:
		return "every " + string(t.With.Every)
	case TriggerFile:
		return fmt.Sprintf("new %s in %s", t.FilePattern(), t.With.Path)
	case TriggerHTTP:
		return t.HTTPMethod() + " " + t.With.Path
	}
	return t.Type
}
