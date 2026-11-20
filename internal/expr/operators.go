package expr

import (
	"errors"
	"fmt"
	"reflect"

	exprlang "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm/runtime"
	"github.com/shopspring/decimal"
)

// Operators on values whose type is only known at run time (fields of a record,
// parameters) are replaced by calls of the functions below (ADR 0006): they compute
// exactly with decimal values (shopspring/decimal) and otherwise behave like the
// operators of expr-lang. Operators on known types (1 + 2, now() > …) stay as they are.

// opFuncs are the functions that replace the operators, by operator.
var opFuncs = map[string]string{
	"+": "_add", "-": "_sub", "*": "_mul", "/": "_div", "%": "_mod",
	"<": "_lt", ">": "_gt", "<=": "_le", ">=": "_ge", "==": "_eq", "!=": "_ne",
}

// dynamicOps patches the operators whose operands have no known type.
type dynamicOps struct{}

func (dynamicOps) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.BinaryNode:
		fn, ok := opFuncs[n.Operator]
		if !ok || (!dynamic(n.Left) && !dynamic(n.Right)) {
			return
		}
		ast.Patch(node, &ast.CallNode{Callee: &ast.IdentifierNode{Value: fn}, Arguments: []ast.Node{n.Left, n.Right}})
	case *ast.UnaryNode:
		if n.Operator == "-" && dynamic(n.Node) {
			ast.Patch(node, &ast.CallNode{Callee: &ast.IdentifierNode{Value: "_neg"}, Arguments: []ast.Node{n.Node}})
		}
	}
}

func dynamic(n ast.Node) bool {
	t := n.Type()
	return t == nil || t.Kind() == reflect.Interface
}

var operators = []exprlang.Option{
	exprlang.Patch(dynamicOps{}),
	arith("_add", "+", decimal.Decimal.Add, runtime.Add),
	arith("_sub", "-", decimal.Decimal.Sub, runtime.Subtract),
	arith("_mul", "*", decimal.Decimal.Mul, runtime.Multiply),
	arith("_div", "/", divide, func(a, b any) any { return runtime.Divide(a, b) }),
	arith("_mod", "%", modulo, func(a, b any) any { return runtime.Modulo(a, b) }),
	compare("_lt", "<", func(c int) bool { return c < 0 }, runtime.Less),
	compare("_gt", ">", func(c int) bool { return c > 0 }, runtime.More),
	compare("_le", "<=", func(c int) bool { return c <= 0 }, runtime.LessOrEqual),
	compare("_ge", ">=", func(c int) bool { return c >= 0 }, runtime.MoreOrEqual),
	equal("_eq", false),
	equal("_ne", true),
	exprlang.Function("_neg", func(p ...any) (any, error) {
		switch v := p[0].(type) {
		case nil:
			return nil, nil
		case decimal.Decimal:
			return v.Neg(), nil
		}
		return safe(func() any { return runtime.Negate(p[0]) })
	}),
}

var errDivZero = errors.New("division by zero")

func divide(a, b decimal.Decimal) decimal.Decimal {
	if b.IsZero() {
		panic(errDivZero)
	}
	return a.Div(b)
}

func modulo(a, b decimal.Decimal) decimal.Decimal {
	if b.IsZero() {
		panic(errDivZero)
	}
	return a.Mod(b)
}

// arith computes with decimals if one operand is a decimal, else like expr-lang. With an
// empty value (null) the result is null, as in SQL: a record without amount has no
// amount with tax either.
func arith(name, op string, dec func(a, b decimal.Decimal) decimal.Decimal, plain func(a, b any) any) exprlang.Option {
	return exprlang.Function(name, func(p ...any) (any, error) {
		a, b := p[0], p[1]
		if a == nil || b == nil {
			return nil, nil
		}
		if isDecimal(a) || isDecimal(b) {
			x, y, err := decimals(op, a, b)
			if err != nil {
				return nil, err
			}
			return safe(func() any { return dec(x, y) })
		}
		return safe(func() any { return plain(a, b) })
	})
}

// compare orders two values. A null value is neither smaller nor larger than another:
// the result is false, as in SQL – a filter skips records without a value.
func compare(name, op string, ok func(int) bool, plain func(a, b any) bool) exprlang.Option {
	return exprlang.Function(name, func(p ...any) (any, error) {
		a, b := p[0], p[1]
		if a == nil || b == nil {
			return false, nil
		}
		if isDecimal(a) || isDecimal(b) {
			x, y, err := decimals(op, a, b)
			if err != nil {
				return nil, err
			}
			return ok(x.Cmp(y)), nil
		}
		return safe(func() any { return plain(a, b) })
	})
}

func equal(name string, not bool) exprlang.Option {
	return exprlang.Function(name, func(p ...any) (any, error) {
		a, b := p[0], p[1]
		var eq bool
		if (isDecimal(a) || isDecimal(b)) && a != nil && b != nil {
			x, y, err := decimals("==", a, b)
			eq = err == nil && x.Equal(y) // a decimal is never equal to a text
		} else {
			eq = runtime.Equal(a, b)
		}
		return eq != not, nil
	})
}

func isDecimal(v any) bool {
	_, ok := v.(decimal.Decimal)
	return ok
}

// decimals converts two numbers to decimals; text is not converted (ADR 0006).
func decimals(op string, a, b any) (decimal.Decimal, decimal.Decimal, error) {
	x, okA := toDecimal(a)
	y, okB := toDecimal(b)
	if !okA || !okB {
		return x, y, fmt.Errorf("invalid operation: %s %s %s", typeName(a), op, typeName(b))
	}
	return x, y, nil
}

func toDecimal(v any) (decimal.Decimal, bool) {
	switch v := v.(type) {
	case decimal.Decimal:
		return v, true
	case int:
		return decimal.NewFromInt(int64(v)), true
	case int64:
		return decimal.NewFromInt(v), true
	case int32:
		return decimal.NewFromInt(int64(v)), true
	case float64:
		return decimal.NewFromFloat(v), true
	case float32:
		return decimal.NewFromFloat32(v), true
	}
	return decimal.Decimal{}, false
}

// typeName names a type in messages like the types of records (ADR 0007).
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case decimal.Decimal:
		return "decimal"
	case string:
		return "string"
	case bool:
		return "bool"
	case int, int64, int32:
		return "int"
	case float64, float32:
		return "float"
	}
	return fmt.Sprintf("%T", v)
}

// safe turns the panics of expr-lang's runtime (e.g. "1" + 2) into errors.
func safe(f func() any) (v any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok && errors.Is(e, errDivZero) {
				err = e
				return
			}
			err = fmt.Errorf("%v", r)
		}
	}()
	return f(), nil
}
