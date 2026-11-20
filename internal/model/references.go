package model

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml/ast"

	"github.com/runnelhq/runnel/internal/vars"
)

// scalar is a string value of a file with its position and YAML path
// (e.g. "$.actions[1].with.message"), which locates problems for the editor.
type scalar struct {
	value string
	pos   Position
	path  string
}

var simpleKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// scalars returns all string values of the document (not the mapping keys).
func (s *source) scalars() []scalar {
	var res []scalar
	var walk func(n ast.Node, path string)
	walk = func(n ast.Node, path string) {
		switch n := n.(type) {
		case *ast.MappingNode:
			for _, v := range n.Values {
				walk(v, path)
			}
		case *ast.MappingValueNode:
			key := ""
			if t := n.Key.GetToken(); t != nil {
				key = t.Value
			}
			if simpleKey.MatchString(key) {
				walk(n.Value, path+"."+key)
			} else {
				walk(n.Value, keyPath(path, key))
			}
		case *ast.SequenceNode:
			for i, v := range n.Values {
				walk(v, fmt.Sprintf("%s[%d]", path, i))
			}
		case *ast.TagNode:
			walk(n.Value, path)
		case *ast.AnchorNode:
			walk(n.Value, path)
		case *ast.LiteralNode:
			res = append(res, scalar{value: n.Value.Value, pos: tokenPos(n.GetToken()), path: path})
		case *ast.StringNode:
			res = append(res, scalar{value: n.Value, pos: tokenPos(n.GetToken()), path: path})
		}
	}
	walk(s.body, "$")
	return res
}

// checkReferenceSyntax reports malformed "${…}" references in all values of the file.
func checkReferenceSyntax(s *source) Errors {
	var errs Errors
	for _, sc := range s.scalars() {
		if _, err := vars.Parse(sc.value); err != nil {
			errs = append(errs, &Error{File: s.name, Pos: sc.pos, Path: sc.path, Msg: err.Error()})
		}
	}
	return errs
}

// VarContext is a set of variables a workflow may run with, e.g. one per environment.
type VarContext struct {
	// Environment is the name of the environment, or empty without environment.
	Environment string
	Scope       *vars.Scope
}

// CheckVariables reports malformed references (errors) and references to variables
// that are not defined in one or more of the given contexts (warnings).
// References to environment variables and secrets are not checked for existence,
// since they depend on the machine the workflow runs on.
func (w *Workflow) CheckVariables(contexts []VarContext) Errors {
	var errs Errors
	for _, sc := range w.src.scalars() {
		refs, err := vars.Parse(sc.value)
		if err != nil {
			errs = append(errs, &Error{File: w.src.name, Pos: sc.pos, Path: sc.path, Msg: err.Error()})
			continue
		}
		for _, r := range refs {
			if r.Kind != vars.KindVar {
				continue
			}
			var missing []string
			for _, c := range contexts {
				if !c.Scope.Has(r.Name) {
					missing = append(missing, c.Environment)
				}
			}
			if msg := undefinedMessage(r.Name, missing, len(contexts)); msg != "" {
				errs = append(errs, &Error{File: w.src.name, Pos: sc.pos, Path: sc.path, Severity: SeverityWarning, Msg: msg})
			}
		}
	}
	errs.Sort()
	return errs
}

func undefinedMessage(name string, missingIn []string, contexts int) string {
	if len(missingIn) == 0 {
		return ""
	}
	if len(missingIn) == contexts {
		return fmt.Sprintf("variable %q is not defined (neither as workflow parameter nor in the project or environment)", name)
	}
	quoted := make([]string, len(missingIn))
	for i, e := range missingIn {
		quoted[i] = fmt.Sprintf("%q", e)
	}
	noun := "environment"
	if len(missingIn) > 1 {
		noun = "environments"
	}
	return fmt.Sprintf("variable %q is not defined in %s %s", name, noun, strings.Join(quoted, ", "))
}
