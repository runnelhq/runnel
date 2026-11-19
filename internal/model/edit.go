package model

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// WorkflowEdit describes changes made on the editor's canvas (roadmap E6a).
type WorkflowEdit struct {
	// Move sets the canvas position of actions (layout block, pixels, top-left corner).
	Move map[string]Point `json:"move,omitempty"`
	// Delete removes actions together with their links and layout entries.
	Delete []string `json:"delete,omitempty"`
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
	ids := map[string]bool{}
	for _, a := range w.Actions {
		ids[a.ID] = true
	}
	deleted := map[string]bool{}
	for _, id := range edit.Delete {
		if !ids[id] {
			return nil, fmt.Errorf("cannot delete action %q: it does not exist", id)
		}
		deleted[id] = true
	}
	for id := range edit.Move {
		if !ids[id] {
			return nil, fmt.Errorf("cannot move action %q: it does not exist", id)
		}
	}

	finalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	var edits []lineEdit
	top := entries(w.src.body)

	// Deleted actions and the links from or to them.
	if seq, ok := valueOf(top, "actions").(*ast.SequenceNode); ok && len(deleted) > 0 {
		for i, item := range seq.Values {
			if i < len(w.Actions) && deleted[w.Actions[i].ID] {
				edits = append(edits, lineEdit{start: minLine(item), end: maxLine(item)})
			}
		}
	}
	if seq, ok := valueOf(top, "links").(*ast.SequenceNode); ok && len(deleted) > 0 {
		for i, item := range seq.Values {
			if i < len(w.Links) && (deleted[w.Links[i].From] || deleted[w.Links[i].To]) {
				edits = append(edits, lineEdit{start: minLine(item), end: maxLine(item)})
			}
		}
	}

	// Layout entries: removed for deleted actions, replaced or added for moved ones.
	layoutKey := keyOf(top, "layout")
	layoutEntries := entries(valueOf(top, "layout"))
	indent := "  "
	if len(layoutEntries) > 0 {
		indent = strings.Repeat(" ", max(0, layoutEntries[0].Key.GetToken().Position.Column-1))
	}
	layoutLine := func(id string, p Point) string {
		return fmt.Sprintf("%s%s: { x: %s, y: %s }", indent, id, coord(p.X), coord(p.Y))
	}
	present := map[string]bool{}
	for _, e := range layoutEntries {
		id := e.Key.GetToken().Value
		present[id] = true
		switch p, moved := edit.Move[id]; {
		case deleted[id]:
			edits = append(edits, lineEdit{start: minLine(e), end: maxLine(e)})
		case moved:
			edits = append(edits, lineEdit{start: minLine(e), end: maxLine(e), lines: []string{layoutLine(id, p)}})
		}
	}
	var added []string
	for _, a := range w.Actions { // in the order of the actions, for a readable file
		if p, moved := edit.Move[a.ID]; moved && !present[a.ID] && !deleted[a.ID] {
			added = append(added, layoutLine(a.ID, p))
		}
	}
	if len(added) > 0 {
		switch {
		case len(layoutEntries) > 0:
			after := maxLine(valueOf(top, "layout"))
			edits = append(edits, lineEdit{start: after + 1, end: after, lines: added})
		case layoutKey != nil:
			// "layout:" without entries (or "layout: {}"): replace the line.
			line := layoutKey.GetToken().Position.Line
			edits = append(edits, lineEdit{start: line, end: line, lines: append([]string{"layout:"}, added...)})
		default:
			edits = append(edits, lineEdit{start: len(lines) + 1, end: len(lines), lines: append([]string{"layout:"}, added...)})
		}
	}

	lines = applyLineEdits(lines, edits)
	out := strings.Join(lines, eol)
	if finalNewline || len(added) > 0 {
		out += eol
	}
	if _, err := parseSource(name, []byte(strings.ReplaceAll(out, "\r\n", "\n"))); err != nil {
		return nil, fmt.Errorf("the edit could not be applied to %s: %w", name, err)
	}
	return []byte(out), nil
}

// lineEdit replaces the lines start..end (1-based, inclusive) with lines; end < start inserts before start.
type lineEdit struct {
	start, end int
	lines      []string
}

func applyLineEdits(lines []string, edits []lineEdit) []string {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
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
