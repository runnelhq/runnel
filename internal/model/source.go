package model

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// source is a parsed YAML file. It decodes into Go values and resolves
// YAML paths (e.g. "$.actions[1].type") to positions for error messages.
type source struct {
	name string
	file *ast.File
	body ast.Node
}

// parseSource parses data as a single YAML document.
func parseSource(name string, data []byte) (*source, error) {
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, Errors{yamlError(name, err)}
	}
	var docs []*ast.DocumentNode
	for _, d := range file.Docs {
		if d.Body != nil {
			docs = append(docs, d)
		}
	}
	switch len(docs) {
	case 0:
		return nil, Errors{{File: name, Msg: "file is empty"}}
	case 1:
		return &source{name: name, file: file, body: docs[0].Body}, nil
	default:
		return nil, Errors{{File: name, Pos: tokenPos(docs[1].Body.GetToken()), Msg: "file must contain exactly one YAML document"}}
	}
}

// decode decodes the document into v. Unknown fields are rejected.
func (s *source) decode(v any) error {
	if err := yaml.NodeToValue(s.body, v, yaml.Strict()); err != nil {
		return Errors{yamlError(s.name, err)}
	}
	return nil
}

// pos returns the position of the node at the given YAML path. If the node does not
// exist (e.g. a missing required field), the position of the closest existing parent is used.
func (s *source) pos(path string) Position {
	for p := path; p != ""; p = parentPath(p) {
		yp, err := yaml.PathString(p)
		if err != nil {
			continue
		}
		if n, err := yp.FilterFile(s.file); err == nil && n != nil {
			return nodePos(n)
		}
	}
	return Position{Line: 1, Column: 1}
}

// errorf creates an error located at the given YAML path.
func (s *source) errorf(path, format string, args ...any) *Error {
	return &Error{File: s.name, Pos: s.pos(path), Msg: fmt.Sprintf(format, args...)}
}

// parentPath strips the last element from a YAML path: "$.a[1].b" → "$.a[1]" → "$.a" → "$" → "".
func parentPath(p string) string {
	if p == "$" {
		return ""
	}
	i := strings.LastIndexAny(p, ".[")
	if i <= 0 {
		return "$"
	}
	return p[:i]
}

// nodePos returns where a node starts. For mappings this is the first key, so that
// errors about a list item point to the item itself rather than to one of its values.
func nodePos(n ast.Node) Position {
	switch n := n.(type) {
	case *ast.MappingNode:
		if len(n.Values) > 0 {
			return nodePos(n.Values[0])
		}
	case *ast.MappingValueNode:
		return nodePos(n.Key)
	}
	return tokenPos(n.GetToken())
}

func tokenPos(t *token.Token) Position {
	if t == nil || t.Position == nil {
		return Position{}
	}
	return Position{Line: t.Position.Line, Column: t.Position.Column}
}

// yamlError converts an error of the YAML library into an Error with a
// user-facing message (no Go type names).
func yamlError(file string, err error) *Error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		return &Error{File: file, Pos: tokenPos(typeErr.Token), Msg: fmt.Sprintf("expected %s, got %s",
			yamlKind(typeErr.DstType), yamlKind(typeErr.SrcType))}
	}
	var syntaxErr *yaml.SyntaxError
	if errors.As(err, &syntaxErr) {
		return &Error{File: file, Pos: tokenPos(syntaxErr.Token), Msg: "syntax error: " + syntaxErr.Message}
	}
	var yerr yaml.Error
	if errors.As(err, &yerr) {
		return &Error{File: file, Pos: tokenPos(yerr.GetToken()), Msg: yerr.GetMessage()}
	}
	return &Error{File: file, Msg: err.Error()}
}

// yamlKind describes a Go type in YAML terms.
func yamlKind(t reflect.Type) string {
	if t == nil {
		return "value"
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Map, reflect.Struct:
		return "a mapping"
	case reflect.Pointer:
		return yamlKind(t.Elem())
	default:
		return "a value"
	}
}
