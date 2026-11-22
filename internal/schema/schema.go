// Package schema derives JSON Schemas (draft 2020-12) from the configuration structs
// of actions, so that code and schema cannot drift apart (ADR 0014). The editor builds
// its property dialogs from these schemas; the same schemas describe actions as tools.
//
// A configuration struct describes its fields with tags:
//
//		type wait struct {
//			Duration string `json:"duration" doc:"How long to wait" schema:"required,format=runnel-duration,example=30s"`
//		}
//
//	  - json: the field name as written in workflow files; fields without a json name
//	    or with json:"-" and unexported fields are not part of the schema.
//	  - doc: description of the field for editor and tools (English, one sentence).
//	  - schema: comma-separated options:
//	    required, default=<value>, enum=<a>|<b>|…, example=<value>, format=<name>,
//	    only=<field>:<a>|<b>|… (the field applies only when the sibling field has one of
//	    these values, e.g. only=function:concat; "x-runnel-only" in the schema, E14).
//	    Values must not contain commas.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Draft is the JSON Schema dialect of all generated schemas.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// FormatDuration marks a time span as written in workflow files, e.g. "30s", "5m",
// "1h30m". JSON Schema's own "duration" format means ISO 8601, so Runnel uses its own name.
const FormatDuration = "runnel-duration"

// FormatText marks a text that may span several lines (the editor shows a growing text
// field instead of a single line, decision for mockup E7).
const FormatText = "runnel-text"

// FormatField marks the name of a field of the incoming records (E14b): the editor offers
// the fields known on the incoming link. Names of new fields are not marked.
const FormatField = "runnel-field"

// FormatTarget marks the id of a transform that a data link of this transform leads to,
// e.g. "to" of core.switch (E14b): the editor offers the targets of the links.
const FormatTarget = "runnel-target"

// OnlyKey is the schema keyword of the option only=: the field applies only when a
// sibling field has one of the given values. The editor hides it otherwise (mockup E14).
const OnlyKey = "x-runnel-only"

// Schema is a JSON Schema, limited to what configuration structs need.
type Schema struct {
	Schema      string     `json:"$schema,omitempty"`
	Type        string     `json:"type,omitempty"`
	Description string     `json:"description,omitempty"`
	Format      string     `json:"format,omitempty"`
	Enum        []any      `json:"enum,omitempty"`
	Default     any        `json:"default,omitempty"`
	Examples    []any      `json:"examples,omitempty"`
	Minimum     *int       `json:"minimum,omitempty"`
	Items       *Schema    `json:"items,omitempty"`
	Properties  Properties `json:"properties,omitempty"`
	Required    []string   `json:"required,omitempty"`
	// Only: see OnlyKey; sibling field name → values.
	Only map[string][]any `json:"x-runnel-only,omitempty"`
	// AdditionalProperties is false for structs (unknown fields are errors) and the
	// value schema for maps.
	AdditionalProperties any `json:"additionalProperties,omitempty"`
}

// Property is a named field of an object schema.
type Property struct {
	Name   string
	Schema *Schema
}

// Properties are the fields of an object schema in the order of the struct,
// which is the order the editor shows them in.
type Properties []Property

// MarshalJSON writes the properties as a JSON object, keeping their order.
func (ps Properties) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range ps {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(p.Name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(p.Schema)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads properties from a JSON object, keeping their order.
func (ps *Properties) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("properties must be a JSON object")
	}
	*ps = nil
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, _ := tok.(string)
		s := &Schema{}
		if err := dec.Decode(s); err != nil {
			return err
		}
		*ps = append(*ps, Property{Name: name, Schema: s})
	}
	_, err := dec.Token()
	return err
}

// Lookup returns the schema of a property.
func (ps Properties) Lookup(name string) (*Schema, bool) {
	for _, p := range ps {
		if p.Name == name {
			return p.Schema, true
		}
	}
	return nil, false
}

// For returns the schema of a configuration struct. config is a struct value or a
// pointer to one; nil stands for "no configuration" and yields an object schema
// without properties.
func For(config any) (*Schema, error) {
	return build(config, "json")
}

// ForFile returns the schema of a file format (Project, Environment, Trigger …): like
// For, but the field names come from the yaml tags, since the structs of package model
// also carry json names for the API that are not part of the files (S58).
func ForFile(format any) (*Schema, error) {
	return build(format, "yaml")
}

func build(config any, tag string) (*Schema, error) {
	s := &Schema{Type: "object", AdditionalProperties: false}
	if config != nil {
		t := reflect.TypeOf(config)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return nil, fmt.Errorf("configuration must be a struct, not %s", t)
		}
		var err error
		if s, err = forType(t, "", nil, tag); err != nil {
			return nil, err
		}
	}
	s.Schema = Draft
	return s, nil
}

func forType(t reflect.Type, path string, seen []reflect.Type, tag string) (*Schema, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}, nil
	case reflect.Bool:
		return &Schema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &Schema{Type: "integer"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		zero := 0
		return &Schema{Type: "integer", Minimum: &zero}, nil
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}, nil
	case reflect.Interface:
		return &Schema{}, nil // any value
	case reflect.Slice, reflect.Array:
		items, err := forType(t.Elem(), path+"[]", seen, tag)
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "array", Items: items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%s: map keys must be strings", describe(path))
		}
		values, err := forType(t.Elem(), path+"{}", seen, tag)
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "object", AdditionalProperties: values}, nil
	case reflect.Struct:
		for _, s := range seen {
			if s == t {
				return nil, fmt.Errorf("%s: recursive type %s", describe(path), t)
			}
		}
		return forStruct(t, path, append(seen, t), tag)
	default:
		return nil, fmt.Errorf("%s: unsupported type %s", describe(path), t)
	}
}

func forStruct(t reflect.Type, path string, seen []reflect.Type, tag string) (*Schema, error) {
	s := &Schema{Type: "object", AdditionalProperties: false}
	for _, f := range fieldsBy(t, tag) {
		fieldPath := join(path, f.name)
		if f.options.err != nil {
			return nil, fmt.Errorf("%s: %w", describe(fieldPath), f.options.err)
		}
		fs, err := forType(f.typ, fieldPath, seen, tag)
		if err != nil {
			return nil, err
		}
		fs.Description = f.doc
		if err := applyOptions(fs, f.options, fieldPath); err != nil {
			return nil, err
		}
		if _, dup := s.Properties.Lookup(f.name); dup {
			return nil, fmt.Errorf("%s: field name used twice", describe(fieldPath))
		}
		s.Properties = append(s.Properties, Property{f.name, fs})
		if f.options.required {
			s.Required = append(s.Required, f.name)
		}
	}
	for _, p := range s.Properties {
		for sibling := range p.Schema.Only {
			if _, ok := s.Properties.Lookup(sibling); !ok || sibling == p.Name {
				return nil, fmt.Errorf("%s: only= names %q, which is not another field", describe(join(path, p.Name)), sibling)
			}
		}
	}
	return s, nil
}

func applyOptions(s *Schema, o options, path string) error {
	if o.only != "" {
		field, values, ok := strings.Cut(o.only, ":")
		if !ok || field == "" || values == "" {
			return fmt.Errorf("%s: only must be <field>:<value>|<value>…", describe(path))
		}
		s.Only = map[string][]any{}
		for v := range strings.SplitSeq(values, "|") {
			s.Only[field] = append(s.Only[field], v)
		}
	}
	if o.format != "" {
		switch {
		case s.Type == "string":
			s.Format = o.format
		case s.Type == "array" && s.Items != nil && s.Items.Type == "string":
			// A list of texts: the format describes each text, e.g. field names (E14b).
			s.Items.Format = o.format
		default:
			return fmt.Errorf("%s: format is only allowed for strings and lists of strings", describe(path))
		}
	}
	var err error
	if o.defaultValue != nil {
		if s.Default, err = parseValue(s.Type, *o.defaultValue); err != nil {
			return fmt.Errorf("%s: default: %w", describe(path), err)
		}
	}
	if o.example != nil {
		v, err := parseValue(s.Type, *o.example)
		if err != nil {
			return fmt.Errorf("%s: example: %w", describe(path), err)
		}
		s.Examples = []any{v}
	}
	for _, e := range o.enum {
		v, err := parseValue(s.Type, e)
		if err != nil {
			return fmt.Errorf("%s: enum: %w", describe(path), err)
		}
		s.Enum = append(s.Enum, v)
	}
	return nil
}

// parseValue converts a tag value to the JSON type of the field.
func parseValue(typ, text string) (any, error) {
	switch typ {
	case "string":
		return text, nil
	case "boolean":
		return strconv.ParseBool(text)
	case "integer":
		return strconv.ParseInt(text, 10, 64)
	case "number":
		return strconv.ParseFloat(text, 64)
	}
	return nil, fmt.Errorf("values are only supported for strings, numbers and booleans, not %q", typ)
}

type field struct {
	name    string
	doc     string
	typ     reflect.Type
	options options
}

type options struct {
	required     bool
	defaultValue *string
	example      *string
	enum         []string
	format       string
	only         string
	err          error
}

// fields returns the configuration fields of a struct, including those of embedded
// structs, in declaration order – the fields encoding/json decodes.
func fields(t reflect.Type) []field { return fieldsBy(t, "json") }

// fieldsBy is fields with the names from the given tag (json or yaml).
func fieldsBy(t reflect.Type, tag string) []field {
	var out []field
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
		if f.Anonymous && name == "" {
			et := f.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				out = append(out, fieldsBy(et, tag)...)
				continue
			}
		}
		if !f.IsExported() || name == "" || name == "-" {
			continue
		}
		out = append(out, field{
			name:    name,
			doc:     f.Tag.Get("doc"),
			typ:     f.Type,
			options: parseOptions(f.Tag.Get("schema")),
		})
	}
	return out
}

func parseOptions(tag string) options {
	var o options
	if tag == "" {
		return o
	}
	for opt := range strings.SplitSeq(tag, ",") {
		key, value, hasValue := strings.Cut(opt, "=")
		switch {
		case key == "required" && !hasValue:
			o.required = true
		case key == "default" && hasValue:
			o.defaultValue = &value
		case key == "example" && hasValue:
			o.example = &value
		case key == "enum" && hasValue:
			o.enum = strings.Split(value, "|")
		case key == "format" && hasValue:
			o.format = value
		case key == "only" && hasValue:
			o.only = value
		default:
			o.err = fmt.Errorf("unknown schema option %q", opt)
		}
	}
	return o
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func describe(path string) string {
	if path == "" {
		return "configuration"
	}
	return fmt.Sprintf("field %q", path)
}

// MissingRequired returns the path of the first required field that is missing in
// data, the decoded configuration of the struct config (e.g. "duration" or
// "target.host"), or "" if all required fields are present. A field set to null
// counts as missing. Nested objects are checked where data contains them.
func MissingRequired(config any, data map[string]any) string {
	if config == nil {
		return ""
	}
	return missing(reflect.TypeOf(config), data, "")
}

func missing(t reflect.Type, value any, path string) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		data, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		for _, f := range fields(t) {
			fieldPath := join(path, f.name)
			v, present := data[f.name]
			if present && v == nil {
				present = false
			}
			if !present {
				if f.options.required {
					return fieldPath
				}
				continue
			}
			if m := missing(f.typ, v, fieldPath); m != "" {
				return m
			}
		}
	case reflect.Slice, reflect.Array:
		items, _ := value.([]any)
		for i, item := range items {
			if m := missing(t.Elem(), item, fmt.Sprintf("%s[%d]", path, i)); m != "" {
				return m
			}
		}
	case reflect.Map:
		entries, _ := value.(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			if m := missing(t.Elem(), entries[key], join(path, key)); m != "" {
				return m
			}
		}
	}
	return ""
}
