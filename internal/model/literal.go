package model

import (
	"fmt"
	"strings"

	"github.com/runnelhq/runnel/internal/vars"
)

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
	return checkLiteral(s, isSQL, "the statement must not contain ${…} or {{ … }}: write @name and give the value under params, so that values cannot change the statement")
}

// CheckScript reports ${…} and {{ … }} in the scripts of shell.powershell and in the
// files of scripts (format runnel-script, isScript): values come in as params only (ADR
// 0023, 0028). The JavaScript of core.script is not checked: there they are plain text.
func (w *Workflow) CheckScript(isScript func(typ, field string) bool) Errors {
	types := make([]string, len(w.Actions))
	for i, a := range w.Actions {
		types[i] = a.Type
	}
	const powershell = "shell.powershell"
	errs := checkLiteral(w.src, stepSecretPaths("actions", types, func(typ, field string) bool {
		return typ == powershell && isScript(typ, field)
	}), "the script must not contain ${…} or {{ … }}: declare a parameter with param(...) and give the value under params, so that values cannot change the script; for PowerShell variables write $Name or $env:NAME instead of ${…}")
	return append(errs, checkLiteral(w.src, stepSecretPaths("actions", types, func(typ, field string) bool {
		return typ != powershell && isScript(typ, field)
	}), "the script file must not contain ${…} or {{ … }}: write its path as it is and give values under params, so that values cannot choose the script")...)
}

// CheckScript is Workflow.CheckScript for the transforms of a pipeline: the file of a
// script must not contain ${…} or {{ … }}.
func (pl *Pipeline) CheckScript(isScript func(typ, field string) bool) Errors {
	types := make([]string, len(pl.Transforms))
	for i, t := range pl.Transforms {
		types[i] = t.Type
	}
	return checkLiteral(pl.src, stepSecretPaths("transforms", types, isScript),
		"the script file must not contain ${…} or {{ … }}: write its path as it is and give values under params, so that values cannot choose the script")
}

// checkLiteral reports ${…} and {{ … }} in fields that are passed as written.
func checkLiteral(s *source, is func(path string) bool, msg string) Errors {
	var errs Errors
	for _, sc := range s.scalars() {
		if !is(sc.path) || (!strings.Contains(sc.value, "${") && !strings.Contains(sc.value, "{{")) {
			continue
		}
		errs = append(errs, &Error{File: s.name, Pos: sc.pos, Path: sc.path, Msg: msg})
	}
	return errs
}

// CheckProgram reports parameters of the workflow and {{ … }} in the program of
// shell.run (fields of format runnel-program, isProgram): variables of the project and
// its environments are allowed, but a caller – e.g. of an http trigger – must not choose
// what runs (ADR 0023).
func (w *Workflow) CheckProgram(isProgram func(typ, field string) bool) Errors {
	types := make([]string, len(w.Actions))
	for i, a := range w.Actions {
		types[i] = a.Type
	}
	params := map[string]bool{}
	for _, p := range w.Parameters {
		params[p.Name] = true
	}
	is := stepSecretPaths("actions", types, isProgram)
	var errs Errors
	for _, sc := range w.src.scalars() {
		if !is(sc.path) {
			continue
		}
		if strings.Contains(sc.value, "{{") {
			errs = append(errs, &Error{File: w.src.name, Pos: sc.pos, Path: sc.path,
				Msg: "the program must not contain {{ … }}: pass values as args, so that they cannot choose what runs"})
			continue
		}
		refs, err := vars.Parse(sc.value)
		if err != nil {
			continue // reported by checkReferenceSyntax
		}
		for _, r := range refs {
			if r.Kind == vars.KindVar && params[r.Name] {
				errs = append(errs, &Error{File: w.src.name, Pos: sc.pos, Path: sc.path,
					Msg: fmt.Sprintf("the program must not use the parameter %s: pass it as one of args; variables of the project and its environments are allowed", r)})
			}
		}
	}
	return errs
}
