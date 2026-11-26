package model

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// unknownFields returns every key of the document that the target type does not know,
// sorted by position, with a suggestion for typos. The YAML library rejects unknown
// fields as well, but reports only one of them, and with several not always the same.
func unknownFields(file string, node ast.Node, t reflect.Type) Errors {
	var errs Errors
	walkFields(node, t, func(key ast.Node, st reflect.Type, name string, known []string) {
		msg := fmt.Sprintf("unknown field %q%s", name, Suggest(name, known))
		if use, ok := renamedFields[st][name]; ok {
			msg = fmt.Sprintf("field %q was renamed before 1.0: use %s", name, use)
		}
		errs = append(errs, &Error{File: file, Pos: nodePos(key), Msg: msg})
	})
	errs.Sort()
	return errs
}

// renamedFields holds fields renamed before 1.0 by struct and old name, with what to
// write instead (R5b): a clean break with a clear message instead of "unknown field".
// Renames after 1.0 keep the old name until the next major version (ADR 0029).
var renamedFields = map[reflect.Type]map[string]string{
	reflect.TypeFor[TriggerSettings](): {"maxConcurrent": "maxConcurrentRuns (as in settings of project.yaml)"},
	reflect.TypeFor[Project]():         {"workflow": "settings.defaultJoin instead of workflow.defaultJoin"},
}

// walkFields compares the keys of mappings with the fields of structs (yaml tags) and
// descends into known fields, slices and maps. Values of other types are not checked here.
func walkFields(node ast.Node, t reflect.Type, unknown func(key ast.Node, st reflect.Type, name string, known []string)) {
	node = unwrap(node)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := node.(*ast.MappingNode)
		if !ok {
			return // a type error, reported by the YAML library
		}
		fields := yamlFields(t)
		known := make([]string, 0, len(fields))
		for name := range fields {
			known = append(known, name)
		}
		slices.Sort(known) // the same suggestion every time when names are equally close
		for _, kv := range m.Values {
			name := strings.TrimSpace(kv.Key.String())
			if name == "<<" {
				continue // merge key
			}
			ft, ok := fields[name]
			if !ok {
				unknown(kv.Key, t, name, known)
				continue
			}
			walkFields(kv.Value, ft, unknown)
		}
	case reflect.Slice, reflect.Array:
		if s, ok := node.(*ast.SequenceNode); ok {
			for _, v := range s.Values {
				walkFields(v, t.Elem(), unknown)
			}
		}
	case reflect.Map:
		if m, ok := node.(*ast.MappingNode); ok {
			for _, kv := range m.Values {
				walkFields(kv.Value, t.Elem(), unknown)
			}
		}
	}
}

// unwrap returns the value behind anchors and tags.
func unwrap(n ast.Node) ast.Node {
	for {
		switch v := n.(type) {
		case *ast.AnchorNode:
			n = v.Value
		case *ast.TagNode:
			n = v.Value
		default:
			return n
		}
	}
}

// yamlFields returns the fields of a struct by their YAML name.
func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = strings.ToLower(f.Name)
		}
		out[name] = f.Type
	}
	return out
}
