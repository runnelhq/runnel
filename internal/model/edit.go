package model

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// WorkflowEdit describes changes made on the editor's canvas (roadmap E6a, E6b).
type WorkflowEdit struct {
	// Move sets the canvas position of actions (layout block, pixels, top-left corner).
	Move map[string]Point `json:"move,omitempty"`
	// Delete removes actions together with their links and layout entries.
	Delete []string `json:"delete,omitempty"`
	// Add appends new actions (without configuration); Move can give their positions.
	Add []NewAction `json:"add,omitempty"`
	// Rename changes action ids (old → new) in the action, its links and the layout.
	// It cannot be combined with other changes.
	Rename map[string]string `json:"rename,omitempty"`
	// AddLinks appends links.
	AddLinks []Link `json:"addLinks,omitempty"`
	// DeleteLinks removes links by their index in the list of links.
	DeleteLinks []int `json:"deleteLinks,omitempty"`
	// UpdateLinks changes the condition of links.
	UpdateLinks []LinkUpdate `json:"updateLinks,omitempty"`
	// Set changes fields of actions (properties panel, E7a); at most one per action.
	Set []SetField `json:"set,omitempty"`
	// SetWorkflow changes a field of the workflow itself (E7b); at most one.
	SetWorkflow []WorkflowField `json:"setWorkflow,omitempty"`
	// Entries changes a parameter or an output (E7c); at most one.
	Entries []EntryEdit `json:"entries,omitempty"`
}

// SetField sets a field of an action to a value; a nil value removes the field.
type SetField struct {
	ID string `json:"id"`
	// Field is one of description, timeout, continueOnError, join, joinTimeout,
	// retry.attempts, retry.backoff, retry.strategy, forEach, parallel, stopOnError or
	// with.<name>.
	Field string `json:"field"`
	// Value is a text, a number or true/false.
	Value any `json:"value"`
	// Map sets a field of the configuration to a mapping of texts instead (e.g.
	// with.params), in this order; Value must then be nil.
	Map []KeyValue `json:"map,omitempty"`
	// List sets a field of the configuration to a list of mappings instead, each written
	// on one line, e.g. with.fields as "- { name: kdnr, type: int }" (S46); Value and
	// Map must then be nil.
	List [][]KeyValue `json:"list,omitempty"`
	// Texts sets a field of the configuration to a list of texts, written as
	// "[a, b]", e.g. groupBy of core.group-by (E14); Value, Map and List must then be nil.
	Texts []string `json:"texts,omitempty"`
}

var (
	settableFields = map[string]bool{
		"description": true, "timeout": true, "continueOnError": true, "join": true, "joinTimeout": true,
		"retry.attempts": true, "retry.backoff": true, "retry.strategy": true,
		"forEach": true, "parallel": true, "stopOnError": true,
	}
	configKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

// NewAction is an action added on the canvas.
type NewAction struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// LinkUpdate changes the link with the given index; fields that are not given stay as they are.
type LinkUpdate struct {
	Index int `json:"index"`
	// When is "success", "failure", "always" or an expression "{{ … }}"; "success" removes the field.
	When *string `json:"when,omitempty"`
	// Loop marks a link back to an earlier action.
	Loop *bool `json:"loop,omitempty"`
	// Disabled keeps the link in the file but never follows it.
	Disabled *bool `json:"disabled,omitempty"`
}

// WorkflowField sets a field of the workflow itself; a nil value removes it.
type WorkflowField struct {
	// Field is one of name, description, settings.timeout or settings.maxConcurrentRuns.
	Field string `json:"field"`
	Value any    `json:"value"`
}

// EditWorkflow applies an edit to the text of a workflow file and returns the new text.
// Only the lines concerned are changed: comments, order, quoting and formatting of
// everything else stay as they are, so that a saved file gives a small Git diff.
// The workflow must be readable (no syntax errors, wrong types or unknown fields).
func EditWorkflow(name string, data []byte, edit WorkflowEdit) ([]byte, error) {
	return editText(name, data, func(text []byte) (*graphDoc, *graphEdit, error) {
		d, w, err := workflowDoc(name, text)
		if d == nil {
			return nil, nil, err
		}
		g := &graphEdit{Move: edit.Move, Delete: edit.Delete, Add: edit.Add, Rename: edit.Rename,
			DeleteLinks: edit.DeleteLinks, Set: edit.Set, SetDoc: edit.SetWorkflow, Entries: edit.Entries}
		for _, l := range edit.AddLinks {
			if err := checkWhen(l.When); err != nil {
				return nil, nil, fmt.Errorf("cannot add link from %q to %q: %w", l.From, l.To, err)
			}
			g.AddLinks = append(g.AddLinks, workflowLinkItem(l))
		}
		if len(edit.UpdateLinks) > 0 {
			g.UpdateLinks = map[int]linkItem{}
		}
		changed := map[int]Link{} // several updates of one link add up
		for _, u := range edit.UpdateLinks {
			if u.Index < 0 || u.Index >= len(w.Links) {
				return nil, nil, fmt.Errorf("cannot change link #%d: it does not exist", u.Index+1)
			}
			l, ok := changed[u.Index]
			if !ok {
				l = w.Links[u.Index]
			}
			if u.When != nil {
				if err := checkWhen(*u.When); err != nil {
					return nil, nil, fmt.Errorf("cannot change link #%d: %w", u.Index+1, err)
				}
				l.When = *u.When
			}
			if u.Loop != nil {
				l.Loop = *u.Loop
			}
			if u.Disabled != nil {
				l.Disabled = *u.Disabled
			}
			changed[u.Index] = l
			g.UpdateLinks[u.Index] = workflowLinkItem(l)
		}
		return d, g, nil
	})
}

// workflowDoc reads a workflow for the line editor. Field errors (e.g. an invalid
// duration) do not prevent editing; they are fixed by editing.
func workflowDoc(name string, text []byte) (*graphDoc, *Workflow, error) {
	w, err := ReadWorkflow(name, text)
	if w == nil {
		return nil, nil, err
	}
	d := &graphDoc{noun: "action", docNoun: "workflow", stepsKey: "actions", src: w.src,
		stepFields: settableFields, docFields: workflowFields,
		lists:  map[string]int{"parameters": len(w.Parameters), "outputs": len(w.Outputs)},
		params: w.Parameters, outputs: w.Outputs}
	for _, a := range w.Actions {
		d.steps = append(d.steps, a.ID)
	}
	for _, l := range w.Links {
		d.links = append(d.links, linkRef{l.From, l.To})
	}
	return d, w, nil
}

// workflowLinkItem writes a workflow link; "success" is the default and left out.
func workflowLinkItem(l Link) linkItem {
	item := linkItem{From: l.From, To: l.To}
	if l.When != "" && l.When != "success" {
		item.Fields = append(item.Fields, [2]string{"when", yamlScalar(l.When)})
	}
	if l.Loop {
		item.Fields = append(item.Fields, [2]string{"loop", "true"})
	}
	if l.Disabled {
		item.Fields = append(item.Fields, [2]string{"disabled", "true"})
	}
	return item
}

// graphDoc is what the line editor needs to know of a workflow or a pipeline: its
// steps (actions or transforms) and links in the order of the file.
type graphDoc struct {
	noun     string // "action" or "transform", for messages
	docNoun  string // "workflow" or "pipeline"
	stepsKey string // "actions" or "transforms"
	steps    []string
	links    []linkRef
	src      *source
	// stepFields are the fields of a step that Set may change besides with.*; docFields
	// those of the document that SetDoc may change.
	stepFields, docFields map[string]bool
	// lists are the lists Entries may change, with their number of entries; params and
	// outputs are their entries as read.
	lists   map[string]int
	params  []Parameter
	outputs []Output
}

type linkRef struct{ From, To string }

// rawStep is a step as written: its id and its lines without "- " and without the
// indentation of the list.
type rawStep struct {
	ID    string
	Lines []string
}

// linkItem is a link as written: from, to and further fields with their YAML values.
type linkItem struct {
	From, To string
	Fields   [][2]string
}

// graphEdit is an edit of a workflow or pipeline, with links already in written form.
type graphEdit struct {
	Move   map[string]Point
	Delete []string
	Add    []NewAction
	// Insert appends steps as written (pasted, E16).
	Insert      []rawStep
	Rename      map[string]string
	AddLinks    []linkItem
	DeleteLinks []int
	// UpdateLinks are the links after the change, by index.
	UpdateLinks map[int]linkItem
	Set         []SetField
	SetDoc      []WorkflowField
	Entries     []EntryEdit
}

// editText applies an edit to the text of a file: read parses the text into the steps
// and links of the file and translates the edit.
func editText(name string, data []byte, read func([]byte) (*graphDoc, *graphEdit, error)) ([]byte, error) {
	text := string(data)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	// Line numbers of the parser refer to LF line endings; CRLF is restored when joining.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	d, edit, err := read([]byte(text))
	if err != nil {
		return nil, err
	}
	finalNewline := strings.HasSuffix(text, "\n")
	e := &editor{
		d:     d,
		edit:  *edit,
		top:   entries(d.src.body),
		lines: strings.Split(strings.TrimSuffix(text, "\n"), "\n"),
	}
	if err := e.check(); err != nil {
		return nil, err
	}
	if len(edit.Rename) > 0 {
		err = e.rename()
	} else {
		err = e.apply()
	}
	if err != nil {
		return nil, err
	}

	lines := applyLineEdits(e.lines, e.edits)
	out := strings.Join(lines, eol)
	if finalNewline || e.atEnd {
		out += eol
	}
	if _, err := parseSource(name, []byte(strings.ReplaceAll(out, "\r\n", "\n"))); err != nil {
		return nil, fmt.Errorf("the edit could not be applied to %s: %w", name, err)
	}
	return []byte(out), nil
}

// editor collects the line edits for one edit.
type editor struct {
	d     *graphDoc
	edit  graphEdit
	top   []*ast.MappingValueNode
	lines []string
	edits []lineEdit
	// atEnd is set when lines are appended at the end of the file.
	atEnd bool
	// ids are the steps after the edit (existing minus deleted plus added).
	ids     map[string]bool
	deleted map[string]bool
}

func (e *editor) check() error {
	edit, noun := e.edit, e.d.noun
	if len(edit.Rename) > 0 && (len(edit.Move) > 0 || len(edit.Delete) > 0 || len(edit.Add) > 0 || len(edit.Insert) > 0 ||
		len(edit.AddLinks) > 0 || len(edit.DeleteLinks) > 0 || len(edit.UpdateLinks) > 0 || len(edit.Set) > 0 || len(edit.SetDoc) > 0 ||
		len(edit.Entries) > 0) {
		return errors.New("renaming cannot be combined with other changes")
	}
	e.ids = map[string]bool{}
	for _, id := range e.d.steps {
		e.ids[id] = true
	}
	e.deleted = map[string]bool{}
	for _, id := range edit.Delete {
		if !e.ids[id] {
			return fmt.Errorf("cannot delete %s %q: it does not exist", noun, id)
		}
		e.deleted[id] = true
	}
	for id := range e.deleted {
		delete(e.ids, id)
	}
	for _, a := range edit.Add {
		if err := checkID(noun, a.ID); err != nil {
			return fmt.Errorf("cannot add %s: %w", noun, err)
		}
		if e.ids[a.ID] || e.deleted[a.ID] {
			return fmt.Errorf("cannot add %s %q: the id is already used", noun, a.ID)
		}
		if !actionTypePattern.MatchString(a.Type) {
			return fmt.Errorf("cannot add %s %q: invalid %s type %q", noun, a.ID, noun, a.Type)
		}
		e.ids[a.ID] = true
	}
	for _, r := range edit.Insert {
		if err := checkID(noun, r.ID); err != nil {
			return fmt.Errorf("cannot add %s: %w", noun, err)
		}
		if e.ids[r.ID] || e.deleted[r.ID] {
			return fmt.Errorf("cannot add %s %q: the id is already used", noun, r.ID)
		}
		e.ids[r.ID] = true
	}
	for id := range edit.Move {
		if !e.ids[id] {
			return fmt.Errorf("cannot move %s %q: it does not exist", noun, id)
		}
	}
	for old, id := range edit.Rename {
		if !e.ids[old] {
			return fmt.Errorf("cannot rename %s %q: it does not exist", noun, old)
		}
		if err := checkID(noun, id); err != nil {
			return fmt.Errorf("cannot rename %s %q: %w", noun, old, err)
		}
		if e.ids[id] {
			return fmt.Errorf("cannot rename %s %q to %q: the id is already used", noun, old, id)
		}
	}
	for _, l := range edit.AddLinks {
		for _, id := range []string{l.From, l.To} {
			if !e.ids[id] {
				return fmt.Errorf("cannot add link from %q to %q: %s %q does not exist", l.From, l.To, noun, id)
			}
		}
	}
	existing := map[string]bool{}
	for _, id := range e.d.steps {
		existing[id] = !e.deleted[id]
	}
	set := map[string]bool{}
	for _, f := range edit.Set {
		if !existing[f.ID] {
			return fmt.Errorf("cannot change %s %q: it does not exist", noun, f.ID)
		}
		if set[f.ID] {
			return fmt.Errorf("cannot change %s %q: only one field per %s can be changed at a time", noun, f.ID, noun)
		}
		set[f.ID] = true
		if err := checkSetField(f, e.d.stepFields); err != nil {
			return fmt.Errorf("cannot change %s %q: %w", noun, f.ID, err)
		}
	}
	links := map[int]bool{}
	for _, i := range edit.DeleteLinks {
		if i < 0 || i >= len(e.d.links) {
			return fmt.Errorf("cannot delete link #%d: it does not exist", i+1)
		}
		links[i] = true
	}
	for i := range edit.UpdateLinks {
		if i < 0 || i >= len(e.d.links) || links[i] {
			return fmt.Errorf("cannot change link #%d: it does not exist", i+1)
		}
	}
	if len(edit.SetDoc) > 1 {
		return fmt.Errorf("only one field of the %s can be changed at a time", e.d.docNoun)
	}
	for _, f := range edit.SetDoc {
		if !e.d.docFields[f.Field] {
			return fmt.Errorf("field %q of the %s cannot be changed here", f.Field, e.d.docNoun)
		}
		if err := checkSetField(SetField{Field: "with.x", Value: f.Value}, nil); err != nil {
			return fmt.Errorf("%s: only a text, a number or true/false can be set here", f.Field)
		}
	}
	return e.checkEntries()
}

var workflowFields = map[string]bool{"name": true, "description": true, "settings.timeout": true, "settings.maxConcurrentRuns": true}

func checkID(noun, id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid %s id %q (letters, digits, - and _, starting with a letter)", noun, id)
	}
	return nil
}

func checkSetField(f SetField, fields map[string]bool) error {
	name, ok := strings.CutPrefix(f.Field, "with.")
	if (!ok || !configKeyPattern.MatchString(name)) && !fields[f.Field] {
		return fmt.Errorf("field %q cannot be changed here", f.Field)
	}
	if len(f.Texts) > 0 {
		if !ok || f.Value != nil || len(f.Map) > 0 || len(f.List) > 0 {
			return fmt.Errorf("%s: a list of texts can only be set for a field of the configuration", f.Field)
		}
		return nil
	}
	if len(f.List) > 0 {
		if !ok || f.Value != nil || len(f.Map) > 0 {
			return fmt.Errorf("%s: a list can only be set for a field of the configuration", f.Field)
		}
		for _, item := range f.List {
			seen := map[string]bool{}
			for _, kv := range item {
				if !configKeyPattern.MatchString(kv.Key) || seen[kv.Key] {
					return fmt.Errorf("%s: invalid or duplicate key %q", f.Field, kv.Key)
				}
				seen[kv.Key] = true
				switch kv.Value.(type) {
				case string, bool, float64, int:
				default:
					return fmt.Errorf("%s: %s: only a text, a number or true/false can be set here", f.Field, kv.Key)
				}
			}
		}
		return nil
	}
	if len(f.Map) > 0 {
		if !ok || f.Value != nil {
			return fmt.Errorf("%s: a mapping can only be set for a field of the configuration", f.Field)
		}
		seen := map[string]bool{}
		for _, kv := range f.Map {
			if !configKeyPattern.MatchString(kv.Key) || seen[kv.Key] {
				return fmt.Errorf("%s: invalid or duplicate key %q", f.Field, kv.Key)
			}
			seen[kv.Key] = true
			if _, text := kv.Value.(string); !text {
				return fmt.Errorf("%s: %s: the values of a mapping are texts", f.Field, kv.Key)
			}
		}
		return nil
	}
	switch v := f.Value.(type) {
	case nil, string, bool, float64, int:
	default:
		return fmt.Errorf("%s: only a text, a number or true/false can be set here, not %T", f.Field, v)
	}
	return nil
}

func checkWhen(when string) error {
	switch when {
	case "", "success", "failure", "always":
		return nil
	}
	// Expressions are checked like any other value of the file after the change.
	if t := strings.TrimSpace(when); strings.HasPrefix(t, "{{") && strings.HasSuffix(t, "}}") && !strings.ContainsAny(t, "\n\r") {
		return nil
	}
	return fmt.Errorf(`condition %q is not supported here (success, failure, always or an expression "{{ … }}")`, when)
}

// apply handles all changes except renaming.
func (e *editor) apply() error {
	edit, d := e.edit, e.d
	steps := valueOf(e.top, d.stepsKey)
	linksNode := valueOf(e.top, "links")

	// Steps: deleted ones are removed, new ones appended.
	if seq, ok := steps.(*ast.SequenceNode); ok && len(e.deleted) > 0 {
		for i, item := range seq.Values {
			if i < len(d.steps) && e.deleted[d.steps[i]] {
				if err := e.removeItem(item, d.noun); err != nil {
					return err
				}
			}
		}
	}
	if len(edit.Add) > 0 || len(edit.Insert) > 0 {
		var items [][]string
		for _, a := range edit.Add {
			items = append(items, []string{"id: " + a.ID, "type: " + a.Type})
		}
		for _, r := range edit.Insert {
			items = append(items, r.Lines)
		}
		if err := e.appendItems(d.stepsKey, items, "links", "layout"); err != nil {
			return err
		}
	}

	// Links: removed with their steps or on their own, changed, appended.
	removed := map[int]bool{}
	for _, i := range edit.DeleteLinks {
		removed[i] = true
	}
	if seq, ok := linksNode.(*ast.SequenceNode); ok {
		for i, item := range seq.Values {
			if i >= len(d.links) {
				break
			}
			l := d.links[i]
			if removed[i] || e.deleted[l.From] || e.deleted[l.To] {
				if err := e.removeItem(item, "link"); err != nil {
					return err
				}
				removed[i] = true
			}
		}
		for _, i := range slices.Sorted(maps.Keys(edit.UpdateLinks)) {
			if removed[i] || i >= len(seq.Values) {
				continue
			}
			if err := e.rewriteLink(seq.Values[i], edit.UpdateLinks[i]); err != nil {
				return err
			}
		}
	}
	if len(edit.AddLinks) > 0 {
		var items [][]string
		for _, l := range edit.AddLinks {
			items = append(items, []string{flowLink(l)})
		}
		if err := e.appendItems("links", items, "layout"); err != nil {
			return err
		}
	}

	for _, f := range edit.Set {
		if err := e.set(f); err != nil {
			return fmt.Errorf("cannot change %s %q: %w", d.noun, f.ID, err)
		}
	}
	for _, f := range edit.SetDoc {
		if err := e.setDoc(f); err != nil {
			return fmt.Errorf("cannot change the %s: %w", d.docNoun, err)
		}
	}
	if err := e.applyEntries(); err != nil {
		return err
	}

	e.layout()
	return nil
}

// set changes one field of a step in place, adds it or removes it.
func (e *editor) set(f SetField) error {
	items := seqValues(valueOf(e.top, e.d.stepsKey))
	i := slices.Index(e.d.steps, f.ID)
	if i < 0 || i >= len(items) {
		return fmt.Errorf("the %s was not found in the file", e.d.noun)
	}
	item := items[i]
	itemEntries := entries(item)
	if m, ok := item.(*ast.MappingNode); ok && m.IsFlowStyle || len(itemEntries) == 0 {
		return fmt.Errorf("the %s is written in flow style ({ … }); write it as a block to edit it in the properties", e.d.noun)
	}
	indent := strings.Repeat(" ", itemEntries[0].Key.GetToken().Position.Column-1)
	value := f.Value
	if len(f.List) > 0 {
		value = mappingList(f.List)
	}
	if len(f.Texts) > 0 {
		value = rawValue(flowList(f.Texts))
	}
	if len(f.Map) > 0 {
		value = inFileOrder(f.Map, entries(valueOf(entries(valueOf(itemEntries, "with")), strings.TrimPrefix(f.Field, "with."))))
	}
	return e.setField(itemEntries, maxLine(item), indent, f.Field, value)
}

// inFileOrder orders the entries of a mapping like the keys already in the file (the
// editor gets mappings with sorted keys); new keys follow in the order given.
func inFileOrder(m []KeyValue, existing []*ast.MappingValueNode) []KeyValue {
	out := make([]KeyValue, 0, len(m))
	used := map[string]bool{}
	for _, e := range existing {
		key := e.Key.GetToken().Value
		for _, kv := range m {
			if kv.Key == key && !used[key] {
				out = append(out, kv)
				used[key] = true
			}
		}
	}
	for _, kv := range m {
		if !used[kv.Key] {
			out = append(out, kv)
			used[kv.Key] = true
		}
	}
	return out
}

// setField sets "field" or "parent.child" in a block mapping with the given entries and
// indentation; a missing entry is inserted after the line after.
func (e *editor) setField(es []*ast.MappingValueNode, after int, indent, field string, value any) error {
	parent, child, nested := strings.Cut(field, ".")
	if !nested {
		return e.setEntry(keyOf(es, parent), after, indent, parent, value)
	}

	pe := keyOf(es, parent)
	if pe == nil {
		if value == nil {
			return nil
		}
		lines, err := entryLines(indent+"  ", child, value)
		if err != nil {
			return err
		}
		e.insert(after+1, append([]string{indent + parent + ":"}, lines...)...)
		return nil
	}
	children := entries(pe.Value)
	if m, ok := pe.Value.(*ast.MappingNode); ok && m.IsFlowStyle && len(children) > 0 {
		return fmt.Errorf("%q is written in flow style ({ … }); write it as a block to edit it in the properties", parent)
	}
	if len(children) == 0 {
		// "with:" without entries (or "with: {}"): replace the line.
		if value == nil {
			return nil
		}
		line := minLine(pe)
		if maxLine(pe) != line {
			return fmt.Errorf("%q cannot be edited in the properties", parent)
		}
		prefix, err := e.prefix(pe)
		if err != nil {
			return err
		}
		lines, err := entryLines(indent+"  ", child, value)
		if err != nil {
			return err
		}
		e.edits = append(e.edits, lineEdit{start: line, end: line, lines: append([]string{prefix + parent + ":"}, lines...)})
		return nil
	}
	ce := keyOf(children, child)
	if value == nil && ce != nil && len(children) == 1 {
		return e.setEntry(pe, 0, "", parent, nil) // the last entry: remove the parent as well
	}
	childIndent := strings.Repeat(" ", children[0].Key.GetToken().Position.Column-1)
	return e.setEntry(ce, maxLine(pe.Value), childIndent, child, value)
}

// setDoc sets a field of the workflow or pipeline itself. A missing description is
// added after the name, missing settings before the steps.
func (e *editor) setDoc(f WorkflowField) error {
	after := len(e.lines)
	switch {
	case f.Field == "description" && keyOf(e.top, "name") != nil:
		after = maxLine(keyOf(e.top, "name"))
	default:
		for _, k := range []string{e.d.stepsKey, "links", "layout"} {
			if next := keyOf(e.top, k); next != nil {
				after = minLine(next) - 1
				break
			}
		}
	}
	if after == len(e.lines) {
		e.atEnd = true
	}
	return e.setField(e.top, after, "", f.Field, f.Value)
}

// setEntry replaces, removes (nil value) or – if entry is nil – inserts "key: value" after the given line.
func (e *editor) setEntry(entry *ast.MappingValueNode, after int, indent, key string, value any) error {
	if entry == nil {
		if value == nil {
			return nil
		}
		lines, err := entryLines(indent, key, value)
		if err != nil {
			return err
		}
		e.insert(after+1, lines...)
		return nil
	}
	prefix, err := e.prefix(entry)
	if err != nil {
		return err
	}
	if value == nil {
		if strings.Contains(prefix, "-") {
			return fmt.Errorf("%q is the first field and cannot be removed here", key)
		}
		e.remove(entry)
		return nil
	}
	lines, err := entryLines(strings.Repeat(" ", len(prefix)), key, value)
	if err != nil {
		return err
	}
	lines[0] = prefix + strings.TrimLeft(lines[0], " ")
	if len(lines) == 1 {
		lines[0] += e.trailingComment(entry)
	}
	e.replace(entry, lines...)
	return nil
}

// trailingComment returns the comment after a one-line scalar entry ("   # short"), or "".
func (e *editor) trailingComment(entry *ast.MappingValueNode) string {
	t := entry.Value.GetToken()
	if t == nil || t.Position == nil || minLine(entry) != maxLine(entry) {
		return ""
	}
	line := []rune(e.lines[t.Position.Line-1])
	at := t.Position.Column - 1
	if at < 0 || at >= len(line) {
		return ""
	}
	end := at + len([]rune(t.Value))
	if q := line[at]; q == '"' || q == '\'' {
		end = at + 1
		for end < len(line) && (line[end] != q || (q == '"' && line[end-1] == '\\')) {
			end++
		}
		end++
	}
	if end > len(line) {
		return ""
	}
	if rest := string(line[end:]); strings.HasPrefix(strings.TrimSpace(rest), "#") {
		return rest
	}
	return ""
}

// prefix returns the text before the key of an entry: its indentation, maybe with "- ".
func (e *editor) prefix(entry *ast.MappingValueNode) (string, error) {
	pos := entry.Key.GetToken().Position
	line := []rune(e.lines[pos.Line-1])
	if pos.Column-1 > len(line) || strings.Trim(string(line[:pos.Column-1]), " -") != "" {
		return "", errors.New("the field shares its line with others; change it in the YAML file")
	}
	return string(line[:pos.Column-1]), nil
}

// entryLines writes "key: value" with the given indentation; multi-line texts become a literal block.
func entryLines(indent, key string, value any) ([]string, error) {
	switch v := value.(type) {
	case bool:
		return []string{fmt.Sprintf("%s%s: %t", indent, key, v)}, nil
	case float64:
		return []string{indent + key + ": " + coord(v)}, nil
	case int:
		return []string{fmt.Sprintf("%s%s: %d", indent, key, v)}, nil
	case string:
		if block, ok := literalBlock(indent, v); ok {
			return append([]string{indent + key + ": " + block[0]}, block[1:]...), nil
		}
		return []string{indent + key + ": " + yamlScalar(v)}, nil
	case rawValue:
		return []string{indent + key + ": " + string(v)}, nil
	case mappingList:
		lines := []string{indent + key + ":"}
		for _, item := range v {
			parts := make([]string, len(item))
			for i, kv := range item {
				parts[i] = kv.Key + ": " + flowValue(kv.Value)
			}
			lines = append(lines, indent+"  - { "+strings.Join(parts, ", ")+" }")
		}
		return lines, nil
	case []KeyValue:
		lines := []string{indent + key + ":"}
		for _, kv := range v {
			entry, err := entryLines(indent+"  ", kv.Key, kv.Value)
			if err != nil {
				return nil, err
			}
			lines = append(lines, entry...)
		}
		return lines, nil
	}
	return nil, fmt.Errorf("unsupported value %T", value)
}

// mappingList is a list of mappings, written one per line in flow style.
type mappingList [][]KeyValue

// yamlScalar writes a text as plain YAML scalar if it reads back unchanged, otherwise double-quoted.
func yamlScalar(s string) string {
	if s != "" && s == strings.TrimSpace(s) && !strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@`") &&
		!strings.Contains(s, ": ") && !strings.Contains(s, " #") && !strings.HasSuffix(s, ":") {
		var v map[string]any
		if err := yaml.Unmarshal([]byte("v: "+s), &v); err == nil && v["v"] == s {
			return s
		}
	}
	return strconv.Quote(s)
}

// literalBlock writes a multi-line text as "|" block; ok is false for texts it cannot hold exactly.
func literalBlock(indent, s string) ([]string, bool) {
	if !strings.Contains(s, "\n") {
		return nil, false
	}
	head, body := "|-", s
	if strings.HasSuffix(s, "\n") {
		head, body = "|", strings.TrimSuffix(s, "\n")
	}
	if strings.HasSuffix(body, "\n") || strings.HasPrefix(body, " ") || strings.ContainsAny(body, "\t\r") {
		return nil, false
	}
	lines := []string{head}
	for _, l := range strings.Split(body, "\n") {
		if l == "" {
			lines = append(lines, "")
		} else {
			lines = append(lines, indent+"  "+l)
		}
	}
	return lines, true
}

// layout removes the entries of deleted actions and sets the moved ones.
func (e *editor) layout() {
	layoutKey := keyOf(e.top, "layout")
	layoutEntries := entries(valueOf(e.top, "layout"))
	indent := "  "
	if len(layoutEntries) > 0 {
		indent = strings.Repeat(" ", max(0, layoutEntries[0].Key.GetToken().Position.Column-1))
	}
	layoutLine := func(id string, p Point) string {
		return fmt.Sprintf("%s%s: { x: %s, y: %s }", indent, id, coord(p.X), coord(p.Y))
	}
	present := map[string]bool{}
	for _, entry := range layoutEntries {
		id := entry.Key.GetToken().Value
		present[id] = true
		switch p, moved := e.edit.Move[id]; {
		case e.deleted[id]:
			e.remove(entry)
		case moved:
			e.replace(entry, layoutLine(id, p))
		}
	}
	var added []string
	order := make([]string, 0, len(e.d.steps)+len(e.edit.Add))
	order = append(order, e.d.steps...) // in the order of the steps, for a readable file
	for _, a := range e.edit.Add {
		order = append(order, a.ID)
	}
	for _, r := range e.edit.Insert {
		order = append(order, r.ID)
	}
	for _, id := range order {
		if p, moved := e.edit.Move[id]; moved && !present[id] && !e.deleted[id] {
			added = append(added, layoutLine(id, p))
		}
	}
	if len(added) == 0 {
		return
	}
	switch {
	case len(layoutEntries) > 0:
		e.insert(maxLine(valueOf(e.top, "layout"))+1, added...)
	case layoutKey != nil:
		// "layout:" without entries (or "layout: {}"): replace the line.
		line := layoutKey.GetToken().Position.Line
		e.edits = append(e.edits, lineEdit{start: line, end: line, lines: append([]string{"layout:"}, added...)})
	default:
		e.insertAtEnd(append([]string{"layout:"}, added...)...)
	}
}

// appendItems appends block items (each given as its lines without "- ") to the list
// under key. A missing list is created before the first of the keys given in before,
// or at the end of the file.
func (e *editor) appendItems(key string, items [][]string, before ...string) error {
	entry := keyOf(e.top, key)
	var seq *ast.SequenceNode
	if entry != nil {
		seq, _ = entry.Value.(*ast.SequenceNode)
	}
	if seq != nil && len(seq.Values) > 0 {
		if seq.IsFlowStyle {
			return fmt.Errorf("the list %q is written in flow style ([…]); write it as a block list to edit it on the canvas", key)
		}
		first := minLine(seq.Values[0])
		indent := leadingSpace(e.lines[first-1])
		e.insert(maxLine(seq)+1, itemLines(indent, items)...)
		return nil
	}
	lines := append([]string{key + ":"}, itemLines("  ", items)...)
	if entry != nil {
		// "key:" without items (or "key: []"): replace the line.
		line := entry.GetToken().Position.Line
		if minLine(entry) != line || maxLine(entry) != line {
			return fmt.Errorf("the list %q cannot be edited on the canvas", key)
		}
		e.edits = append(e.edits, lineEdit{start: line, end: line, lines: lines})
		return nil
	}
	for _, k := range before {
		if next := keyOf(e.top, k); next != nil {
			e.insert(minLine(next), lines...)
			return nil
		}
	}
	e.insertAtEnd(lines...)
	return nil
}

func itemLines(indent string, items [][]string) []string {
	var out []string
	for _, item := range items {
		for i, l := range item {
			if l == "" {
				out = append(out, "")
				continue
			}
			prefix := indent + "  "
			if i == 0 {
				prefix = indent + "- "
			}
			out = append(out, prefix+l)
		}
	}
	return out
}

// flowLink writes a link in flow style: { from: a, to: b, when: failure }.
func flowLink(l linkItem) string {
	parts := []string{"from: " + l.From, "to: " + l.To}
	for _, f := range l.Fields {
		parts = append(parts, f[0]+": "+f[1])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// rewriteLink replaces a link item. A link on one line in flow style stays one line
// (with its comment); a block item is written in block style.
func (e *editor) rewriteLink(item ast.Node, l linkItem) error {
	start, end := minLine(item), maxLine(item)
	line := e.lines[start-1]
	indent := leadingSpace(line)
	if m, ok := item.(*ast.MappingNode); ok && m.IsFlowStyle && start == end && strings.HasPrefix(line[len(indent):], "- ") {
		comment := ""
		if i := strings.LastIndex(line, "}"); i >= 0 && strings.HasPrefix(strings.TrimSpace(line[i+1:]), "#") {
			comment = line[i+1:]
		}
		e.edits = append(e.edits, lineEdit{start: start, end: end, lines: []string{indent + "- " + flowLink(l) + comment}})
		return nil
	}
	if !strings.HasPrefix(line[len(indent):], "- ") {
		return errors.New("the link cannot be changed on the canvas; change it in the YAML file")
	}
	item2 := []string{"from: " + l.From, "to: " + l.To}
	for _, f := range l.Fields {
		item2 = append(item2, f[0]+": "+f[1])
	}
	e.edits = append(e.edits, lineEdit{start: start, end: end, lines: itemLines(indent, [][]string{item2})})
	return nil
}

// removeItem removes a list item that has lines of its own.
func (e *editor) removeItem(item ast.Node, what string) error {
	start := minLine(item)
	line := e.lines[start-1]
	if !strings.HasPrefix(line[len(leadingSpace(line)):], "- ") {
		return fmt.Errorf("the %s cannot be deleted on the canvas because it shares a line with others; change it in the YAML file", what)
	}
	e.remove(item)
	return nil
}

// rename changes step ids in place: the id of the step, from/to of links and layout keys.
func (e *editor) rename() error {
	type spot struct {
		col  int
		from string
		to   string
	}
	spots := map[int][]spot{}
	add := func(n ast.Node, id string) {
		if n == nil {
			return
		}
		t := n.GetToken()
		if to, ok := e.edit.Rename[id]; ok && t != nil && t.Position != nil {
			spots[t.Position.Line] = append(spots[t.Position.Line], spot{t.Position.Column, id, to})
		}
	}
	for i, item := range seqValues(valueOf(e.top, e.d.stepsKey)) {
		if i < len(e.d.steps) {
			add(valueOf(entries(item), "id"), e.d.steps[i])
		}
	}
	for i, item := range seqValues(valueOf(e.top, "links")) {
		if i < len(e.d.links) {
			add(valueOf(entries(item), "from"), e.d.links[i].From)
			add(valueOf(entries(item), "to"), e.d.links[i].To)
		}
	}
	for _, entry := range entries(valueOf(e.top, "layout")) {
		add(entry.Key, entry.Key.GetToken().Value)
	}
	for line, ss := range spots {
		// Right to left, so that the columns of the other spots stay valid.
		sort.Slice(ss, func(i, j int) bool { return ss[i].col > ss[j].col })
		text := []rune(e.lines[line-1])
		for _, s := range ss {
			at := s.col - 1
			if at < 0 || at >= len(text) {
				return fmt.Errorf("cannot rename %s %q: unexpected file content in line %d", e.d.noun, s.from, line)
			}
			old := s.from
			if q := text[at]; q == '"' || q == '\'' {
				old = string(q) + s.from + string(q)
			}
			if !strings.HasPrefix(string(text[at:]), old) {
				return fmt.Errorf("cannot rename %s %q: unexpected file content in line %d", e.d.noun, s.from, line)
			}
			text = slices.Concat(text[:at], []rune(s.to), text[at+len([]rune(old)):])
		}
		e.edits = append(e.edits, lineEdit{start: line, end: line, lines: []string{string(text)}})
	}
	return nil
}

func seqValues(n ast.Node) []ast.Node {
	if seq, ok := n.(*ast.SequenceNode); ok {
		return seq.Values
	}
	return nil
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func (e *editor) remove(n ast.Node) {
	e.edits = append(e.edits, lineEdit{start: minLine(n), end: maxLine(n)})
}

func (e *editor) replace(n ast.Node, lines ...string) {
	e.edits = append(e.edits, lineEdit{start: minLine(n), end: maxLine(n), lines: lines})
}

func (e *editor) insert(before int, lines ...string) {
	e.edits = append(e.edits, lineEdit{start: before, end: before - 1, lines: lines})
}

func (e *editor) insertAtEnd(lines ...string) {
	e.insert(len(e.lines)+1, lines...)
	e.atEnd = true
}

// lineEdit replaces the lines start..end (1-based, inclusive) with lines; end < start inserts before start.
type lineEdit struct {
	start, end int
	lines      []string
}

// applyLineEdits applies the edits from the bottom up. Insertions at the same line end
// up in the order they were added.
func applyLineEdits(lines []string, edits []lineEdit) []string {
	order := make([]int, len(edits))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := edits[order[i]], edits[order[j]]
		if a.start != b.start {
			return a.start > b.start
		}
		return order[i] > order[j]
	})
	for _, i := range order {
		e := edits[i]
		lines = slices.Replace(lines, e.start-1, e.end, e.lines...)
	}
	return lines
}

func coord(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// entries returns the key/value pairs of a mapping node (a mapping with one key is
// represented by a single MappingValueNode).
func entries(n ast.Node) []*ast.MappingValueNode {
	switch n := n.(type) {
	case *ast.MappingNode:
		return n.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{n}
	}
	return nil
}

func keyOf(es []*ast.MappingValueNode, key string) *ast.MappingValueNode {
	for _, e := range es {
		if t := e.Key.GetToken(); t != nil && t.Value == key {
			return e
		}
	}
	return nil
}

func valueOf(es []*ast.MappingValueNode, key string) ast.Node {
	if e := keyOf(es, key); e != nil {
		return e.Value
	}
	return nil
}

// lineRange collects the first and last line of the tokens of a node.
type lineRange struct{ min, max int }

func (r *lineRange) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	if t := n.GetToken(); t != nil && t.Position != nil {
		line := t.Position.Line
		if r.min == 0 || line < r.min {
			r.min = line
		}
		end := line
		if lit, ok := n.(*ast.LiteralNode); ok && lit.Value != nil {
			end += strings.Count(strings.TrimSuffix(lit.Value.Value, "\n"), "\n") + 1
		}
		r.max = max(r.max, end)
	}
	return r
}

func nodeLines(n ast.Node) lineRange {
	var r lineRange
	ast.Walk(&r, n)
	return r
}

func minLine(n ast.Node) int { return nodeLines(n).min }
func maxLine(n ast.Node) int { return nodeLines(n).max }

// PipelineEdit describes changes made on the editor's canvas of a pipeline (roadmap
// S25a), like WorkflowEdit.
type PipelineEdit struct {
	Move        map[string]Point     `json:"move,omitempty"`
	Delete      []string             `json:"delete,omitempty"`
	Add         []NewAction          `json:"add,omitempty"`
	Rename      map[string]string    `json:"rename,omitempty"`
	AddLinks    []PipelineLink       `json:"addLinks,omitempty"`
	DeleteLinks []int                `json:"deleteLinks,omitempty"`
	UpdateLinks []PipelineLinkUpdate `json:"updateLinks,omitempty"`
	// Set changes fields of transforms: copies, distribution or with.<name>.
	Set []SetField `json:"set,omitempty"`
	// SetPipeline changes a field of the pipeline: name, description or settings.*.
	SetPipeline []WorkflowField `json:"setPipeline,omitempty"`
	// Entries changes a parameter (E7c); at most one.
	Entries []EntryEdit `json:"entries,omitempty"`
}

// PipelineLinkUpdate changes the type of a link: data or error.
type PipelineLinkUpdate struct {
	Index int       `json:"index"`
	Type  *LinkType `json:"type,omitempty"`
}

var (
	transformFields = map[string]bool{"copies": true, "distribution": true}
	pipelineFields  = map[string]bool{"name": true, "description": true,
		"settings.batchSize": true, "settings.bufferSize": true, "settings.strict": true}
)

// EditPipeline applies an edit to the text of a pipeline file and returns the new text,
// changing only the lines concerned (see EditWorkflow).
func EditPipeline(name string, data []byte, edit PipelineEdit) ([]byte, error) {
	return editText(name, data, func(text []byte) (*graphDoc, *graphEdit, error) {
		d, p, err := pipelineDoc(name, text)
		if d == nil {
			return nil, nil, err
		}
		g := &graphEdit{Move: edit.Move, Delete: edit.Delete, Add: edit.Add, Rename: edit.Rename,
			DeleteLinks: edit.DeleteLinks, Set: edit.Set, SetDoc: edit.SetPipeline, Entries: edit.Entries}
		for _, l := range edit.AddLinks {
			if err := checkLinkType(l.Type); err != nil {
				return nil, nil, fmt.Errorf("cannot add link from %q to %q: %w", l.From, l.To, err)
			}
			g.AddLinks = append(g.AddLinks, pipelineLinkItem(l))
		}
		if len(edit.UpdateLinks) > 0 {
			g.UpdateLinks = map[int]linkItem{}
		}
		for _, u := range edit.UpdateLinks {
			if u.Index < 0 || u.Index >= len(p.Links) {
				return nil, nil, fmt.Errorf("cannot change link #%d: it does not exist", u.Index+1)
			}
			l := p.Links[u.Index]
			if u.Type != nil {
				if err := checkLinkType(*u.Type); err != nil {
					return nil, nil, fmt.Errorf("cannot change link #%d: %w", u.Index+1, err)
				}
				l.Type = *u.Type
			}
			g.UpdateLinks[u.Index] = pipelineLinkItem(l)
		}
		return d, g, nil
	})
}

// pipelineDoc reads a pipeline for the line editor.
func pipelineDoc(name string, text []byte) (*graphDoc, *Pipeline, error) {
	p, err := ReadPipeline(name, text)
	if p == nil {
		return nil, nil, err
	}
	d := &graphDoc{noun: "transform", docNoun: "pipeline", stepsKey: "transforms", src: p.src,
		stepFields: transformFields, docFields: pipelineFields,
		lists: map[string]int{"parameters": len(p.Parameters)}, params: p.Parameters}
	for _, t := range p.Transforms {
		d.steps = append(d.steps, t.ID)
	}
	for _, l := range p.Links {
		d.links = append(d.links, linkRef{l.From, l.To})
	}
	return d, p, nil
}

func checkLinkType(t LinkType) error {
	switch t {
	case "", DataLink, ErrorLink:
		return nil
	}
	return fmt.Errorf("link type %q is not supported here (data or error)", t)
}

// pipelineLinkItem writes a pipeline link; "data" is the default and left out.
func pipelineLinkItem(l PipelineLink) linkItem {
	item := linkItem{From: l.From, To: l.To}
	if l.Type != "" && l.Type != DataLink {
		item.Fields = append(item.Fields, [2]string{"type", string(l.Type)})
	}
	return item
}
