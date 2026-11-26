package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// FileField sets one field of a file with metadata in the form of the editor (S39c2a,
// E20a): a field of the file (e.g. description) or one level below (with.<name>,
// params.<NAME>). A nil value removes the field; Map sets a mapping instead (e.g.
// with.headers or with.auth).
type FileField struct {
	Field string     `json:"field"`
	Value any        `json:"value"`
	Map   []KeyValue `json:"map,omitempty"`
}

// formFields are the fields of each kind the form sets, in the order a new one is
// inserted: a missing field goes after the last of the fields before it that exists.
// Fields with "." take a name below them.
var formFields = map[string][]string{
	KindConnection:  {"name", "description", "type", "with."},
	KindTrigger:     {"name", "description", "type", "target", "environment", "params.", "enabled", "with."},
	KindEnvironment: {"name", "description", "variables.", "secretStore", "secrets."},
	KindProject:     {"name", "description", "defaultEnvironment", "variables.", "secretStore", "secrets.", "settings."},
}

// fixedName are the kinds whose name the form does not change: it is the file name
// (environments) or what other files refer to (connections, triggers).
var fixedName = map[string]bool{KindConnection: true, KindTrigger: true, KindEnvironment: true}

// EditFile sets a field in the text of a connection, trigger, environment or project
// file (formFields) and returns the new
// text. Like EditWorkflow, only the lines concerned change: comments, order and
// formatting of the rest stay. The file must be readable YAML with a mapping at the top.
//
// A new type of a trigger also removes the fields under with: the new type does not
// use; removed names them.
func EditFile(kind, name string, data []byte, f FileField) (out []byte, removed []string, err error) {
	order := formFields[kind]
	field, sub, nested := strings.Cut(f.Field, ".")
	if !nested && field == "name" && fixedName[kind] || !slices.Contains(order, field+map[bool]string{true: "."}[nested]) || nested && !configKeyPattern.MatchString(sub) {
		return nil, nil, fmt.Errorf("field %q cannot be set in the form", f.Field)
	}
	if f.Value == nil && len(f.Map) == 0 && (field == "type" || field == "name" || kind == KindTrigger && field == "target") {
		return nil, nil, fmt.Errorf("the %s of a %s cannot be removed", field, strings.ToLower(kind))
	}
	text := string(data)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	src, err := parseSource(name, []byte(text))
	if err != nil {
		return nil, nil, err
	}
	top := entries(src.body)
	if len(top) == 0 {
		return nil, nil, errors.New("the file has no fields; change it in the YAML view")
	}
	e := &editor{top: top, lines: strings.Split(strings.TrimSuffix(text, "\n"), "\n")}
	value := f.Value
	if len(f.Map) > 0 {
		value = inFileOrder(f.Map, entries(valueOf(entries(valueOf(top, field)), sub)))
	}
	if err := e.setField(top, e.insertAfter(order, field), "", f.Field, value); err != nil {
		return nil, nil, err
	}
	if kind == KindTrigger && field == "type" {
		if removed, err = e.removeUnused(fmt.Sprint(f.Value)); err != nil {
			return nil, nil, err
		}
	}
	res := strings.Join(applyLineEdits(e.lines, e.edits), eol)
	if strings.HasSuffix(text, "\n") || e.atEnd {
		res += eol
	}
	if _, err := parseSource(name, []byte(strings.ReplaceAll(res, "\r\n", "\n"))); err != nil {
		return nil, nil, fmt.Errorf("the edit could not be applied to %s: %w", name, err)
	}
	return []byte(res), removed, nil
}

// insertAfter returns the line after which a missing field goes: after the nearest
// existing field before it in order; the last field of the order (with:) and a field
// without one before it go to the end of the file.
func (e *editor) insertAfter(order []string, field string) int {
	i := slices.IndexFunc(order, func(k string) bool { return strings.TrimSuffix(k, ".") == field })
	after := len(e.lines)
	for j := i - 1; j >= 0 && i < len(order)-1; j-- {
		if k := keyOf(e.top, strings.TrimSuffix(order[j], ".")); k != nil {
			after = maxLine(k)
			break
		}
	}
	if after == len(e.lines) {
		e.atEnd = true
	}
	return after
}

// removeUnused removes the fields under with: a trigger of the given type does not use
// (TriggerFields), and with: itself when none is left.
func (e *editor) removeUnused(typ string) ([]string, error) {
	w := keyOf(e.top, "with")
	if w == nil {
		return nil, nil
	}
	var removed []string
	var keep int
	for _, c := range entries(w.Value) {
		if k := c.Key.GetToken().Value; slices.Contains(fieldsOf[typ], k) {
			keep++
		} else {
			removed = append(removed, k)
		}
	}
	switch {
	case len(removed) == 0:
		return nil, nil
	case keep == 0:
		return removed, e.setEntry(w, 0, "", "with", nil)
	}
	if m, ok := w.Value.(*ast.MappingNode); ok && m.IsFlowStyle {
		return nil, errors.New(`"with" is written in flow style ({ … }); write it as a block to change the type in the form`)
	}
	for _, k := range removed {
		e.remove(keyOf(entries(w.Value), k))
	}
	return removed, nil
}

// TriggerFields returns the fields under with: a trigger type uses, e.g. schedule,
// timezone and overlap for cron; nil for an unknown type.
func TriggerFields(typ string) []string { return slices.Clone(fieldsOf[typ]) }
