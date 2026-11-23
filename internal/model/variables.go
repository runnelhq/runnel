package model

import (
	"github.com/goccy/go-yaml/ast"

	"github.com/runnelhq/runnel/internal/vars"
)

// VarUse is a reference ${NAME} to a variable in a file, at the position of its value
// (E18a). References to environment variables (${env:…}) and secrets are not variables.
type VarUse struct {
	Name string
	Pos  Position
}

// SecretUse is a reference ${secret:[STORE/]NAME} in a file, at the position of its value.
type SecretUse struct {
	Name  string
	Store string // the store named in the reference; empty for the default store
	Pos   Position
}

// ScanSecrets returns the references to secrets in any YAML file of a project, like
// ScanVariables (S39c1).
func ScanSecrets(name string, data []byte) ([]SecretUse, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	var uses []SecretUse
	for _, sc := range src.scalars() {
		refs, err := vars.Parse(sc.value)
		if err != nil {
			continue
		}
		for _, r := range refs {
			if r.Kind == vars.KindSecret {
				uses = append(uses, SecretUse{Name: r.Name, Store: r.Store, Pos: sc.pos})
			}
		}
	}
	return uses, nil
}

// ScanVariables returns the references to variables in any YAML file of a project and
// the names of the parameters it declares (parameters[].name of workflows and
// pipelines), so that uses of parameters can be told apart from uses of project
// variables. Malformed references are left out; validate reports them.
func ScanVariables(name string, data []byte) (uses []VarUse, params []string, err error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, nil, err
	}
	for _, sc := range src.scalars() {
		refs, err := vars.Parse(sc.value)
		if err != nil {
			continue
		}
		for _, r := range refs {
			if r.Kind == vars.KindVar {
				uses = append(uses, VarUse{Name: r.Name, Pos: sc.pos})
			}
		}
	}
	return uses, parameterNames(src.body), nil
}

// parameterNames returns the values of parameters[].name at the top of a document.
func parameterNames(body ast.Node) []string {
	m, ok := body.(*ast.MappingNode)
	if !ok {
		return nil
	}
	for _, kv := range m.Values {
		if t := kv.Key.GetToken(); t == nil || t.Value != "parameters" {
			continue
		}
		seq, ok := kv.Value.(*ast.SequenceNode)
		if !ok {
			return nil
		}
		var names []string
		for _, item := range seq.Values {
			im, ok := item.(*ast.MappingNode)
			if !ok {
				continue
			}
			for _, f := range im.Values {
				if t := f.Key.GetToken(); t != nil && t.Value == "name" {
					if s, ok := f.Value.(*ast.StringNode); ok {
						names = append(names, s.Value)
					}
				}
			}
		}
		return names
	}
	return nil
}

// VariablePos returns the position of the value of a variable in project.yaml.
func (p *Project) VariablePos(name string) Position {
	if p.src == nil {
		return Position{}
	}
	return p.src.pos(keyPath("$.variables", name))
}

// VariablePos returns the position of the value of a variable in the environment file.
func (e *Environment) VariablePos(name string) Position {
	if e.src == nil {
		return Position{}
	}
	return e.src.pos(keyPath("$.variables", name))
}
