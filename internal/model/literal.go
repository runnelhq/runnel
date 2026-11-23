package model

import "strings"

// CheckSQL reports ${…} and {{ … }} in the SQL statements of the actions (fields of
// format runnel-sql, isSQL): values come in as parameters @name only, so that values –
// e.g. from an http trigger – cannot change the statement (ADR 0022).
func (w *Workflow) CheckSQL(isSQL func(typ, field string) bool) Errors {
	types := make([]string, len(w.Actions))
	for i, a := range w.Actions {
		types[i] = a.Type
	}
	return checkSQL(w.src, stepSecretPaths("actions", types, isSQL))
}

// CheckSQL is Workflow.CheckSQL for the transforms of a pipeline.
func (pl *Pipeline) CheckSQL(isSQL func(typ, field string) bool) Errors {
	types := make([]string, len(pl.Transforms))
	for i, t := range pl.Transforms {
		types[i] = t.Type
	}
	return checkSQL(pl.src, stepSecretPaths("transforms", types, isSQL))
}

func checkSQL(s *source, isSQL func(path string) bool) Errors {
	var errs Errors
	for _, sc := range s.scalars() {
		if !isSQL(sc.path) || (!strings.Contains(sc.value, "${") && !strings.Contains(sc.value, "{{")) {
			continue
		}
		errs = append(errs, &Error{File: s.name, Pos: sc.pos, Path: sc.path,
			Msg: "the statement must not contain ${…} or {{ … }}: write @name and give the value under params, so that values cannot change the statement"})
	}
	return errs
}
