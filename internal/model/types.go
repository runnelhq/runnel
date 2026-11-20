package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ValueType is the declared type of a workflow parameter or output (see ADR 0012).
type ValueType string

// Value types. The zero value means string.
const (
	TypeString  ValueType = "string"
	TypeInteger ValueType = "integer"
	TypeNumber  ValueType = "number"
	TypeBoolean ValueType = "boolean"
	TypeJSON    ValueType = "json"
)

var valueTypes = []ValueType{TypeString, TypeInteger, TypeNumber, TypeBoolean, TypeJSON}

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

// Parse converts text to a value of the type: string → string, integer → int64,
// number → float64, boolean → bool ("true"/"false"), json → any decoded JSON value.
func (t ValueType) Parse(text string) (any, error) {
	switch t.OrString() {
	case TypeString:
		return text, nil
	case TypeInteger:
		v, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", text)
		}
		return v, nil
	case TypeNumber:
		v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", text)
		}
		return v, nil
	case TypeBoolean:
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a boolean (expected true or false)", text)
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
		return v, nil
	}
	return nil, fmt.Errorf("unknown type %q", t)
}

func typeNames() string {
	names := make([]string, len(valueTypes))
	for i, t := range valueTypes {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// CheckValue parses text as the parameter's type and checks it against the allowed values.
func (p Parameter) CheckValue(text string) (any, error) {
	v, err := p.Type.Parse(text)
	if err != nil {
		return nil, err
	}
	if len(p.Enum) == 0 {
		return v, nil
	}
	for _, allowed := range p.Enum {
		if a, err := p.Type.Parse(allowed); err == nil && reflect.DeepEqual(a, v) {
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
