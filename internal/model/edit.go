package model

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

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
}

// NewAction is an action added on the canvas.
type NewAction struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// LinkUpdate sets the condition of the link with the given index.
type LinkUpdate struct {
	Index int `json:"index"`
	// When is "success", "failure" or "always"; "success" removes the field.
	When string `json:"when"`
}

// EditWorkflow applies an edit to the text of a workflow file and returns the new text.
// Only the lines concerned are changed: comments, order, quoting and formatting of
// everything else stay as they are, so that a saved file gives a small Git diff.
// The workflow must be readable (no syntax or field errors).
func EditWorkflow(name string, data []byte, edit WorkflowEdit) ([]byte, error) {
	text := string(data)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	// Line numbers of the parser refer to LF line endings; CRLF is restored when joining.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	w, err := ParseWorkflow(name, []byte(text))
	if err != nil {
		return nil, err
	}
	finalNewline := strings.HasSuffix(text, "\n")
	e := &editor{
		w:     w,
		edit:  edit,
		top:   entries(w.src.body),
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

// editor collects the line edits for one WorkflowEdit.
type editor struct {
	w     *Workflow
	edit  WorkflowEdit
	top   []*ast.MappingValueNode
	lines []string
	edits []lineEdit
	// atEnd is set when lines are appended at the end of the file.
	atEnd bool
	// ids are the actions after the edit (existing minus deleted plus added).
	ids     map[string]bool
	deleted map[string]bool
}

func (e *editor) check() error {
	edit := e.edit
	if len(edit.Rename) > 0 && (len(edit.Move) > 0 || len(edit.Delete) > 0 || len(edit.Add) > 0 ||
		len(edit.AddLinks) > 0 || len(edit.DeleteLinks) > 0 || len(edit.UpdateLinks) > 0) {
		return errors.New("renaming cannot be combined with other changes")
	}
	e.ids = map[string]bool{}
	for _, a := range e.w.Actions {
		e.ids[a.ID] = true
	}
	e.deleted = map[string]bool{}
	for _, id := range edit.Delete {
		if !e.ids[id] {
			return fmt.Errorf("cannot delete action %q: it does not exist", id)
		}
		e.deleted[id] = true
	}
	for id := range e.deleted {
		delete(e.ids, id)
	}
	for _, a := range edit.Add {
		if err := checkID(a.ID); err != nil {
			return fmt.Errorf("cannot add action: %w", err)
		}
		if e.ids[a.ID] || e.deleted[a.ID] {
			return fmt.Errorf("cannot add action %q: the id is already used", a.ID)
		}
		if !actionTypePattern.MatchString(a.Type) {
			return fmt.Errorf("cannot add action %q: invalid action type %q", a.ID, a.Type)
		}
		e.ids[a.ID] = true
	}
	for id := range edit.Move {
		if !e.ids[id] {
			return fmt.Errorf("cannot move action %q: it does not exist", id)
		}
	}
	for old, id := range edit.Rename {
		if !e.ids[old] {
			return fmt.Errorf("cannot rename action %q: it does not exist", old)
		}
		if err := checkID(id); err != nil {
			return fmt.Errorf("cannot rename action %q: %w", old, err)
		}
		if e.ids[id] {
			return fmt.Errorf("cannot rename action %q to %q: the id is already used", old, id)
		}
	}
	for _, l := range edit.AddLinks {
		for _, id := range []string{l.From, l.To} {
			if !e.ids[id] {
				return fmt.Errorf("cannot add link from %q to %q: action %q does not exist", l.From, l.To, id)
			}
		}
		if err := checkWhen(l.When); err != nil {
			return fmt.Errorf("cannot add link from %q to %q: %w", l.From, l.To, err)
		}
	}
	links := map[int]bool{}
	for _, i := range edit.DeleteLinks {
		if i < 0 || i >= len(e.w.Links) {
			return fmt.Errorf("cannot delete link #%d: it does not exist", i+1)
		}
		links[i] = true
	}
	for _, u := range edit.UpdateLinks {
		if u.Index < 0 || u.Index >= len(e.w.Links) || links[u.Index] {
			return fmt.Errorf("cannot change link #%d: it does not exist", u.Index+1)
		}
		if err := checkWhen(u.When); err != nil {
			return fmt.Errorf("cannot change link #%d: %w", u.Index+1, err)
		}
	}
	return nil
}

func checkID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid action id %q (letters, digits, - and _, starting with a letter)", id)
	}
	return nil
}

func checkWhen(when string) error {
	switch when {
	case "", "success", "failure", "always":
		return nil
	}
	return fmt.Errorf(`condition %q is not supported here (success, failure or always)`, when)
}

// apply handles all changes except renaming.
func (e *editor) apply() error {
	edit := e.edit
	actions := valueOf(e.top, "actions")
	linksNode := valueOf(e.top, "links")

	// Actions: deleted ones are removed, new ones appended.
	if seq, ok := actions.(*ast.SequenceNode); ok && len(e.deleted) > 0 {
		for i, item := range seq.Values {
			if i < len(e.w.Actions) && e.deleted[e.w.Actions[i].ID] {
				if err := e.removeItem(item, "action"); err != nil {
					return err
				}
			}
		}
	}
	if len(edit.Add) > 0 {
		var items [][]string
		for _, a := range edit.Add {
			items = append(items, []string{"id: " + a.ID, "type: " + a.Type})
		}
		if err := e.appendItems("actions", items, "links", "layout"); err != nil {
			return err
		}
	}

	// Links: removed with their actions or on their own, changed, appended.
	removed := map[int]bool{}
	for _, i := range edit.DeleteLinks {
		removed[i] = true
	}
	if seq, ok := linksNode.(*ast.SequenceNode); ok {
		for i, item := range seq.Values {
			if i >= len(e.w.Links) {
				break
			}
			l := e.w.Links[i]
			if removed[i] || e.deleted[l.From] || e.deleted[l.To] {
				if err := e.removeItem(item, "link"); err != nil {
					return err
				}
				removed[i] = true
			}
		}
		for _, u := range edit.UpdateLinks {
			if removed[u.Index] || u.Index >= len(seq.Values) {
				continue
			}
			l := e.w.Links[u.Index]
			l.When = u.When
			if l.When == "success" {
				l.When = ""
			}
			if err := e.rewriteLink(seq.Values[u.Index], l); err != nil {
				return err
			}
		}
	}
	if len(edit.AddLinks) > 0 {
		var items [][]string
		for _, l := range edit.AddLinks {
			if l.When == "success" {
				l.When = ""
			}
			items = append(items, []string{flowLink(l)})
		}
		if err := e.appendItems("links", items, "layout"); err != nil {
			return err
		}
	}

	e.layout()
	return nil
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
	order := make([]string, 0, len(e.w.Actions)+len(e.edit.Add))
	for _, a := range e.w.Actions { // in the order of the actions, for a readable file
		order = append(order, a.ID)
	}
	for _, a := range e.edit.Add {
		order = append(order, a.ID)
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
func flowLink(l Link) string {
	parts := []string{"from: " + l.From, "to: " + l.To}
	if l.When != "" {
		parts = append(parts, "when: "+l.When)
	}
	if l.Loop {
		parts = append(parts, "loop: true")
	}
	if l.Disabled {
		parts = append(parts, "disabled: true")
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// rewriteLink replaces a link item. A link on one line in flow style stays one line
// (with its comment); a block item is written in block style.
func (e *editor) rewriteLink(item ast.Node, l Link) error {
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
	if l.When != "" {
		item2 = append(item2, "when: "+l.When)
	}
	if l.Loop {
		item2 = append(item2, "loop: true")
	}
	if l.Disabled {
		item2 = append(item2, "disabled: true")
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

// rename changes action ids in place: the id of the action, from/to of links and layout keys.
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
	for i, item := range seqValues(valueOf(e.top, "actions")) {
		if i < len(e.w.Actions) {
			add(valueOf(entries(item), "id"), e.w.Actions[i].ID)
		}
	}
	for i, item := range seqValues(valueOf(e.top, "links")) {
		if i < len(e.w.Links) {
			add(valueOf(entries(item), "from"), e.w.Links[i].From)
			add(valueOf(entries(item), "to"), e.w.Links[i].To)
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
				return fmt.Errorf("cannot rename action %q: unexpected file content in line %d", s.from, line)
			}
			old := s.from
			if q := text[at]; q == '"' || q == '\'' {
				old = string(q) + s.from + string(q)
			}
			if !strings.HasPrefix(string(text[at:]), old) {
				return fmt.Errorf("cannot rename action %q: unexpected file content in line %d", s.from, line)
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
