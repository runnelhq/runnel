package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/model"
)

// expressionsResponse is the reference for the expression editor (roadmap S35): the
// names and functions of expressions and the variables of the project.
type expressionsResponse struct {
	Roots     []expr.Doc `json:"roots"`
	Functions []expr.Doc `json:"functions"`
	// Variables of the project and the default environment, as written in the files.
	Variables []variableDoc `json:"variables"`
}

type variableDoc struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Source is the file that sets the value, e.g. "project.yaml" or "environments/dev.yaml".
	Source string `json:"source"`
}

func (s *Server) handleExpressions(w http.ResponseWriter, _ *http.Request) {
	res := expressionsResponse{Roots: expr.Roots, Functions: expr.Functions, Variables: []variableDoc{}}
	p, err := model.LoadProject(s.dir)
	var diags model.Errors
	if errors.As(err, &diags) && diags.HasErrors() || err != nil && !errors.As(err, &diags) {
		// Without a readable project there are no variables; the editor still gets the rest.
		writeJSON(w, http.StatusOK, res)
		return
	}
	vars := map[string]variableDoc{}
	// The built-in folder of the project (ADR 0020); project and environment may override it.
	if dir, err := filepath.Abs(s.dir); err == nil {
		vars[model.ProjectDirVariable] = variableDoc{Name: model.ProjectDirVariable, Value: dir, Source: "built-in"}
	}
	for name, v := range p.Variables {
		vars[name] = variableDoc{Name: name, Value: v, Source: model.ProjectFile}
	}
	if env, err := p.SelectEnvironment(""); err == nil && env != nil {
		for name, v := range env.Variables {
			vars[name] = variableDoc{Name: name, Value: v, Source: "environments/" + env.Name + ".yaml"}
		}
	}
	for _, v := range vars {
		res.Variables = append(res.Variables, v)
	}
	slices.SortFunc(res.Variables, func(a, b variableDoc) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, res)
}
