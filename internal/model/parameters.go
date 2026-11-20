package model

import (
	"fmt"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/vars"
)

// checkParameters checks the parameters of a workflow or pipeline: names, types,
// allowed values and default values. Expressions in defaults may use the given roots.
func checkParameters(s *source, params []Parameter, roots []string) Errors {
	var errs Errors
	add := func(path, format string, args ...any) {
		errs = append(errs, s.errorf(path, format, args...))
	}
	seen := map[string]bool{}
	for i, p := range params {
		path := fmt.Sprintf("$.parameters[%d]", i)
		switch {
		case p.Name == "":
			add(path, `parameter: missing required field "name"`)
		case !vars.ValidName(p.Name):
			add(path+".name", "invalid parameter name %q (letters, digits and _, not starting with a digit)", p.Name)
		case seen[p.Name]:
			add(path+".name", "duplicate parameter %q", p.Name)
		}
		seen[p.Name] = true
		if !p.Type.Valid() {
			add(path+".type", "parameter %q: invalid type %q (expected %s)", p.Name, p.Type, typeNames())
			continue
		}
		for j, e := range p.Enum {
			if _, err := p.Type.Parse(e); err != nil {
				add(fmt.Sprintf("%s.enum[%d]", path, j), "parameter %q: allowed value %v", p.Name, err)
			}
		}
		if p.Default != "" && !hasReferences(p.Default) {
			if _, err := p.CheckValue(p.Default); err != nil {
				add(path+".default", "parameter %q: default value %v", p.Name, err)
			}
		}
		if expr.Contains(p.Default) {
			if err := expr.Check(p.Default, roots); err != nil {
				add(path+".default", "parameter %q: default value: %v", p.Name, err)
			}
		}
	}
	return errs
}
