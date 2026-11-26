package script

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/dop251/goja"
	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// maxSafeInt is the largest integer a JavaScript number holds exactly (2^53).
const maxSafeInt = 1 << 53

// maxDepth limits the nesting of a result; deeper results are most likely circular.
const maxDepth = 100

// Value converts a Go value of a run to JavaScript (ADR 0028): texts, numbers and
// true/false as they are, integers beyond ±2^53 as BigInt, decimal numbers and dates
// as text ("12.50", "2026-11-25"), times as Date, bytes as Uint8Array, maps as objects
// (keys sorted) and slices as arrays; nil is null. Other values become text.
func (r *Runtime) Value(v any) goja.Value {
	switch v := v.(type) {
	case nil:
		return goja.Null()
	case string, bool, float64:
		return r.vm.ToValue(v)
	case int64:
		if v > maxSafeInt || v < -maxSafeInt {
			return r.vm.ToValue(big.NewInt(v))
		}
		return r.vm.ToValue(v)
	case int:
		return r.Value(int64(v))
	case decimal.Decimal:
		if e := v.Exponent(); e < 0 {
			return r.vm.ToValue(v.StringFixed(-e)) // keeps the places, e.g. "12.50"
		}
		return r.vm.ToValue(v.String())
	case record.Date:
		return r.vm.ToValue(v.String())
	case time.Time:
		d, err := r.vm.New(r.vm.Get("Date"), r.vm.ToValue(v.UnixMilli()))
		if err != nil {
			return r.vm.ToValue(v.Format(time.RFC3339Nano))
		}
		return d
	case []byte:
		a, err := r.vm.New(r.vm.Get("Uint8Array"), r.vm.ToValue(r.vm.NewArrayBuffer(slices.Clone(v))))
		if err != nil {
			return goja.Null()
		}
		return a
	case map[string]any:
		obj := r.vm.NewObject()
		for _, k := range slices.Sorted(maps.Keys(v)) {
			_ = obj.Set(k, r.Value(v[k]))
		}
		return obj
	case []any:
		items := make([]any, len(v))
		for i, x := range v {
			items[i] = r.Value(x)
		}
		return r.vm.NewArray(items...)
	case []map[string]any:
		items := make([]any, len(v))
		for i, x := range v {
			items[i] = r.Value(x)
		}
		return r.vm.NewArray(items...)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return r.Value(rv.Int())
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return r.Value(int64(rv.Uint())) //nolint:gosec // at most 32 bits
	case reflect.Float32:
		return r.vm.ToValue(rv.Float())
	case reflect.Slice, reflect.Array:
		items := make([]any, rv.Len())
		for i := range items {
			items[i] = r.Value(rv.Index(i).Interface())
		}
		return r.vm.NewArray(items...)
	}
	// No Go objects in the script: they could offer methods of the host.
	return r.vm.ToValue(fmt.Sprint(v))
}

// Export converts a JavaScript value to Go (ADR 0028): text, whole numbers as int64,
// other numbers as float64 (NaN and Infinity as nil, like JSON.stringify), BigInt as
// int64, true/false, Date as time.Time, arrays as []any and objects as map[string]any;
// null and undefined are nil. Functions, symbols and results larger than
// MaxResultBytes (as JSON) are errors.
func (r *Runtime) Export(v goja.Value) (any, error) {
	size := 0
	return r.export(v, 0, &size)
}

var errTooLarge = fmt.Errorf("the result is larger than %d MiB", MaxResultBytes>>20)

func (r *Runtime) export(v goja.Value, depth int, size *int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("the result is nested deeper than %d levels (or refers to itself)", maxDepth)
	}
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		*size += 4
		return nil, nil
	}
	if _, ok := v.(*goja.Symbol); ok {
		return nil, errors.New("a symbol cannot be part of the result")
	}
	obj, ok := v.(*goja.Object)
	if !ok {
		out, n, err := primitive(v.Export())
		*size += n
		if err == nil && *size > MaxResultBytes {
			err = errTooLarge
		}
		return out, err
	}
	if _, fn := goja.AssertFunction(obj); fn {
		return nil, errors.New("a function cannot be part of the result")
	}
	switch obj.ClassName() {
	case "Date":
		*size += 32
		if t, ok := obj.Export().(time.Time); ok {
			return t, nil
		}
		return nil, nil
	case "Array":
		n := int(obj.Get("length").ToInteger())
		out := make([]any, 0, min(n, 1024))
		for i := range n {
			*size++
			x, err := r.export(obj.Get(strconv.Itoa(i)), depth+1, size)
			if err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		*size += 2
		return out, nil
	case "String":
		*size += len(obj.String()) + 2
		return obj.String(), nil
	case "Number":
		out, n, err := primitive(obj.ToFloat())
		*size += n
		return out, err
	case "Boolean":
		*size += 5
		return obj.ToBoolean(), nil
	}
	keys := obj.Keys()
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		x := obj.Get(k)
		if _, fn := goja.AssertFunction(x); fn {
			continue // like JSON.stringify: methods are left out
		}
		*size += len(k) + 4
		e, err := r.export(x, depth+1, size)
		if err != nil {
			return nil, err
		}
		out[k] = e
	}
	*size += 2
	return out, nil
}

// primitive converts an exported primitive and returns its size as JSON (about).
func primitive(v any) (any, int, error) {
	switch v := v.(type) {
	case string:
		return v, len(v) + 2, nil
	case bool:
		return v, 5, nil
	case int64:
		return v, 20, nil
	case float64:
		switch {
		case math.IsNaN(v) || math.IsInf(v, 0):
			return nil, 4, nil
		case v == math.Trunc(v) && math.Abs(v) <= maxSafeInt:
			return int64(v), 20, nil
		}
		return v, 24, nil
	case *big.Int:
		if !v.IsInt64() {
			return nil, 0, fmt.Errorf("the BigInt %s is too large for an integer", v)
		}
		return v.Int64(), 20, nil
	}
	return nil, 0, fmt.Errorf("a value of type %T cannot be part of the result", v)
}
