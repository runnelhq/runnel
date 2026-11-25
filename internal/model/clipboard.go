package model

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Copy and paste on the canvas (E16, mockup design/mockups/e16-copy-paste.html): the
// copied actions or transforms are YAML text with the links between them and their
// positions, as in a file. Pasting appends them as written; ids that are already used
// get a suffix, and references to renamed steps among the pasted ones follow.

// Clip is the text copied from a workflow or pipeline.
type Clip struct {
	Text string `json:"text"`
	// Steps and Links are the number of copied steps and links.
	Steps int `json:"steps"`
	Links int `json:"links"`
	// Omitted is the number of links to steps that were not copied.
	Omitted int `json:"omitted"`
}

// Paste is text pasted into a workflow or a pipeline.
type Paste struct {
	Text string `json:"text"`
	// At is the position of the top-left pasted step on the canvas; nil places the
	// steps where they were, moved a little if an id had to be changed.
	At *Point `json:"at,omitempty"`
	// Targets: see PipelineEdit.Targets; the target fields of pasted transforms follow
	// renamed ids. nil: only "to".
	Targets func(typ string) [][]string `json:"-"`
}

// Pasted tells what was pasted.
type Pasted struct {
	// IDs are the pasted steps, with their ids in the file.
	IDs   []string `json:"ids"`
	Links int      `json:"links"`
	// Renamed are the ids that were already used (old → new).
	Renamed map[string]string `json:"renamed,omitempty"`
	// References is the number of expressions (workflows) or targets (pipelines) that
	// now name a renamed step.
	References int `json:"references"`
}

// MaxPasteSize limits the pasted text.
const MaxPasteSize = 1 << 20

// pasteOffset moves pasted steps away from the copied ones when ids had to be changed
// (they are then likely pasted into the same file).
const pasteOffset = 40

// CopyWorkflow returns the actions with the given ids, the links between them and their
// positions as YAML text.
func CopyWorkflow(name string, data []byte, ids []string) (Clip, error) {
	d, _, err := workflowDoc(name, []byte(strings.ReplaceAll(string(data), "\r\n", "\n")))
	if d == nil {
		return Clip{}, err
	}
	return copySteps(d, name, string(data), ids)
}

// CopyPipeline is CopyWorkflow for transforms of a pipeline.
func CopyPipeline(name string, data []byte, ids []string) (Clip, error) {
	d, _, err := pipelineDoc(name, []byte(strings.ReplaceAll(string(data), "\r\n", "\n")))
	if d == nil {
		return Clip{}, err
	}
	return copySteps(d, name, string(data), ids)
}

func copySteps(d *graphDoc, name, text string, ids []string) (Clip, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	top := entries(d.src.body)
	chosen := map[string]bool{}
	for _, id := range ids {
		if !slices.Contains(d.steps, id) {
			return Clip{}, fmt.Errorf("cannot copy %s %q: it does not exist", d.noun, id)
		}
		chosen[id] = true
	}
	if len(chosen) == 0 {
		return Clip{}, fmt.Errorf("nothing to copy: select one or more %ss", d.noun)
	}

	var clip Clip
	var steps, links []string
	items := seqValues(valueOf(top, d.stepsKey))
	for i := range items {
		if i < len(d.steps) && chosen[d.steps[i]] {
			item, err := itemText(lines, top, items, i)
			if err != nil {
				return Clip{}, fmt.Errorf("cannot copy %s %q: %w", d.noun, d.steps[i], err)
			}
			steps = append(steps, item...)
			clip.Steps++
		}
	}
	linkItems := seqValues(valueOf(top, "links"))
	for i, l := range d.links {
		switch {
		case chosen[l.From] && chosen[l.To] && i < len(linkItems):
			item, err := itemText(lines, top, linkItems, i)
			if err != nil {
				return Clip{}, fmt.Errorf("cannot copy the link from %q to %q: %w", l.From, l.To, err)
			}
			links = append(links, item...)
			clip.Links++
		case chosen[l.From] || chosen[l.To]:
			clip.Omitted++
		}
	}
	var layout []string
	for _, entry := range entries(valueOf(top, "layout")) {
		id := entry.Key.GetToken().Value
		var p Point
		if chosen[id] && yaml.NodeToValue(entry.Value, &p) == nil {
			layout = append(layout, fmt.Sprintf("  %s: { x: %s, y: %s }", id, coord(p.X), coord(p.Y)))
		}
	}

	out := []string{fmt.Sprintf("# Runnel: %s and %s from %s", count(clip.Steps, d.noun), count(clip.Links, "link"), name),
		d.stepsKey + ":"}
	out = append(out, steps...)
	if len(links) > 0 {
		out = append(append(out, "links:"), links...)
	}
	if len(layout) > 0 {
		out = append(append(out, "layout:"), layout...)
	}
	clip.Text = strings.Join(out, "\n") + "\n"
	return clip, nil
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// itemText returns the lines of the list item items[i], indented by two spaces as in a
// list directly under a key. The item ends before the next item or the next key of the
// document; blank lines and comments at its end belong to what follows.
func itemText(lines []string, top []*ast.MappingValueNode, items []ast.Node, i int) ([]string, error) {
	start := minLine(items[i])
	first := lines[start-1]
	indent := leadingSpace(first)
	if !strings.HasPrefix(first[len(indent):], "- ") || (i > 0 && maxLine(items[i-1]) >= start) {
		return nil, errors.New("it shares a line with others; change it in the YAML file")
	}
	end := len(lines)
	if i+1 < len(items) {
		end = minLine(items[i+1]) - 1
	} else {
		for _, entry := range top {
			if l := entry.Key.GetToken().Position.Line; l > start && l-1 < end {
				end = l - 1
			}
		}
	}
	for end > start {
		if t := strings.TrimSpace(lines[end-1]); t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		end--
	}
	out := make([]string, 0, end-start+1)
	for _, l := range lines[start-1 : end] {
		switch {
		case strings.TrimSpace(l) == "":
			out = append(out, "")
		case strings.HasPrefix(l, indent):
			out = append(out, "  "+l[len(indent):])
		default:
			out = append(out, "  "+strings.TrimLeft(l, " \t"))
		}
	}
	return out, nil
}

// clipSteps is pasted text: its steps as written and its links and positions.
type clipSteps struct {
	steps  []rawStep
	links  []clipLink
	layout map[string]Point
}

type clipLink struct {
	From     string   `yaml:"from"`
	To       string   `yaml:"to"`
	When     string   `yaml:"when"`
	Loop     bool     `yaml:"loop"`
	Disabled bool     `yaml:"disabled"`
	Type     LinkType `yaml:"type"`
}

// readClip reads pasted text with steps under stepsKey ("actions" or "transforms").
func readClip(text, stepsKey string) (c *clipSteps, err error) {
	defer func() {
		if r := recover(); r != nil { // the YAML library panics on some unusual input
			c, err = nil, errors.New("the pasted text cannot be read; check the YAML structure near unusual syntax such as tags (!) or anchors")
		}
	}()
	if len(text) > MaxPasteSize {
		return nil, fmt.Errorf("the pasted text is too large (more than %d bytes)", MaxPasteSize)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	src, perr := parseSource("clipboard", []byte(text))
	if perr != nil {
		return nil, errNoSteps
	}
	top := entries(src.body)
	other := map[string]string{"actions": "transforms", "transforms": "actions"}[stepsKey]
	items := seqValues(valueOf(top, stepsKey))
	if len(items) == 0 {
		if len(seqValues(valueOf(top, other))) > 0 {
			into := map[string]string{"actions": "workflow", "transforms": "pipeline"}[stepsKey]
			return nil, fmt.Errorf("%s can't be pasted into a %s", strings.ToUpper(other[:1])+other[1:], into)
		}
		return nil, errNoSteps
	}
	lines := strings.Split(text, "\n")
	c = &clipSteps{layout: map[string]Point{}}
	for i, item := range items {
		es := entries(item)
		idNode := valueOf(es, "id")
		if len(es) == 0 || idNode == nil {
			return nil, fmt.Errorf("entry %d under %s: missing id", i+1, stepsKey)
		}
		raw, err := itemText(lines, top, items, i)
		if err != nil {
			return nil, fmt.Errorf("entry %d under %s: %w", i+1, stepsKey, err)
		}
		// Without "- " and the two spaces of the list: lines of the step's mapping.
		for j, l := range raw {
			switch {
			case j == 0:
				raw[j] = strings.TrimPrefix(strings.TrimPrefix(l, "  "), "- ")
			case strings.HasPrefix(l, "    "):
				raw[j] = l[4:]
			default:
				raw[j] = strings.TrimLeft(l, " ")
			}
		}
		step := rawStep{ID: idNode.GetToken().Value, Lines: raw}
		if t, ok := valueOf(es, "type").(*ast.StringNode); ok {
			step.Type = t.Value
		}
		c.steps = append(c.steps, step)
	}
	if n := valueOf(top, "links"); n != nil {
		if err := yaml.NodeToValue(n, &c.links); err != nil {
			return nil, errors.New("the links of the pasted text cannot be read")
		}
	}
	if n := valueOf(top, "layout"); n != nil {
		_ = yaml.NodeToValue(n, &c.layout) // positions are optional
	}
	return c, nil
}

var errNoSteps = errors.New("the clipboard holds no actions or transforms")

// PasteWorkflow appends pasted actions with their links and positions to a workflow.
func PasteWorkflow(name string, data []byte, p Paste) ([]byte, Pasted, error) {
	return paste(name, data, p, workflowDoc)
}

// PastePipeline appends pasted transforms with their links and positions to a pipeline.
func PastePipeline(name string, data []byte, p Paste) ([]byte, Pasted, error) {
	return paste(name, data, p, pipelineDoc)
}

func paste[T any](name string, data []byte, p Paste, read func(string, []byte) (*graphDoc, T, error)) ([]byte, Pasted, error) {
	var res Pasted
	out, err := editText(name, data, func(text []byte) (*graphDoc, *graphEdit, error) {
		d, _, err := read(name, text)
		if d == nil {
			return nil, nil, err
		}
		g, r, err := pasteEdit(d, p)
		res = r
		return d, g, err
	})
	if err != nil {
		return nil, Pasted{}, err
	}
	// The pasted steps must leave a file that can be edited further.
	if d, _, err := read(name, []byte(strings.ReplaceAll(string(out), "\r\n", "\n"))); d == nil {
		return nil, Pasted{}, fmt.Errorf("the pasted text does not fit into %s: %w", name, err)
	}
	return out, res, nil
}

// pasteEdit translates pasted text into an edit of the document d.
func pasteEdit(d *graphDoc, p Paste) (*graphEdit, Pasted, error) {
	c, err := readClip(p.Text, d.stepsKey)
	if err != nil {
		return nil, Pasted{}, err
	}
	used := map[string]bool{}
	for _, id := range d.steps {
		used[id] = true
	}
	res := Pasted{Renamed: map[string]string{}}
	ids := map[string]string{} // id in the clipboard → id in the file
	for _, s := range c.steps {
		id := s.ID
		if used[id] {
			id = freeID(id, used)
			res.Renamed[s.ID] = id
		}
		used[id] = true
		if _, dup := ids[s.ID]; !dup {
			ids[s.ID] = id
		}
	}

	g := &graphEdit{Move: map[string]Point{}}
	// Duplicate ids in the pasted text: every step gets its own id.
	seen := map[string]bool{}
	for _, s := range c.steps {
		id := ids[s.ID]
		if seen[s.ID] {
			id = freeID(s.ID, used)
			used[id] = true
		}
		seen[s.ID] = true
		lines := slices.Clone(s.Lines)
		if id != s.ID {
			if !renameIDLine(lines, s.ID, id) {
				return nil, Pasted{}, fmt.Errorf("cannot paste %s %q: its id cannot be changed; change it in the YAML text", d.noun, s.ID)
			}
		}
		n := 0
		keys := targetKeys(p.Targets, s.Type)
		for j := range lines {
			if d.stepsKey == "actions" {
				lines[j], n = renameResults(lines[j], res.Renamed, n)
			} else {
				lines[j], n = renameTargets(lines[j], res.Renamed, n, keys)
			}
		}
		res.References += n
		g.Insert = append(g.Insert, rawStep{ID: id, Lines: lines})
		res.IDs = append(res.IDs, id)
		if pos, ok := c.layout[s.ID]; ok {
			g.Move[id] = pos
		}
	}

	for _, l := range c.links {
		from, okFrom := ids[l.From]
		to, okTo := ids[l.To]
		if !okFrom || !okTo {
			continue // links to steps that were not copied are left out
		}
		if d.stepsKey == "actions" {
			when, n := renameResults(l.When, res.Renamed, 0)
			res.References += n
			if err := checkWhen(when); err != nil {
				return nil, Pasted{}, fmt.Errorf("cannot paste the link from %q to %q: %w", l.From, l.To, err)
			}
			g.AddLinks = append(g.AddLinks, workflowLinkItem(Link{From: from, To: to, When: when, Loop: l.Loop, Disabled: l.Disabled}))
		} else {
			if err := checkLinkType(l.Type); err != nil {
				return nil, Pasted{}, fmt.Errorf("cannot paste the link from %q to %q: %w", l.From, l.To, err)
			}
			g.AddLinks = append(g.AddLinks, pipelineLinkItem(PipelineLink{From: from, To: to, Type: l.Type}))
		}
		res.Links++
	}

	placeSteps(g.Move, p.At, len(res.Renamed) > 0)
	if len(res.Renamed) == 0 {
		res.Renamed = nil
	}
	return g, res, nil
}

// placeSteps moves the pasted steps to at (their top-left one), or a little away from
// where they were copied if they were renamed.
func placeSteps(pos map[string]Point, at *Point, renamed bool) {
	if len(pos) == 0 || (at == nil && !renamed) {
		return
	}
	dx, dy := float64(pasteOffset), float64(pasteOffset)
	if at != nil {
		minX, minY := 0.0, 0.0
		for i, id := range slices.Sorted(maps.Keys(pos)) {
			if p := pos[id]; i == 0 || p.X < minX {
				minX = p.X
			}
			if p := pos[id]; i == 0 || p.Y < minY {
				minY = p.Y
			}
		}
		dx, dy = at.X-minX, at.Y-minY
	}
	for id, p := range pos {
		pos[id] = Point{X: p.X + dx, Y: p.Y + dy}
	}
}

var numbered = regexp.MustCompile(`^(.*)-([0-9]+)$`)

// freeID returns the first free id of the form id-2, id-3 …; an id that already ends
// with a number counts on from it (types-2 → types-3).
func freeID(id string, used map[string]bool) string {
	base, n := id, 2
	if m := numbered.FindStringSubmatch(id); m != nil {
		if k, err := strconv.Atoi(m[2]); err == nil && k < 1_000_000 {
			base, n = m[1], k+1
		}
	}
	for ; ; n++ {
		if c := base + "-" + strconv.Itoa(n); !used[c] {
			return c
		}
	}
}

var idLine = regexp.MustCompile(`^((?:\{\s*)?id:\s*)(["']?)([^"',}\s#]+)(["']?)`)

// renameIDLine changes the id of a pasted step in the line "id: …" of its mapping.
func renameIDLine(lines []string, from, to string) bool {
	for i, l := range lines {
		if strings.HasPrefix(l, " ") {
			continue // a line of a nested value
		}
		m := idLine.FindStringSubmatchIndex(l)
		if m == nil || l[m[6]:m[7]] != from || l[m[4]:m[5]] != l[m[8]:m[9]] {
			continue
		}
		lines[i] = l[:m[6]] + to + l[m[7]:]
		return true
	}
	return false
}

var (
	expressions = regexp.MustCompile(`\{\{.*?\}\}`)
	resultsRef  = regexp.MustCompile(`\bresults(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[\s*(["'])([^"'\]]+)["']\s*\])`)
	identifier  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	quotedValue = regexp.MustCompile(`^\s*(?:-\s+)?(?:[A-Za-z0-9_.-]+\s*:\s+)?(["'])`)
)

// renameResults changes results.<id> and results["<id>"] in the expressions of a line to
// the renamed ids and adds the number of changes to n. A new id that is not a name
// in expressions (e.g. with "-") is written with brackets, in the quotes that do not end
// the YAML value of the line.
func renameResults(line string, renamed map[string]string, n int) (string, int) {
	if len(renamed) == 0 || !strings.Contains(line, "results") {
		return line, n
	}
	quote := `"`
	if m := quotedValue.FindStringSubmatch(line); m != nil && m[1] == `"` {
		quote = `'`
	}
	line = expressions.ReplaceAllStringFunc(line, func(e string) string {
		return resultsRef.ReplaceAllStringFunc(e, func(ref string) string {
			m := resultsRef.FindStringSubmatch(ref)
			old, q := m[1], m[2]
			if old == "" {
				old = m[3]
			}
			to, ok := renamed[old]
			if !ok {
				return ref
			}
			n++
			switch {
			case q != "":
				return "results[" + q + to + q + "]"
			case identifier.MatchString(to):
				return "results." + to
			default:
				return "results[" + quote + to + quote + "]"
			}
		})
	})
	return line, n
}

// targetKeys returns a pattern for "<key>: <id>" with the keys of the fields of a
// transform type that name other transforms, e.g. to and default for core.switch;
// without targets only "to"; nil if the type has none.
func targetKeys(targets func(string) [][]string, typ string) *regexp.Regexp {
	keys := []string{"to"}
	if targets != nil {
		keys = nil
		for _, path := range targets(typ) {
			if k := regexp.QuoteMeta(path[len(path)-1]); k != `\*` && !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return regexp.MustCompile(`(\b(?:` + strings.Join(keys, "|") + `):\s*)(["']?)([A-Za-z][A-Za-z0-9_-]*)(["']?)(\s*(?:[,}#]|$))`)
}

// renameTargets changes "<key>: <id>" in a line of a pasted transform (to, default and
// else of core.switch and core.filter, see targetKeys) to the renamed ids.
func renameTargets(line string, renamed map[string]string, n int, ref *regexp.Regexp) (string, int) {
	if len(renamed) == 0 || ref == nil || !strings.Contains(line, ":") {
		return line, n
	}
	line = ref.ReplaceAllStringFunc(line, func(s string) string {
		m := ref.FindStringSubmatch(s)
		to, ok := renamed[m[3]]
		if !ok || m[2] != m[4] {
			return s
		}
		n++
		return m[1] + m[2] + to + m[4] + m[5]
	})
	return line, n
}
