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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	exprlang "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/types"
	"github.com/expr-lang/expr/vm"
	"github.com/shopspring/decimal"
)

// MaxLength is the longest expression accepted (between the braces).
const MaxLength = 4096

// maxNodes limits the size of a compiled expression.
const maxNodes = 2000

// WorkflowRoots are the names a workflow expression can refer to (ADR 0006).
var WorkflowRoots = []string{"vars", "params", "env", "run", "result", "results", "request"}

// RowRoots are the names an expression in the configuration of a workflow action with
// forEach can refer to: the workflow names and row, the current entry (S24b).
var RowRoots = append(append([]string(nil), WorkflowRoots...), "row")

// RecordRoots are the names an expression evaluated per record of a pipeline can refer
// to, e.g. the condition of core.filter (ADR 0006).
var RecordRoots = []string{"record", "vars", "params", "env", "run"}

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

// programs caches compiled expressions by code and roots (S38b2): the same workflow
// runs again and again, e.g. for each request to an http trigger. Programs can be run
// by several goroutines at once. The cache is cleared when it is full.
var programs = struct {
	sync.Mutex
	m map[string]*vm.Program
}{m: map[string]*vm.Program{}}

// maxPrograms is the number of compiled expressions kept.
const maxPrograms = 4096

func compile(code string, roots []string) (*vm.Program, error) {
	key := code + "\x00" + strings.Join(roots, ",")
	programs.Lock()
	prog := programs.m[key]
	programs.Unlock()
	if prog != nil {
		return prog, nil
	}
	prog, err := compileProgram(code, roots)
	if err != nil {
		return nil, err
	}
	programs.Lock()
	if len(programs.m) >= maxPrograms {
		clear(programs.m)
	}
	programs.m[key] = prog
	programs.Unlock()
	return prog, nil
}

func compileProgram(code string, roots []string) (*vm.Program, error) {
	// The roots are maps with any keys; row (forEach) can be any value: a number, a
	// text or an object.
	env := make(types.Map, len(roots))
	for _, r := range roots {
		env[r] = types.TypeOf(map[string]any{})
	}
	if _, ok := env["row"]; ok {
		env["row"] = types.Any
	}
	opts := append([]exprlang.Option{exprlang.Env(env), exprlang.MaxNodes(maxNodes)}, functions...)
	opts = append(opts, operators...)
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
	return t.eval(env, nil)
}

// eval evaluates the template with the virtual machine m; nil uses a new one.
func (t *Template) eval(env map[string]any, m *vm.VM) (any, error) {
	if t.whole {
		return run(t.parts[0], env, m)
	}
	var b strings.Builder
	for _, p := range t.parts {
		if p.prog == nil {
			b.WriteString(p.text)
			continue
		}
		v, err := run(p, env, m)
		if err != nil {
			return nil, err
		}
		b.WriteString(Format(v))
	}
	return b.String(), nil
}

// EvalBool evaluates a condition (e.g. of a link); the result must be true or false.
func (t *Template) EvalBool(env map[string]any) (bool, error) {
	return t.evalBool(env, nil)
}

func (t *Template) evalBool(env map[string]any, m *vm.VM) (bool, error) {
	v, err := t.eval(env, m)
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

// Evaluator evaluates a template again and again with one virtual machine, e.g. once
// per record, which saves allocations per evaluation. It is not safe for concurrent use.
type Evaluator struct {
	t  *Template
	vm vm.VM
}

// Evaluator returns an evaluator of the template.
func (t *Template) Evaluator() *Evaluator { return &Evaluator{t: t} }

// Eval is Template.Eval with the evaluator's virtual machine.
func (e *Evaluator) Eval(env map[string]any) (any, error) { return e.t.eval(env, &e.vm) }

// EvalBool is Template.EvalBool with the evaluator's virtual machine.
func (e *Evaluator) EvalBool(env map[string]any) (bool, error) { return e.t.evalBool(env, &e.vm) }

func run(p part, env map[string]any, m *vm.VM) (any, error) {
	if m == nil {
		m = &vm.VM{}
	}
	v, err := m.Run(p.prog, env)
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
	case decimal.Decimal:
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

// Members returns the names used after root in the expressions of t, in order of
// appearance and without duplicates: "record" for {{ record.amount > record.limit }}
// gives amount and limit. Only fixed names count (record.amount, record["amount"]).
func (t *Template) Members(root string) []string {
	var names []string
	for _, p := range t.parts {
		if p.prog == nil {
			continue
		}
		tree, err := parser.Parse(p.code)
		if err != nil {
			continue
		}
		ast.Walk(&tree.Node, memberVisitor{root: root, names: &names})
	}
	return names
}

// OnlyMembers is like Members, but also reports whether the expressions use root only
// through fixed names: false for record[name], len(record) or "x" in record, which
// may need any field.
func (t *Template) OnlyMembers(root string) ([]string, bool) {
	var names []string
	only := true
	for _, p := range t.parts {
		if p.prog == nil {
			continue
		}
		tree, err := parser.Parse(p.code)
		if err != nil {
			return nil, false
		}
		v := memberVisitor{root: root, names: &names, fixed: new(int), uses: new(int)}
		ast.Walk(&tree.Node, v)
		only = only && *v.fixed == *v.uses
	}
	return names, only
}

type memberVisitor struct {
	root  string
	names *[]string
	// fixed counts the uses of root with a fixed name (root.name), uses all of them.
	fixed, uses *int
}

func (v memberVisitor) Visit(node *ast.Node) {
	if id, ok := (*node).(*ast.IdentifierNode); ok && id.Value == v.root && v.uses != nil {
		*v.uses++
	}
	m, ok := (*node).(*ast.MemberNode)
	if !ok {
		return
	}
	id, ok := m.Node.(*ast.IdentifierNode)
	if !ok || id.Value != v.root {
		return
	}
	if s, ok := m.Property.(*ast.StringNode); ok {
		if v.fixed != nil {
			*v.fixed++
		}
		if !slices.Contains(*v.names, s.Value) {
			*v.names = append(*v.names, s.Value)
		}
	}
}
