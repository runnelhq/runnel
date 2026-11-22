package model

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// EntryEdit changes the list of parameters (workflows and pipelines) or outputs
// (workflows) in the properties panel (roadmap E7c). Entries are addressed by their
// index, so that entries with an invalid or duplicate name can be changed as well.
type EntryEdit struct {
	// List is "parameters" or "outputs".
	List string `json:"list"`
	// Op is add (a new entry at the end), set (one field), delete or move.
	Op    string `json:"op"`
	Index int    `json:"index"`
	// Name is the name of a new entry (add).
	Name string `json:"name,omitempty"`
	// Field is the field to set: name, description, type, default, required or enum
	// for parameters; name, description, type or value for outputs.
	Field string `json:"field,omitempty"`
	// Value is a text, true/false (required) or a list of texts (enum); nil removes the field.
	Value any `json:"value,omitempty"`
	// By moves the entry up (-1) or down (1).
	By int `json:"by,omitempty"`
}

// KeyValue is an entry of a mapping set with SetField.Map, e.g. a parameter given to a
// sub-workflow (with.params).
type KeyValue struct {
	Key string `json:"key"`
	// Value is a text, or in the entries of a list (SetField.List) also a number or
	// true/false (E14), e.g. "descending: true".
	Value any `json:"value"`
}

var (
	parameterFields = []string{"name", "description", "type", "default", "required", "enum"}
	outputFields    = []string{"name", "description", "type", "value"}
	// listsBefore are the keys a new list is inserted before, in the order of the format.
	listsBefore = map[string][]string{
		"parameters": {"outputs", "settings", "actions", "transforms", "links", "layout"},
		"outputs":    {"settings", "actions", "links", "layout"},
	}
)

// docEntry is an entry of a list as read from the file, for rewriting it.
type docEntry struct {
	fields [][2]string // field and value, in the order of the format; values as written in YAML
}

// parameterEntry returns the fields of a parameter as they are written.
func parameterEntry(p Parameter) docEntry {
	var d docEntry
	d.add("name", p.Name, true)
	d.add("description", p.Description, false)
	d.add("type", string(p.Type), false)
	d.add("default", p.Default, false)
	if p.Required {
		d.fields = append(d.fields, [2]string{"required", "true"})
	}
	if len(p.Enum) > 0 {
		d.fields = append(d.fields, [2]string{"enum", flowList(p.Enum)})
	}
	return d
}

// outputEntry returns the fields of an output as they are written.
func outputEntry(o Output) docEntry {
	var d docEntry
	d.add("name", o.Name, true)
	d.add("description", o.Description, false)
	d.add("type", string(o.Type), false)
	d.add("value", o.Value, true)
	return d
}

func (d *docEntry) add(field, value string, always bool) {
	if value != "" || always {
		d.fields = append(d.fields, [2]string{field, flowScalar(value)})
	}
}

// flowValue writes a text, a number or true/false for a flow mapping ({ … }).
func flowValue(v any) string {
	switch v := v.(type) {
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return coord(v)
	case int:
		return strconv.Itoa(v)
	case string:
		return flowScalar(v)
	}
	return flowScalar(fmt.Sprint(v))
}

// flowScalar writes a text so that it can stand in a flow mapping or list ({ … }, [ … ]).
func flowScalar(s string) string {
	if strings.ContainsAny(s, ",[]{}") {
		return strconv.Quote(s)
	}
	return yamlScalar(s)
}

func flowList(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = flowScalar(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// checkEntries checks the entry edits against the lists of the file.
func (e *editor) checkEntries() error {
	if len(e.edit.Entries) > 1 {
		return errors.New("only one parameter or output can be changed at a time")
	}
	for _, en := range e.edit.Entries {
		n, ok := e.d.lists[en.List]
		if !ok {
			return fmt.Errorf("the %s has no list %q", e.d.docNoun, en.List)
		}
		noun := strings.TrimSuffix(en.List, "s")
		switch en.Op {
		case "add":
			if strings.TrimSpace(en.Name) == "" {
				return fmt.Errorf("cannot add %s: the name is empty", noun)
			}
			continue
		case "set", "delete", "move":
		default:
			return fmt.Errorf("unknown change %q of the %s", en.Op, en.List)
		}
		if en.Index < 0 || en.Index >= n {
			return fmt.Errorf("cannot change %s #%d: it does not exist", noun, en.Index+1)
		}
		switch en.Op {
		case "move":
			if to := en.Index + en.By; (en.By != -1 && en.By != 1) || to < 0 || to >= n {
				return fmt.Errorf("cannot move %s #%d there", noun, en.Index+1)
			}
		case "set":
			fields := parameterFields
			if en.List == "outputs" {
				fields = outputFields
			}
			if !slices.Contains(fields, en.Field) {
				return fmt.Errorf("field %q of a %s cannot be changed here", en.Field, noun)
			}
			if _, err := entryValue(en); err != nil {
				return fmt.Errorf("cannot change %s #%d: %w", noun, en.Index+1, err)
			}
		}
	}
	return nil
}

// entryValue returns the value to write for a set: a text, true (required) or the
// texts of enum; nil removes the field. Name and value of an output cannot be removed.
func entryValue(en EntryEdit) (any, error) {
	switch en.Field {
	case "required":
		switch v := en.Value.(type) {
		case nil:
			return nil, nil
		case bool:
			if !v {
				return nil, nil // false is the default
			}
			return true, nil
		}
		return nil, errors.New("required: expected true or false")
	case "enum":
		list, ok := en.Value.([]any)
		if en.Value == nil || ok && len(list) == 0 {
			return nil, nil
		}
		if !ok {
			return nil, errors.New("enum: expected a list of texts")
		}
		values := make([]string, len(list))
		for i, v := range list {
			s, ok := v.(string)
			if !ok {
				return nil, errors.New("enum: expected a list of texts")
			}
			values[i] = s
		}
		return values, nil
	}
	switch v := en.Value.(type) {
	case nil:
		if en.Field == "name" || en.Field == "value" {
			return nil, fmt.Errorf("the field %q is required", en.Field)
		}
		return nil, nil
	case string:
		if strings.ContainsAny(v, "\r\n") && en.Field != "description" && en.Field != "value" {
			return nil, fmt.Errorf("%s: the text must be on one line", en.Field)
		}
		if v == "" && en.Field != "name" && en.Field != "value" {
			return nil, nil
		}
		return v, nil
	}
	return nil, fmt.Errorf("%s: expected a text", en.Field)
}

// applyEntries applies the entry edits.
func (e *editor) applyEntries() error {
	for _, en := range e.edit.Entries {
		noun := strings.TrimSuffix(en.List, "s")
		if err := e.applyEntry(en); err != nil {
			return fmt.Errorf("cannot change the %s: %w", noun, err)
		}
	}
	return nil
}

func (e *editor) applyEntry(en EntryEdit) error {
	key := keyOf(e.top, en.List)
	var seq *ast.SequenceNode
	if key != nil {
		seq, _ = key.Value.(*ast.SequenceNode)
	}
	if seq != nil && seq.IsFlowStyle && len(seq.Values) > 0 {
		return fmt.Errorf("the list %q is written in flow style ([…]); write it as a block list to edit it in the properties", en.List)
	}
	var items []ast.Node
	if seq != nil {
		items = seq.Values
	}
	switch en.Op {
	case "add":
		item := []string{"name: " + yamlScalar(en.Name)}
		if en.List == "outputs" {
			item = append(item, `value: ""`)
		}
		// A list of one-line items ({ name: … }) gets one more of them.
		if len(items) > 0 && oneLineFlow(items[len(items)-1]) {
			item = []string{"{ " + strings.Join(item, ", ") + " }"}
		}
		return e.appendItems(en.List, [][]string{item}, listsBefore[en.List]...)
	case "delete":
		if len(items) == 1 {
			e.remove(key) // no empty "parameters:" is left behind
			return nil
		}
		return e.removeItem(items[en.Index], strings.TrimSuffix(en.List, "s"))
	case "move":
		a, b := items[en.Index], items[en.Index+en.By]
		for _, item := range []ast.Node{a, b} {
			line := e.lines[minLine(item)-1]
			if !strings.HasPrefix(line[len(leadingSpace(line)):], "- ") {
				return errors.New("the entry shares a line with others; change the order in your text editor")
			}
		}
		e.replace(a, e.lines[minLine(b)-1:maxLine(b)]...)
		e.replace(b, e.lines[minLine(a)-1:maxLine(a)]...)
		return nil
	}

	// set: a block entry is changed in place, a one-line entry ({ … }) is written anew.
	item := items[en.Index]
	value, _ := entryValue(en)
	if oneLineFlow(item) {
		return e.rewriteEntry(item, en, value)
	}
	es := entries(item)
	if len(es) == 0 {
		return errors.New("the entry cannot be changed in the properties")
	}
	if m, ok := item.(*ast.MappingNode); ok && m.IsFlowStyle {
		return errors.New("the entry is written in flow style over several lines; write it as a block to edit it in the properties")
	}
	if list, ok := value.([]string); ok {
		value = rawValue(flowList(list))
	}
	indent := strings.Repeat(" ", es[0].Key.GetToken().Position.Column-1)
	return e.setField(es, maxLine(item), indent, en.Field, value)
}

// rawValue is a value written as it is (e.g. a flow list).
type rawValue string

// oneLineFlow reports whether a list item is a flow mapping on its own line: - { … }.
func oneLineFlow(item ast.Node) bool {
	m, ok := item.(*ast.MappingNode)
	return ok && m.IsFlowStyle && minLine(item) == maxLine(item)
}

// rewriteEntry writes a one-line entry anew with the changed field, keeping its comment.
func (e *editor) rewriteEntry(item ast.Node, en EntryEdit, value any) error {
	var d docEntry
	if en.List == "outputs" {
		d = outputEntry(e.d.outputs[en.Index])
	} else {
		d = parameterEntry(e.d.params[en.Index])
	}
	var written string
	switch v := value.(type) {
	case string:
		written = flowScalar(v)
	case bool:
		written = "true"
	case []string:
		written = flowList(v)
	}
	found := false
	var fields [][2]string
	for _, f := range d.fields {
		if f[0] == en.Field {
			found = true
			if value == nil {
				continue
			}
			f[1] = written
		}
		fields = append(fields, f)
	}
	if !found && value != nil {
		fields = append(fields, [2]string{en.Field, written})
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f[0] + ": " + f[1]
	}
	line := e.lines[minLine(item)-1]
	indent := leadingSpace(line)
	if !strings.HasPrefix(line[len(indent):], "- ") {
		return errors.New("the entry shares a line with others; change it in your text editor")
	}
	comment := ""
	if i := strings.LastIndex(line, "}"); i >= 0 && strings.HasPrefix(strings.TrimSpace(line[i+1:]), "#") {
		comment = line[i+1:]
	}
	e.replace(item, indent+"- { "+strings.Join(parts, ", ")+" }"+comment)
	return nil
}
