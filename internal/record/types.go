package record

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/shopspring/decimal"
)

// Type is the value type of a field (ADR 0007). Every field may also be null (nil).
type Type string

// Value types and their Go representation.
const (
	// Unknown: the type is not known, e.g. in an open schema or for a null constant.
	Unknown  Type = ""
	String   Type = "string"   // string
	Int      Type = "int"      // int64
	Decimal  Type = "decimal"  // decimal.Decimal – exact, for amounts of money
	Float    Type = "float"    // float64
	Bool     Type = "bool"     // bool
	DateType Type = "date"     // Date – a day without time and time zone
	DateTime Type = "datetime" // time.Time
	Binary   Type = "binary"   // []byte
	JSON     Type = "json"     // map[string]any or []any – nested structures
)

// Types lists the value types in the order of ADR 0007.
var Types = []Type{String, Int, Decimal, Float, Bool, DateType, DateTime, Binary, JSON}

// Valid reports whether t is one of Types (Unknown is not).
func (t Type) Valid() bool {
	return slices.Contains(Types, t)
}

// TypeOf returns the type of a value in its Go representation; Unknown for nil and
// values that are not normalized (see Normalize).
func TypeOf(v any) Type {
	switch v.(type) {
	case string:
		return String
	case int64:
		return Int
	case decimal.Decimal:
		return Decimal
	case float64:
		return Float
	case bool:
		return Bool
	case Date:
		return DateType
	case time.Time:
		return DateTime
	case []byte:
		return Binary
	case map[string]any, []any:
		return JSON
	}
	return Unknown
}

// Normalize converts a Go value into the representation of its value type, e.g. int
// or uint64 (as read from YAML) into int64. nil stays nil. Values without a value
// type (e.g. a struct) are an error.
func Normalize(v any) (any, error) {
	switch v := v.(type) {
	case nil, string, int64, decimal.Decimal, float64, bool, Date, time.Time, []byte:
		return v, nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case uint:
		return unsigned(uint64(v))
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return unsigned(v)
	case float32:
		return float64(v), nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		return v.Float64()
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			n, err := Normalize(e)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = n
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			n, err := Normalize(e)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = n
		}
		return out, nil
	}
	return nil, fmt.Errorf("a value of Go type %T has no value type", v)
}

func unsigned(v uint64) (any, error) {
	if v > math.MaxInt64 {
		return nil, fmt.Errorf("the number %d is too large (at most %d)", v, int64(math.MaxInt64))
	}
	return int64(v), nil
}
