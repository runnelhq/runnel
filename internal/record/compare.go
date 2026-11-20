package record

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Kind groups the value types that can be compared with each other (core.sort,
// core.group-by): numbers (int, decimal and float), text, bools, dates and times.
type Kind string

// Kinds of values; Incomparable for binary and json values.
const (
	KindNone     Kind = ""
	KindNumber   Kind = "number"
	KindString   Kind = "text"
	KindBool     Kind = "bool"
	KindDate     Kind = "date"
	KindDateTime Kind = "datetime"
	Incomparable Kind = "incomparable"
)

// KindOf returns the kind of a value; KindNone for nil.
func KindOf(v any) Kind {
	switch v.(type) {
	case nil:
		return KindNone
	case int64, decimal.Decimal, float64:
		return KindNumber
	case string:
		return KindString
	case bool:
		return KindBool
	case Date:
		return KindDate
	case time.Time:
		return KindDateTime
	}
	return Incomparable
}

// Compare orders two values of the same kind (see KindOf): -1, 0 or +1. Numbers are
// compared exactly across int, decimal and float; text by Unicode code points; false
// before true. Values of different kinds give 0; callers check the kinds first.
func Compare(a, b any) int {
	switch x := a.(type) {
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y)
		}
	case bool:
		if y, ok := b.(bool); ok {
			switch {
			case x == y:
				return 0
			case !x:
				return -1
			}
			return 1
		}
	case Date:
		if y, ok := b.(Date); ok {
			return x.In(time.UTC).Compare(y.In(time.UTC))
		}
	case time.Time:
		if y, ok := b.(time.Time); ok {
			return x.Compare(y)
		}
	case int64:
		if y, ok := b.(int64); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	case float64:
		if y, ok := b.(float64); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	}
	if x, ok := ToDecimal(a); ok {
		if y, ok := ToDecimal(b); ok {
			return x.Cmp(y)
		}
	}
	return 0
}

// ToDecimal converts a number (int, decimal, float) to a decimal.
func ToDecimal(v any) (decimal.Decimal, bool) {
	switch v := v.(type) {
	case int64:
		return decimal.NewFromInt(v), true
	case decimal.Decimal:
		return v, true
	case float64:
		return decimal.NewFromFloat(v), true
	}
	return decimal.Decimal{}, false
}

// Key returns a text that is the same for equal values and different otherwise, for
// grouping: numbers are equal across int, decimal and float (1 = 1.0), null has a key
// of its own.
func Key(v any) string {
	switch v := v.(type) {
	case nil:
		return "n"
	case string:
		return "s" + v
	case bool:
		return "b" + strconv.FormatBool(v)
	case Date:
		return "d" + v.String()
	case time.Time:
		return "t" + v.UTC().Format(time.RFC3339Nano)
	case int64, decimal.Decimal, float64:
		d, _ := ToDecimal(v)
		return "#" + d.String()
	case []byte:
		return "x" + string(v)
	}
	b, _ := json.Marshal(v)
	return "j" + string(b)
}
