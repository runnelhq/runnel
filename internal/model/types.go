package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/record"
)

// ValueType is the declared type of a workflow parameter or output: the value types of
// records (ADR 0007, Nachtrag R5c) without binary.
type ValueType string

// Value types. The zero value means string.
const (
	TypeString   = ValueType(record.String)
	TypeInt      = ValueType(record.Int)
	TypeDecimal  = ValueType(record.Decimal)
	TypeFloat    = ValueType(record.Float)
	TypeBool     = ValueType(record.Bool)
	TypeDate     = ValueType(record.DateType)
	TypeDateTime = ValueType(record.DateTime)
	TypeJSON     = ValueType(record.JSON)
)

var valueTypes = []ValueType{TypeString, TypeInt, TypeDecimal, TypeFloat, TypeBool, TypeDate, TypeDateTime, TypeJSON}

// Valid reports whether t is empty (string) or one of the known types.
func (t ValueType) Valid() bool {
	return t == "" || slices.Contains(valueTypes, t)
}

// OrString returns the type, with the empty type meaning string.
func (t ValueType) OrString() ValueType {
	if t == "" {
		return TypeString
	}
	return t
}

// Parse converts text, as given with --param, by a trigger or the API, to a value of the
// type in the representation of records (ADR 0007): string as it is; int, decimal and
// float in the form 1234.5 (no grouping); bool true/false, also 1/0 and yes/no; date as
// 2026-11-20; datetime in ISO 8601, without a zone in local time; json any JSON value.
// Space around the text is ignored except for string; an empty text is no value.
func (t ValueType) Parse(text string) (any, error) {
	typ := t.OrString()
	switch typ {
	case TypeString:
		return text, nil
	case TypeJSON:
		dec := json.NewDecoder(bytes.NewReader([]byte(text)))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		if dec.More() {
			return nil, fmt.Errorf("invalid JSON: unexpected data after the value")
		}
		return record.Normalize(v)
	}
	if !typ.Valid() {
		return nil, fmt.Errorf("unknown type %q", t)
	}
	s := strings.TrimSpace(text)
	if s == "" {
		return nil, fmt.Errorf("an empty text is not a value of type %s", typ)
	}
	c, err := format.New(record.Type(typ), "")
	if err != nil {
		return nil, err
	}
	v, err := c.Parse(s)
	if err == nil {
		return v, nil
	}
	// The messages of format suggest a format, which parameters do not have.
	if hint, ok := parseHints[typ]; ok {
		return nil, fmt.Errorf("%q is not %s", text, hint)
	}
	return nil, err
}

var parseHints = map[ValueType]string{
	TypeInt:      "a whole number (expected e.g. 1234)",
	TypeDecimal:  "a number (expected e.g. 1234.5)",
	TypeFloat:    "a number (expected e.g. 1234.5)",
	TypeDateTime: "a date and time (expected e.g. 2026-11-20T14:30:00Z or 2026-11-20 14:30:00)",
}

func typeNames() string {
	names := make([]string, len(valueTypes))
	for i, t := range valueTypes {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// CheckValue parses text as the parameter's type and checks it against the allowed
// values; values are equal as in records (1 = 1.0 for numbers).
func (p Parameter) CheckValue(text string) (any, error) {
	v, err := p.Type.Parse(text)
	if err != nil {
		return nil, err
	}
	if len(p.Enum) == 0 {
		return v, nil
	}
	for _, allowed := range p.Enum {
		if a, err := p.Type.Parse(allowed); err == nil && record.Key(a) == record.Key(v) {
			return v, nil
		}
	}
	return nil, fmt.Errorf("%q is not one of the allowed values %s", text, strings.Join(p.Enum, ", "))
}

// hasReferences reports whether text contains variables or expressions,
// whose value is only known at run time.
func hasReferences(text string) bool {
	return strings.Contains(text, "${") || strings.Contains(text, "{{")
}
