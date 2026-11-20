// Package expr evaluates {{ … }} expressions in workflow values (ADR 0006) with
// expr-lang. A value that consists of exactly one expression keeps the type of its
// result (number, date, list …); expressions inside a text are inserted as text.
// Expressions are compiled once, when a file is loaded, so that syntax errors are
// reported by "runnel validate" and in the editor, not only during a run.
package expr

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	exprlang "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// MaxLength is the longest expression accepted (between the braces).
const MaxLength = 4096

// maxNodes limits the size of a compiled expression.
const maxNodes = 2000

// WorkflowRoots are the names a workflow expression can refer to (ADR 0006).
var WorkflowRoots = []string{"vars", "params", "env", "run", "result", "results"}

// Template is a text with {{ … }} expressions, compiled.
type Template struct {
	parts []part
	// whole is set when the value consists of exactly one expression.
	whole bool
}

type part struct {
	text string
	prog *vm.Program
	code string
}

// Error is a problem with an expression; Offset is the byte position of its "{{" in the value.
type Error struct {
	Offset int
	Code   string
	Msg    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("expression {{ %s }}: %s", e.Code, e.Msg)
}

// Contains reports whether s contains the start of an expression.
func Contains(s string) bool {
	return strings.Contains(s, "{{")
}

// Compile parses the expressions of s. roots are the names an expression may use
// (e.g. WorkflowRoots); any other name is an error.
func Compile(s string, roots []string) (*Template, error) {
	t := &Template{}
	rest := s
	offset := 0
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			if rest != "" {
				t.parts = append(t.parts, part{text: rest})
			}
			break
		}
		if i > 0 {
			t.parts = append(t.parts, part{text: rest[:i]})
		}
		end := strings.Index(rest[i+2:], "}}")
		if end < 0 {
			return nil, &Error{Offset: offset + i, Code: strings.TrimSpace(rest[i+2:]), Msg: `missing closing "}}"`}
		}
		code := strings.TrimSpace(rest[i+2 : i+2+end])
		if code == "" {
			return nil, &Error{Offset: offset + i, Msg: "the expression is empty"}
		}
		if len(code) > MaxLength {
			return nil, &Error{Offset: offset + i, Code: code[:40] + "…", Msg: fmt.Sprintf("the expression is longer than %d characters", MaxLength)}
		}
		prog, err := compile(code, roots)
		if err != nil {
			return nil, &Error{Offset: offset + i, Code: code, Msg: cleanError(err)}
		}
		t.parts = append(t.parts, part{prog: prog, code: code})
		next := i + 2 + end + 2
		offset += next
		rest = rest[next:]
	}
	// A value that is one expression (apart from surrounding spaces) keeps the result's type.
	var progs []part
	blank := true
	for _, p := range t.parts {
		if p.prog != nil {
			progs = append(progs, p)
		} else if strings.TrimSpace(p.text) != "" {
			blank = false
		}
	}
	if len(progs) == 1 && blank {
		t.parts, t.whole = progs, true
	}
	return t, nil
}

var varRef = regexp.MustCompile(`\$\{[^}]*\}`)

// Check compiles the expressions of s as they will be after ${…} references have been
// resolved (ADR 0006). A reference outside quotes can only stand for a number or a
// boolean, inside quotes it is text; the check uses 0, which fits both.
func Check(s string, roots []string) error {
	_, err := Compile(varRef.ReplaceAllString(s, "0"), roots)
	return err
}

func compile(code string, roots []string) (*vm.Program, error) {
	env := make(map[string]any, len(roots))
	for _, r := range roots {
		env[r] = map[string]any{}
	}
	opts := append([]exprlang.Option{exprlang.Env(env), exprlang.MaxNodes(maxNodes)}, functions...)
	return exprlang.Compile(code, opts...)
}

// positionSuffix is the position within the expression that expr-lang appends, e.g. " (1:6)".
var positionSuffix = regexp.MustCompile(`\s*\(\d+:\d+\)$`)

// cleanError shortens the messages of expr-lang to their first line, without the
// position inside the expression (the file position is reported separately).
func cleanError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "\n"); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(positionSuffix.ReplaceAllString(msg, ""))
}

// Static reports whether the template has no expressions.
func (t *Template) Static() bool {
	for _, p := range t.parts {
		if p.prog != nil {
			return false
		}
	}
	return true
}

// Eval evaluates the template. A value that consists of one expression returns the
// result with its type; otherwise the parts are joined as text (see Format).
func (t *Template) Eval(env map[string]any) (any, error) {
	if t.whole {
		return run(t.parts[0], env)
	}
	var b strings.Builder
	for _, p := range t.parts {
		if p.prog == nil {
			b.WriteString(p.text)
			continue
		}
		v, err := run(p, env)
		if err != nil {
			return nil, err
		}
		b.WriteString(Format(v))
	}
	return b.String(), nil
}

// EvalBool evaluates a condition (e.g. of a link); the result must be true or false.
func (t *Template) EvalBool(env map[string]any) (bool, error) {
	v, err := t.Eval(env)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expression {{ %s }}: the result must be true or false, got %s", t.code(), Format(v))
	}
	return b, nil
}

func (t *Template) code() string {
	for _, p := range t.parts {
		if p.prog != nil {
			return p.code
		}
	}
	return ""
}

func run(p part, env map[string]any) (any, error) {
	v, err := exprlang.Run(p.prog, env)
	if err != nil {
		return nil, &Error{Code: p.code, Msg: cleanError(err)}
	}
	return v, nil
}

// Format writes a value as text: dates in ISO 8601, whole numbers without decimals,
// lists and objects as JSON, nothing for nil.
func Format(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case time.Time:
		if v.Nanosecond() == 0 {
			return v.Format(time.RFC3339)
		}
		return v.Format(time.RFC3339Nano)
	case time.Duration:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v)
	case error:
		return v.Error()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// IsError reports whether err is an expression error (and returns it).
func IsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
