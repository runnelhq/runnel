package pipeline

import (
	"fmt"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/transform"
)

// checkInfoLinks checks the info links of a pipeline (S23b): they lead to transforms that
// take lookup data, one each, and every such transform has one. The lookup data must not
// come from a transform that also feeds the data links of the same transform, directly or
// further back: the transform reads the info link to the end first, so the records on its
// data links would wait for it and the transform before both would stop (deadlock).
func checkInfoLinks(p *model.Pipeline, reg *transform.Registry) model.Errors {
	var errs model.Errors
	def := func(id string) (transform.Definition, bool) {
		for _, t := range p.Transforms {
			if t.ID == id {
				return reg.Lookup(t.Type)
			}
		}
		return transform.Definition{}, false
	}
	var takers []string
	for _, t := range p.Transforms {
		if d, ok := reg.Lookup(t.Type); ok && d.InfoInput {
			takers = append(takers, t.Type)
		}
	}
	slices.Sort(takers)
	takers = slices.Compact(takers)
	infoOf := map[string]int{} // target → index of its first info link
	for i, l := range p.Links {
		if l.Type != model.InfoLink {
			continue
		}
		path := fmt.Sprintf("$.links[%d]", i)
		d, ok := def(l.To)
		switch {
		case !ok:
			continue // unknown type, reported elsewhere
		case !d.InfoInput:
			only := "only transforms for lookups do, e.g. core.stream-lookup"
			if len(takers) > 0 {
				only = "only lookups do (" + strings.Join(takers, ", ") + ")"
			}
			errs = append(errs, p.ErrorAt(path+".type", "link #%d (%s → %s): %s takes no info link; %s", i+1, l.From, l.To, d.Type, only))
			continue
		}
		if first, dup := infoOf[l.To]; dup {
			errs = append(errs, p.ErrorAt(path+".type", "link #%d (%s → %s): %s takes one info link; link #%d already brings its lookup data", i+1, l.From, l.To, l.To, first+1))
			continue
		}
		infoOf[l.To] = i
	}
	// Transforms before each transform (over all links), to find shared sources.
	before := func(id string) map[string]bool {
		seen := map[string]bool{id: true}
		for changed := true; changed; {
			changed = false
			for _, l := range p.Links {
				if seen[l.To] && !seen[l.From] {
					seen[l.From], changed = true, true
				}
			}
		}
		return seen
	}
	for i, t := range p.Transforms {
		d, ok := reg.Lookup(t.Type)
		if !ok || !d.InfoInput {
			continue
		}
		li, has := infoOf[t.ID]
		if !has {
			errs = append(errs, p.ErrorAt(fmt.Sprintf("$.transforms[%d]", i), "transform %q: %s needs an info link with the lookup data", t.ID, t.Type))
			continue
		}
		info := before(p.Links[li].From)
		for k, l := range p.Links {
			if l.To != t.ID || k == li {
				continue
			}
			for _, x := range p.Transforms { // in file order, for a stable message
				if info[x.ID] && before(l.From)[x.ID] {
					errs = append(errs, p.ErrorAt(fmt.Sprintf("$.links[%d]", li),
						"link #%d (%s → %s): the lookup data and the records of link #%d both come from %q; %s reads the lookup data completely first, so they must come from separate transforms",
						li+1, p.Links[li].From, t.ID, k+1, x.ID, t.ID))
					break
				}
			}
		}
	}
	return errs
}
