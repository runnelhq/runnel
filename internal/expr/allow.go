package expr

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	exprlang "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/parser"
)

// Expressions may only call the functions of the reference (Functions): the library of
// Runnel and the built-in functions of expr-lang it documents (R5a, ADR 0006 Nachtrag).
// The other built-ins are refused when the expression is compiled, so that the functions
// of a version are those of "runnel catalog --expressions" – also after an update of
// expr-lang. The list functions with a condition (all, filter, map …) are language
// constructs of expr-lang; the ones it knows are documented as well.

// allowed are the names of the documented functions.
var allowed = func() map[string]bool {
	m := make(map[string]bool, len(Functions))
	for _, d := range Functions {
		m[d.Name] = true
	}
	return m
}()

// builtinOptions disable the built-ins of expr-lang that are not documented.
var builtinOptions = func() []exprlang.Option {
	opts := []exprlang.Option{exprlang.DisableAllBuiltins()}
	for _, name := range builtin.Names {
		if allowed[name] {
			opts = append(opts, exprlang.EnableBuiltin(name))
		}
	}
	return opts
}()

// checkCalls returns an error for the first call of a function that is not documented.
// Syntax errors are left to the compiler, which reports them with more detail.
func checkCalls(code string) error {
	tree, err := parser.Parse(code)
	if err != nil {
		return nil //nolint:nilerr // the compiler reports syntax errors with more detail
	}
	v := &callVisitor{}
	ast.Walk(&tree.Node, v)
	return v.err
}

type callVisitor struct{ err error }

func (v *callVisitor) Visit(node *ast.Node) {
	if v.err != nil {
		return
	}
	var name string
	switch n := (*node).(type) {
	case *ast.BuiltinNode:
		name = n.Name
	case *ast.CallNode:
		if id, ok := n.Callee.(*ast.IdentifierNode); ok {
			name = id.Value
		}
	}
	if name != "" && !allowed[name] {
		v.err = unknownFunction(name)
	}
}

// unknownFunction is the error for a function that is not documented, with similar ones.
func unknownFunction(name string) error {
	if similar := similarFunctions(name); len(similar) > 0 {
		return fmt.Errorf("unknown function %q (similar: %s)", name, strings.Join(similar, ", "))
	}
	return fmt.Errorf("unknown function %q (the functions are listed by \"runnel catalog --expressions\")", name)
}

// similarFunctions returns up to four documented functions whose name contains name (or
// the other way round) or is at most two edits away, the closest first.
func similarFunctions(name string) []string {
	type match struct {
		name string
		dist int
	}
	var matches []match
	lower := strings.ToLower(name)
	for _, d := range Functions {
		other := strings.ToLower(d.Name)
		dist := editDistance(lower, other)
		related := len(lower) >= 3 && (strings.Contains(other, lower) || strings.Contains(lower, other))
		if dist <= 2 || related {
			matches = append(matches, match{d.Name, dist})
		}
	}
	slices.SortStableFunc(matches, func(a, b match) int { return cmp.Compare(a.dist, b.dist) })
	var names []string
	for _, m := range matches {
		if !slices.Contains(names, m.name) && len(names) < 4 {
			names = append(names, m.name)
		}
	}
	return names
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
