package model

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/vars"
)

// File format constants.
const (
	APIVersion      = "runnel/v1"
	KindWorkflow    = "Workflow"
	KindProject     = "Project"
	KindEnvironment = "Environment"
)

// checkHeader validates the fields every file starts with.
func checkHeader(s *source, kind, wantKind, apiVersion, name string) Errors {
	var errs Errors
	switch {
	case kind == "":
		errs = append(errs, s.errorf("$.kind", `missing required field "kind" (expected %q)`, wantKind))
	case kind != wantKind:
		errs = append(errs, s.errorf("$.kind", "kind must be %q, got %q", wantKind, kind))
	}
	switch {
	case apiVersion == "":
		errs = append(errs, s.errorf("$.apiVersion", `missing required field "apiVersion" (expected %q)`, APIVersion))
	case apiVersion != APIVersion:
		errs = append(errs, s.errorf("$.apiVersion", "unsupported apiVersion %q (supported: %s)", apiVersion, APIVersion))
	}
	if strings.TrimSpace(name) == "" {
		errs = append(errs, s.errorf("$.name", `missing required field "name"`))
	}
	return errs
}

// Workflow is the content of a *.workflow.yaml file: actions connected by links.
type Workflow struct {
	Kind        string           `yaml:"kind" json:"kind"`
	APIVersion  string           `yaml:"apiVersion" json:"apiVersion"`
	Name        string           `yaml:"name" json:"name"`
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	Parameters  []Parameter      `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Outputs     []Output         `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Settings    WorkflowSettings `yaml:"settings,omitempty" json:"settings,omitzero"`
	Actions     []Action         `yaml:"actions,omitempty" json:"actions,omitempty"`
	Links       []Link           `yaml:"links,omitempty" json:"links,omitempty"`
	// Layout holds editor coordinates per action ID; it has no effect on execution.
	Layout map[string]Point `yaml:"layout,omitempty" json:"layout,omitempty"`

	src *source
}

// Parameter is an input of a workflow, set via CLI, trigger or calling action.
type Parameter struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Type is string (default), integer, number, boolean or json.
	Type     ValueType `yaml:"type,omitempty" json:"type,omitempty"`
	Default  string    `yaml:"default,omitempty" json:"default,omitempty"`
	Required bool      `yaml:"required,omitempty" json:"required,omitempty"`
	// Enum lists the allowed values, if restricted.
	Enum []string `yaml:"enum,omitempty" json:"enum,omitempty"`
}

// Output is a result value of a workflow, evaluated when it ends successfully.
type Output struct {
	Name        string    `yaml:"name" json:"name"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Type        ValueType `yaml:"type,omitempty" json:"type,omitempty"`
	// Value is the text of the output, usually with variables, e.g. "${STATUS}".
	Value string `yaml:"value" json:"value"`
}

// WorkflowSettings control the execution of a workflow as a whole.
type WorkflowSettings struct {
	Timeout           Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	MaxConcurrentRuns int      `yaml:"maxConcurrentRuns,omitempty" json:"maxConcurrentRuns,omitempty"`
	// OnError is the workflow started when a run fails (ADR 0027), relative to the
	// project; none turns off that of the project; empty: that of the project.
	OnError string `yaml:"onError,omitempty" json:"onError,omitempty" doc:"Workflow started when a run of this workflow fails, relative to the project; none: no error workflow; empty: the one of project.yaml"`
}

// Action is a step of a workflow.
type Action struct {
	ID          string `yaml:"id" json:"id"`
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// With is the type-specific configuration of the action.
	With            map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
	Timeout         Duration       `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry           *Retry         `yaml:"retry,omitempty" json:"retry,omitempty"`
	ContinueOnError bool           `yaml:"continueOnError,omitempty" json:"continueOnError,omitempty"`
	// Join defines when an action with several incoming links starts (see ADR 0003).
	// Empty means the project default.
	Join        JoinMode `yaml:"join,omitempty" json:"join,omitempty"`
	JoinTimeout Duration `yaml:"joinTimeout,omitempty" json:"joinTimeout,omitempty"`
	// ForEach repeats the action for every entry of a list (an expression, e.g.
	// "{{ results.list.output }}"); the entry is "row" in its expressions (S24b).
	ForEach string `yaml:"forEach,omitempty" json:"forEach,omitempty"`
	// Parallel is the number of entries of forEach run at the same time; 0: 1.
	Parallel int `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	// StopOnError stops forEach at the first failed entry; nil: true.
	StopOnError *bool `yaml:"stopOnError,omitempty" json:"stopOnError,omitempty"`
}

// MaxParallel limits the entries of forEach that run at the same time.
const MaxParallel = 64

// Retry configures repeated attempts of a failed action.
type Retry struct {
	// Attempts is the total number of attempts, including the first one.
	Attempts int `yaml:"attempts" json:"attempts"`
	// Backoff is the wait before the second attempt.
	Backoff Duration `yaml:"backoff,omitempty" json:"backoff,omitempty"`
	// Strategy is "fixed" (default: always Backoff) or "exponential" (Backoff doubles after each attempt).
	Strategy RetryStrategy `yaml:"strategy,omitempty" json:"strategy,omitempty"`
}

// RetryStrategy defines how the wait between attempts develops.
type RetryStrategy string

// Retry strategies.
const (
	RetryFixed       RetryStrategy = "fixed"
	RetryExponential RetryStrategy = "exponential"
)

// Wait returns the wait before the given attempt (2 = first retry).
func (r Retry) Wait(attempt int) time.Duration {
	d, _ := r.Backoff.Value()
	if r.Strategy == RetryExponential {
		for i := 2; i < attempt && d < 24*time.Hour; i++ {
			d *= 2
		}
	}
	return d
}

// JoinMode defines when an action with several incoming links starts (see ADR 0003).
type JoinMode string

// Join modes.
const (
	JoinAll   JoinMode = "all"
	JoinFirst JoinMode = "first"
	JoinEach  JoinMode = "each"
)

// Link connects two actions. It is followed when its condition holds.
type Link struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	// When is "success" (default), "failure", "always" or an expression "{{ … }}".
	When string `yaml:"when,omitempty" json:"when,omitempty"`
	// Loop marks a link back to an earlier action (see ADR 0003).
	Loop     bool `yaml:"loop,omitempty" json:"loop,omitempty"`
	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

// ConditionKind classifies the condition of a link.
type ConditionKind string

// Condition kinds.
const (
	OnSuccess    ConditionKind = "success"
	OnFailure    ConditionKind = "failure"
	Always       ConditionKind = "always"
	OnExpression ConditionKind = "expression"
)

// Condition returns the kind of the link's condition and, for expressions,
// the expression text without the surrounding "{{ }}".
func (l Link) Condition() (kind ConditionKind, expr string) {
	when := strings.TrimSpace(l.When)
	switch when {
	case "", string(OnSuccess):
		return OnSuccess, ""
	case string(OnFailure):
		return OnFailure, ""
	case string(Always):
		return Always, ""
	}
	if inner, ok := strings.CutPrefix(when, "{{"); ok {
		if inner, ok := strings.CutSuffix(inner, "}}"); ok && strings.TrimSpace(inner) != "" {
			return OnExpression, strings.TrimSpace(inner)
		}
	}
	return "", ""
}

// Point is a position on the editor canvas.
type Point struct {
	X float64 `yaml:"x" json:"x"`
	Y float64 `yaml:"y" json:"y"`
}

// LoadWorkflow reads and parses a workflow file.
func LoadWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseWorkflow(filepath.ToSlash(path), data)
}

// ParseWorkflow parses a workflow from YAML. name is used in error messages.
// The returned error is of type Errors and lists all problems found.
func ParseWorkflow(name string, data []byte) (*Workflow, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	w := &Workflow{src: src}
	if err := src.decode(w); err != nil {
		return nil, err
	}
	if err := w.validate(); err != nil {
		return nil, err
	}
	return w, nil
}

// ReadWorkflow parses a workflow like ParseWorkflow, but also returns it when only
// fields are invalid (e.g. an unknown join mode or an invalid duration): the editor can
// then still show the workflow and the errors at their fields. The workflow is nil if
// the file cannot be read at all (syntax error, wrong types, unknown fields); the
// returned error lists the problems in both cases.
func ReadWorkflow(name string, data []byte) (*Workflow, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	w := &Workflow{src: src}
	if err := src.decode(w); err != nil {
		return nil, err
	}
	return w, w.validate()
}

var (
	idPattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	actionTypePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
)

// validate checks each field on its own. References between elements
// (e.g. links to existing actions) are checked separately.
func (w *Workflow) validate() error {
	s := w.src
	var errs Errors
	add := func(path, format string, args ...any) {
		errs = append(errs, s.errorf(path, format, args...))
	}
	// Expressions {{ … }} are compiled when the file is read, so that mistakes show up
	// before a run (ADR 0006); they are evaluated by the engine.
	checkExprIn := func(path, label, text string, roots []string) {
		if !expr.Contains(text) {
			return
		}
		if err := expr.Check(text, roots); err != nil {
			add(path, "%s: %v", label, err)
		}
	}
	checkExpr := func(path, label, text string) { checkExprIn(path, label, text, expr.WorkflowRoots) }
	// The configuration of an action with forEach may use row, the current entry.
	var checkWith func(path, label string, v any, roots []string)
	checkWith = func(path, label string, v any, roots []string) {
		switch v := v.(type) {
		case string:
			checkExprIn(path, label, v, roots)
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(v)) {
				checkWith(path+"."+k, label, v[k], roots)
			}
		case []any:
			for i, e := range v {
				checkWith(fmt.Sprintf("%s[%d]", path, i), label, e, roots)
			}
		}
	}
	duration := func(path, label string, d Duration) {
		if _, err := d.Value(); err != nil {
			add(path, "%s: %v", label, err)
		}
	}

	errs = append(errs, checkHeader(s, w.Kind, KindWorkflow, w.APIVersion, w.Name)...)

	errs = append(errs, checkParameters(s, w.Parameters, expr.WorkflowRoots)...)

	outputs := map[string]bool{}
	for i, o := range w.Outputs {
		path := fmt.Sprintf("$.outputs[%d]", i)
		switch {
		case o.Name == "":
			add(path, `output: missing required field "name"`)
		case !vars.ValidName(o.Name):
			add(path+".name", "invalid output name %q (letters, digits and _, not starting with a digit)", o.Name)
		case outputs[o.Name]:
			add(path+".name", "duplicate output %q", o.Name)
		}
		outputs[o.Name] = true
		if !o.Type.Valid() {
			add(path+".type", "output %q: invalid type %q (expected %s)", o.Name, o.Type, typeNames())
		}
		switch {
		case o.Value == "":
			add(path, `output %q: missing required field "value"`, o.Name)
		case o.Type.Valid() && !hasReferences(o.Value):
			if _, err := o.Type.Parse(o.Value); err != nil {
				add(path+".value", "output %q: value %v", o.Name, err)
			}
		default:
			checkExpr(path+".value", fmt.Sprintf("output %q", o.Name), o.Value)
		}
	}

	duration("$.settings.timeout", "settings.timeout", w.Settings.Timeout)
	if w.Settings.MaxConcurrentRuns < 0 {
		add("$.settings.maxConcurrentRuns", "maxConcurrentRuns must not be negative")
	}
	if msg := CheckOnErrorPath(w.Settings.OnError); msg != "" {
		add("$.settings.onError", "%s", msg)
	}

	for i, a := range w.Actions {
		path := fmt.Sprintf("$.actions[%d]", i)
		label := fmt.Sprintf("action #%d", i+1)
		switch {
		case a.ID == "":
			add(path, `%s: missing required field "id"`, label)
		case !idPattern.MatchString(a.ID):
			add(path+".id", "invalid action id %q (letters, digits, - and _, starting with a letter)", a.ID)
		default:
			label = fmt.Sprintf("action %q", a.ID)
		}
		switch {
		case a.Type == "":
			add(path, `%s: missing required field "type"`, label)
		case !actionTypePattern.MatchString(a.Type):
			add(path+".type", `%s: invalid type %q (expected "<namespace>.<name>", e.g. "file.copy")`, label, a.Type)
		}
		duration(path+".timeout", label+" timeout", a.Timeout)
		duration(path+".joinTimeout", label+" joinTimeout", a.JoinTimeout)
		roots := expr.WorkflowRoots
		if a.ForEach != "" {
			roots = expr.RowRoots
			if !expr.Contains(a.ForEach) {
				add(path+".forEach", "%s: forEach must be an expression that returns a list, e.g. \"{{ results.list.output }}\"", label)
			} else {
				checkExpr(path+".forEach", label+" forEach", a.ForEach)
			}
		} else {
			if a.Parallel != 0 {
				add(path+".parallel", "%s: parallel is only possible with forEach", label)
			}
			if a.StopOnError != nil {
				add(path+".stopOnError", "%s: stopOnError is only possible with forEach", label)
			}
		}
		if a.Parallel < 0 || a.Parallel > MaxParallel {
			add(path+".parallel", "%s: parallel must be between 1 and %d", label, MaxParallel)
		}
		checkWith(path+".with", label, a.With, roots)
		switch a.Join {
		case "", JoinAll, JoinFirst, JoinEach:
		default:
			add(path+".join", "%s: invalid join %q (expected all, first or each)", label, a.Join)
		}
		if a.Retry != nil {
			if a.Retry.Attempts < 1 {
				add(path+".retry.attempts", "%s: retry attempts must be at least 1", label)
			}
			duration(path+".retry.backoff", label+" retry backoff", a.Retry.Backoff)
			switch a.Retry.Strategy {
			case "", RetryFixed, RetryExponential:
			default:
				add(path+".retry.strategy", "%s: invalid retry strategy %q (expected fixed or exponential)", label, a.Retry.Strategy)
			}
		}
	}

	for i, l := range w.Links {
		path := fmt.Sprintf("$.links[%d]", i)
		label := fmt.Sprintf("link #%d", i+1)
		if l.From == "" {
			add(path, `%s: missing required field "from"`, label)
		}
		if l.To == "" {
			add(path, `%s: missing required field "to"`, label)
		}
		switch kind, _ := l.Condition(); kind {
		case "":
			add(path+".when", `%s: invalid condition %q (expected success, failure, always or an expression "{{ … }}")`, label, l.When)
		case OnExpression:
			checkExpr(path+".when", label, l.When)
		}
	}

	return errs.errOrNil()
}

// WarningAt creates a warning located at a YAML path of the workflow file.
func (w *Workflow) WarningAt(path, format string, args ...any) *Error {
	if w.src == nil {
		return &Error{Severity: SeverityWarning, Msg: fmt.Sprintf(format, args...)}
	}
	return w.src.warnf(path, format, args...)
}

// ErrorAt creates an error located at a YAML path of the workflow file, e.g. "$.actions[2].type".
// It is meant for checks outside this package, e.g. by the engine before a run.
func (w *Workflow) ErrorAt(path, format string, args ...any) *Error {
	if w.src == nil {
		return &Error{Msg: fmt.Sprintf(format, args...)}
	}
	return w.src.errorf(path, format, args...)
}
