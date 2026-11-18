package model

import (
	"fmt"
	"slices"
	"strings"
)

// ActionTypeStart is the type of the action where every workflow begins.
const ActionTypeStart = "core.start"

// Validate checks the workflow as a graph: references between actions and links,
// the start action, cycles and reachability. It returns errors and warnings,
// sorted by position; use Errors.HasErrors to decide whether the workflow is usable.
// It expects a workflow returned by ParseWorkflow or LoadWorkflow.
func (w *Workflow) Validate() Errors {
	v := graphValidator{w: w, s: w.src, index: map[string]int{}}
	v.checkActions()
	v.checkLinks()
	v.checkCycles()
	v.checkReachability()
	v.checkLayout()
	v.diags.Sort()
	return v.diags
}

type graphValidator struct {
	w     *Workflow
	s     *source
	index map[string]int // action id → index of its first definition
	start string         // id of the start action, if exactly one exists
	diags Errors
}

func (v *graphValidator) errorf(path, format string, args ...any) {
	v.diags = append(v.diags, v.s.errorf(path, format, args...))
}

func (v *graphValidator) warnf(path, format string, args ...any) {
	v.diags = append(v.diags, v.s.warnf(path, format, args...))
}

func linkLabel(i int, l Link) string {
	return fmt.Sprintf("link #%d (%s → %s)", i+1, l.From, l.To)
}

func (v *graphValidator) checkActions() {
	var starts []int
	for i, a := range v.w.Actions {
		path := fmt.Sprintf("$.actions[%d]", i)
		if first, dup := v.index[a.ID]; dup {
			v.errorf(path+".id", "duplicate action id %q (first defined at line %d)",
				a.ID, v.s.pos(fmt.Sprintf("$.actions[%d]", first)).Line)
		} else {
			v.index[a.ID] = i
		}
		if a.Type == ActionTypeStart {
			starts = append(starts, i)
		}
		if a.JoinTimeout != "" && (a.Join == JoinFirst || a.Join == JoinEach) {
			v.warnf(path+".joinTimeout", "action %q: joinTimeout has no effect with join %q (only with join all)", a.ID, a.Join)
		}
	}
	switch len(starts) {
	case 0:
		v.errorf("$.actions", "workflow has no start action (an action of type %q)", ActionTypeStart)
	case 1:
		v.start = v.w.Actions[starts[0]].ID
	default:
		for _, i := range starts[1:] {
			v.errorf(fmt.Sprintf("$.actions[%d].type", i), "workflow has more than one start action (first: %q)",
				v.w.Actions[starts[0]].ID)
		}
	}
}

func (v *graphValidator) checkLinks() {
	type key struct {
		from, to string
		when     ConditionKind
		expr     string
	}
	seen := map[key]int{}
	for i, l := range v.w.Links {
		path := fmt.Sprintf("$.links[%d]", i)
		_, fromOK := v.index[l.From]
		_, toOK := v.index[l.To]
		if !fromOK {
			v.errorf(path+".from", "%s: unknown action %q%s", linkLabel(i, l), l.From, v.suggest(l.From))
		}
		if !toOK {
			v.errorf(path+".to", "%s: unknown action %q%s", linkLabel(i, l), l.To, v.suggest(l.To))
		}
		if l.Disabled || !fromOK || !toOK {
			continue
		}
		if l.To == v.start {
			v.errorf(path+".to", "%s: the start action cannot have incoming links", linkLabel(i, l))
		}
		if l.From == l.To && !l.Loop {
			v.errorf(path, "%s: a link from an action to itself must be marked with loop: true", linkLabel(i, l))
		}
		kind, expr := l.Condition()
		k := key{l.From, l.To, kind, expr}
		if first, dup := seen[k]; dup {
			v.warnf(path, "%s: same link as link #%d", linkLabel(i, l), first+1)
		} else {
			seen[k] = i
		}
	}
}

// activeLinks returns the indexes of enabled links between existing actions.
func (v *graphValidator) activeLinks(includeLoops bool) []int {
	var res []int
	for i, l := range v.w.Links {
		_, fromOK := v.index[l.From]
		_, toOK := v.index[l.To]
		if l.Disabled || !fromOK || !toOK || (l.Loop && !includeLoops) {
			continue
		}
		res = append(res, i)
	}
	return res
}

// successors builds the adjacency list (action id → link indexes) for the given links.
func (v *graphValidator) successors(links []int) map[string][]int {
	succ := map[string][]int{}
	for _, i := range links {
		from := v.w.Links[i].From
		succ[from] = append(succ[from], i)
	}
	return succ
}

// checkCycles reports cycles made of links that are not marked as loop links,
// and loop links that do not lead back to an earlier action (ADR 0003).
func (v *graphValidator) checkCycles() {
	// Links into the start action and self-links are already reported by checkLinks.
	links := slices.DeleteFunc(v.activeLinks(false), func(i int) bool {
		l := v.w.Links[i]
		return l.To == v.start || l.From == l.To
	})
	succ := v.successors(links)

	const (
		unvisited = iota
		inProgress
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string)
	visit = func(id string) {
		state[id] = inProgress
		stack = append(stack, id)
		for _, li := range succ[id] {
			l := v.w.Links[li]
			switch state[l.To] {
			case unvisited:
				visit(l.To)
			case inProgress:
				cycle := append(slices.Clone(stack[slices.Index(stack, l.To):]), l.To)
				v.errorf(fmt.Sprintf("$.links[%d]", li),
					"%s closes a cycle (%s); mark the link back with loop: true if the repetition is intended",
					linkLabel(li, l), strings.Join(cycle, " → "))
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
	}
	for _, a := range v.w.Actions {
		if state[a.ID] == unvisited {
			visit(a.ID)
		}
	}

	for _, li := range v.activeLinks(true) {
		l := v.w.Links[li]
		if l.Loop && !reaches(succ, v.w.Links, l.To, l.From) {
			v.warnf(fmt.Sprintf("$.links[%d].loop", li),
				"%s is marked as loop link but does not lead back to an earlier action", linkLabel(li, l))
		}
	}
}

// reaches reports whether "to" can be reached from "from" following the given adjacency list.
func reaches(succ map[string][]int, links []Link, from, to string) bool {
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == to {
			return true
		}
		for _, li := range succ[id] {
			if next := links[li].To; !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

func (v *graphValidator) checkReachability() {
	if v.start == "" {
		return
	}
	succ := v.successors(v.activeLinks(true))
	for i, a := range v.w.Actions {
		if v.index[a.ID] != i || a.ID == v.start {
			continue
		}
		if !reaches(succ, v.w.Links, v.start, a.ID) {
			v.warnf(fmt.Sprintf("$.actions[%d].id", i), "action %q is not reachable from the start action %q and will never run",
				a.ID, v.start)
		}
	}
}

func (v *graphValidator) checkLayout() {
	keys := make([]string, 0, len(v.w.Layout))
	for k := range v.w.Layout {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, ok := v.index[k]; !ok {
			v.warnf(keyPath("$.layout", k), "layout entry for unknown action %q", k)
		}
	}
}

// suggest returns a hint for a similar existing action id, e.g. for typos.
func (v *graphValidator) suggest(id string) string {
	best, bestDist := "", 3 // suggest only for up to 2 edits
	for _, a := range v.w.Actions {
		if d := editDistance(id, a.ID); d < bestDist {
			best, bestDist = a.ID, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
