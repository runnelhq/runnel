package model

import (
	"errors"
	"fmt"
	"strings"
)

// ConnectionField sets one field of a connection file (form of the editor, S39c2a):
// description, type or with.<name>. A nil value removes the field; Map sets a mapping of
// texts instead (e.g. with.headers).
type ConnectionField struct {
	Field string     `json:"field"`
	Value any        `json:"value"`
	Map   []KeyValue `json:"map,omitempty"`
}

// EditConnection sets a field in the text of a connection file and returns the new text.
// Like EditWorkflow, only the lines concerned change: comments, order and formatting of
// the rest stay. The file must be readable YAML with a mapping at the top.
func EditConnection(name string, data []byte, f ConnectionField) ([]byte, error) {
	field, sub, nested := strings.Cut(f.Field, ".")
	switch {
	case !nested && (field == "description" || field == "type"):
	case nested && field == "with" && configKeyPattern.MatchString(sub):
	default:
		return nil, fmt.Errorf("field %q cannot be set (expected description, type or with.<name>)", f.Field)
	}
	if field == "type" && f.Value == nil {
		return nil, errors.New("the type of a connection cannot be removed")
	}
	text := string(data)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	src, err := parseSource(name, []byte(text))
	if err != nil {
		return nil, err
	}
	top := entries(src.body)
	if len(top) == 0 {
		return nil, errors.New("the file has no fields; change it in the YAML view")
	}
	e := &editor{top: top, lines: strings.Split(strings.TrimSuffix(text, "\n"), "\n")}
	value := f.Value
	if len(f.Map) > 0 {
		value = inFileOrder(f.Map, entries(valueOf(entries(valueOf(top, "with")), sub)))
	}
	// A new description follows the name, a new type the description or the name;
	// a new with: goes to the end.
	after := len(e.lines)
	for _, k := range map[string][]string{"description": {"name"}, "type": {"description", "name"}}[field] {
		if k := keyOf(top, k); k != nil {
			after = maxLine(k)
			break
		}
	}
	if after == len(e.lines) {
		e.atEnd = true
	}
	if err := e.setField(top, after, "", f.Field, value); err != nil {
		return nil, err
	}
	out := strings.Join(applyLineEdits(e.lines, e.edits), eol)
	if strings.HasSuffix(text, "\n") || e.atEnd {
		out += eol
	}
	if _, err := parseSource(name, []byte(strings.ReplaceAll(out, "\r\n", "\n"))); err != nil {
		return nil, fmt.Errorf("the edit could not be applied to %s: %w", name, err)
	}
	return []byte(out), nil
}
