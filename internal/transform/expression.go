package transform

import (
	"errors"
	"reflect"
	"regexp"
	"time"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
)

// Expression is an expression evaluated per record (ADR 0006), e.g. the condition of
// core.filter. It may use record and the values of the run (vars, params, env, run).
//
// An Expression keeps its context between evaluations to save allocations per record,
// so it is not safe for concurrent use: each transform instance (and each copy) has its
// own, created by New.
type Expression struct {
	tpl  *expr.Template
	eval *expr.Evaluator
	// fields are the fields of the record the expression reads; nil: any (all are passed).
	fields []string
	// env and rec are the context of the last evaluation, reused for the next record;
	// values are the values of the run env was built from.
	env    map[string]any
	rec    map[string]any
	values map[string]any
}

var varRef = regexp.MustCompile(`\$\{[^}]*\}`)

// CompileExpression compiles an expression of the configuration, with or without {{ }}
// around it. A text that still contains ${…} (runnel validate checks the configuration
// before variables are resolved) is only checked; it cannot be evaluated.
func CompileExpression(text string) (*Expression, error) {
	if !expr.Contains(text) {
		text = "{{ " + text + " }}"
	}
	if varRef.MatchString(text) {
		return &Expression{}, expr.Check(text, expr.RecordRoots)
	}
	tpl, err := expr.Compile(text, expr.RecordRoots)
	if err != nil {
		return nil, err
	}
	e := &Expression{tpl: tpl, eval: tpl.Evaluator()}
	// Only the fields the expression names are passed, unless it may read any field
	// (record[name], len(record) …); a missing field is nil either way.
	if names, only := tpl.OnlyMembers("record"); only {
		e.fields = append([]string{}, names...)
	}
	return e, nil
}

// Fields returns the fields of the record the expression uses, e.g. amount for
// {{ record.amount > 0 }}, for InitContext.Use.
func (e *Expression) Fields() []string {
	if e.tpl == nil {
		return nil
	}
	return e.tpl.Members("record")
}

// Bool evaluates a condition for a record; the result must be true or false.
func (e *Expression) Bool(out *Output, r record.Record) (bool, error) {
	if e.tpl == nil {
		return false, errUnresolved
	}
	return e.eval.EvalBool(e.context(out, r))
}

// Value evaluates the expression for a record and returns its result (e.g. a field of
// core.calculate).
func (e *Expression) Value(out *Output, r record.Record) (any, error) {
	if e.tpl == nil {
		return nil, errUnresolved
	}
	return e.eval.Eval(e.context(out, r))
}

var errUnresolved = errors.New("the expression contains a variable ${…} that was not resolved")

// context returns the context for one record: the values of the run, built once, and
// record with the fields the expression reads, refilled for every record.
func (e *Expression) context(out *Output, r record.Record) map[string]any {
	values := out.Values()
	if e.env == nil || !sameMap(e.values, values) {
		e.values = values
		e.env = make(map[string]any, len(values)+1)
		for k, v := range values {
			e.env[k] = v
		}
		e.rec = map[string]any{}
		e.env["record"] = e.rec
	}
	clear(e.rec)
	if e.fields == nil {
		for _, f := range r.Fields() {
			e.rec[f.Name] = exprValue(f.Value)
		}
		return e.env
	}
	for _, name := range e.fields {
		if v, ok := r.Get(name); ok {
			e.rec[name] = exprValue(v)
		}
	}
	return e.env
}

// sameMap reports whether a and b are the same map (not only equal).
func sameMap(a, b map[string]any) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}

// exprValue returns a value as expressions see it: a date becomes a time at midnight,
// local time, so that it works with today(), formatDate() and < >.
func exprValue(v any) any {
	if d, ok := v.(record.Date); ok {
		return d.In(time.Local)
	}
	return v
}

// RecordValues returns the fields of a record as expressions see them: dates become
// times at midnight, local time, so that they work with today(), formatDate() and < >.
func RecordValues(r record.Record) map[string]any {
	m := make(map[string]any, r.Len())
	for _, f := range r.Fields() {
		m[f.Name] = exprValue(f.Value)
	}
	return m
}
