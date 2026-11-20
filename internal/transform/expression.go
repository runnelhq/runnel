package transform

import (
	"errors"
	"regexp"
	"time"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
)

// Expression is an expression evaluated per record (ADR 0006), e.g. the condition of
// core.filter. It may use record and the values of the run (vars, params, env, run).
type Expression struct {
	tpl *expr.Template
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
	return &Expression{tpl: tpl}, nil
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
	return e.tpl.EvalBool(env(out, r))
}

var errUnresolved = errors.New("the expression contains a variable ${…} that was not resolved")

// env is the context of an expression for one record.
func env(out *Output, r record.Record) map[string]any {
	values := out.Values()
	m := make(map[string]any, len(values)+1)
	for k, v := range values {
		m[k] = v
	}
	m["record"] = RecordValues(r)
	return m
}

// RecordValues returns the fields of a record as expressions see them: dates become
// times at midnight, local time, so that they work with today(), formatDate() and < >.
func RecordValues(r record.Record) map[string]any {
	m := make(map[string]any, r.Len())
	for _, f := range r.Fields() {
		v := f.Value
		if d, ok := v.(record.Date); ok {
			v = d.In(time.Local)
		}
		m[f.Name] = v
	}
	return m
}
