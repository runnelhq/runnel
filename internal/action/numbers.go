package action

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Number fields (R5b2): a field of a number type (int, float, also nested and in lists)
// takes a number or a text. A text that reads as a number is that number; a text with
// ${…} – in actions also {{ … }} – is resolved only when the run starts and keeps the
// field at its default until then ("runnel validate" leaves it to the start of the run);
// any other text is an error. Number fields of type string (e.g. maxRows) follow the
// same rule with ParseWholeNumber.

// Deferred reports whether a value is resolved only when the run starts: it holds ${…}
// or, where the configuration may use expressions (actions), {{ … }}.
func Deferred(text string, expressions bool) bool {
	return strings.Contains(text, "${") || expressions && strings.Contains(text, "{{")
}

// ParseWholeNumber reads a whole number written as text in a field of type string by the
// rule of the number fields: ok is false for an empty text and for one resolved only when
// the run starts (Deferred); any other text that is not a whole number is an error.
func ParseWholeNumber(field, text string, expressions bool) (n int64, ok bool, err error) {
	s := strings.TrimSpace(text)
	if s == "" || Deferred(s, expressions) {
		return 0, false, nil
	}
	if n, err = strconv.ParseInt(s, 10, 64); err != nil {
		return 0, false, fmt.Errorf("field %q: expected a whole number, got %q", field, text)
	}
	return n, true, nil
}

// numberTexts returns with, where the texts in number fields of the struct type t are
// replaced by their numbers and those resolved only when the run starts are left out.
// Maps and lists are copied before they change; with itself stays as it is.
func numberTexts(with map[string]any, t reflect.Type, path string, expressions bool) (map[string]any, bool, error) {
	out, changed := with, false
	for _, f := range configFields(t) {
		raw, ok := with[f.name]
		if !ok || raw == nil {
			continue
		}
		v, keep, c, err := numberValue(raw, f.typ, join(path, f.name), expressions)
		if err != nil {
			return nil, false, err
		}
		if !c {
			continue
		}
		if !changed {
			out, changed = maps.Clone(with), true
		}
		if keep {
			out[f.name] = v
		} else {
			delete(out, f.name)
		}
	}
	return out, changed, nil
}

// numberValue is numberTexts for one value of type t: keep is false for a value left
// out, changed is true when the value is not raw.
func numberValue(raw any, t reflect.Type, path string, expressions bool) (v any, keep, changed bool, err error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		s, isText := raw.(string)
		if !isText {
			return raw, true, false, nil
		}
		if Deferred(s, expressions) {
			return nil, false, true, nil
		}
		n, err := parseNumber(strings.TrimSpace(s), t)
		if err != nil {
			return nil, false, false, fmt.Errorf("field %q: expected %s, got %q", path, numberKind(t), s)
		}
		return n, true, true, nil
	case reflect.Struct:
		if m, ok := raw.(map[string]any); ok {
			out, changed, err := numberTexts(m, t, path, expressions)
			return out, true, changed, err
		}
	case reflect.Slice, reflect.Array:
		list, ok := raw.([]any)
		if !ok {
			break
		}
		var out []any
		for i, e := range list {
			x, keep, c, err := numberValue(e, t.Elem(), fmt.Sprintf("%s[%d]", path, i), expressions)
			if err != nil {
				return nil, false, false, err
			}
			if !c {
				continue
			}
			if out == nil {
				out = slices.Clone(list)
			}
			if !keep {
				x = nil // in a list, a value resolved later is a placeholder until then
			}
			out[i] = x
		}
		if out != nil {
			return out, true, true, nil
		}
	}
	return raw, true, false, nil
}

// parseNumber reads a text as a number of the kind of t; the result is written as JSON.
func parseNumber(s string, t reflect.Type) (any, error) {
	switch t.Kind() {
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, t.Bits())
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("not a number")
		}
		return f, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.ParseUint(s, 10, t.Bits())
	default:
		return strconv.ParseInt(s, 10, t.Bits())
	}
}

// numberKind names the kind of number a field expects, for error messages.
func numberKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number of 0 or more"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "a whole number"
	}
	return t.Kind().String()
}

type configField struct {
	name string
	typ  reflect.Type
}

// configFields returns the fields encoding/json decodes into the struct type t, by their
// json names, including those of embedded structs.
func configFields(t reflect.Type) []configField {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []configField
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" {
			out = append(out, configFields(f.Type)...)
			continue
		}
		if !f.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, configField{name, f.Type})
	}
	return out
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
