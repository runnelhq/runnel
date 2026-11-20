package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Validate checks the pipeline as a graph: at least one transform, unique ids, links
// between existing transforms, no cycles (records only flow forward) and transforms
// that are not connected to the others. It returns errors and warnings, sorted by
// position. It expects a pipeline returned by ParsePipeline or LoadPipeline.
func (p *Pipeline) Validate() Errors {
	var diags Errors
	errorf := func(path, format string, args ...any) { diags = append(diags, p.src.errorf(path, format, args...)) }
	warnf := func(path, format string, args ...any) { diags = append(diags, p.src.warnf(path, format, args...)) }
	label := func(i int, l PipelineLink) string { return fmt.Sprintf("link #%d (%s → %s)", i+1, l.From, l.To) }

	if len(p.Transforms) == 0 {
		errorf("$", `pipeline has no transforms (add them under "transforms:")`)
	}
	index := map[string]int{}
	ids := make([]string, 0, len(p.Transforms))
	for i, t := range p.Transforms {
		if first, dup := index[t.ID]; dup {
			errorf(fmt.Sprintf("$.transforms[%d].id", i), "duplicate transform id %q (first defined at line %d)",
				t.ID, p.src.pos(fmt.Sprintf("$.transforms[%d]", first)).Line)
			continue
		}
		index[t.ID] = i
		ids = append(ids, t.ID)
	}

	// Links: references, links to itself, duplicates.
	succ := map[string][]int{}
	connected := map[string]bool{}
	seen := map[[2]string]int{}
	for i, l := range p.Links {
		path := fmt.Sprintf("$.links[%d]", i)
		_, fromOK := index[l.From]
		_, toOK := index[l.To]
		if !fromOK && l.From != "" {
			errorf(path+".from", "%s: unknown transform %q%s", label(i, l), l.From, Suggest(l.From, ids))
		}
		if !toOK && l.To != "" {
			errorf(path+".to", "%s: unknown transform %q%s", label(i, l), l.To, Suggest(l.To, ids))
		}
		if !fromOK || !toOK {
			continue
		}
		connected[l.From], connected[l.To] = true, true
		if l.From == l.To {
			errorf(path, "%s: a transform cannot pass records to itself", label(i, l))
			continue
		}
		k := [2]string{l.From, l.To}
		if first, dup := seen[k]; dup {
			warnf(path, "%s: same link as link #%d", label(i, l), first+1)
			continue
		}
		seen[k] = i
		succ[l.From] = append(succ[l.From], i)
	}

	// Cycles: records flow only forward (KONZEPT, chapter 2.3).
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
			l := p.Links[li]
			switch state[l.To] {
			case unvisited:
				visit(l.To)
			case inProgress:
				cycle := append(slices.Clone(stack[slices.Index(stack, l.To):]), l.To)
				errorf(fmt.Sprintf("$.links[%d]", li), "%s closes a cycle (%s); records in a pipeline can only flow forward",
					label(li, l), strings.Join(cycle, " → "))
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
	}
	for _, id := range ids {
		if state[id] == unvisited {
			visit(id)
		}
	}

	// A transform without any link does not take part in the flow of the others.
	if len(ids) > 1 {
		for _, id := range ids {
			if !connected[id] {
				warnf(fmt.Sprintf("$.transforms[%d].id", index[id]), "transform %q is not connected to any other transform", id)
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(p.Layout)) {
		if _, ok := index[k]; !ok {
			warnf(keyPath("$.layout", k), "layout entry for unknown transform %q", k)
		}
	}
	diags.Sort()
	return diags
}
