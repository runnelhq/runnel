package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/transform"
)

// fieldsRequest asks an input transform of a pipeline to propose its fields (S46). The
// content is the text the editor shows, saved or not.
type fieldsRequest struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Transform string `json:"transform"`
}

type fieldsResponse struct {
	*transform.Detection
	// Environment is the environment whose variables were used; empty without.
	Environment string `json:"environment,omitempty"`
	// Variables is set when the configuration has ${…} references (resolved here).
	Variables bool `json:"variables,omitempty"`
}

// fieldsProblem is the answer when the input cannot be read: the message and the
// folder relative paths refer to.
type fieldsProblem struct {
	Error   string `json:"error"`
	BaseDir string `json:"baseDir,omitempty"`
}

// handleDetectFields proposes the fields of an input transform from the first rows of
// its input (S46): its configuration with the variables resolved with the defaults of
// the parameters and the default environment, like a run without values.
func (s *Server) handleDetectFields(w http.ResponseWriter, r *http.Request) {
	var req fieldsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFileSize+64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	if fileKind(name) != "pipeline" {
		writeError(w, http.StatusBadRequest, "fields can only be detected in pipeline files (*.pipeline.yaml)")
		return
	}
	p, err := model.ReadPipeline(name, []byte(req.Content))
	if p == nil {
		writeError(w, http.StatusUnprocessableEntity, "the pipeline cannot be read: "+err.Error())
		return
	}
	var t *model.Transform
	for i := range p.Transforms {
		if p.Transforms[i].ID == req.Transform {
			t = &p.Transforms[i]
		}
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "the pipeline has no transform "+req.Transform)
		return
	}
	def, ok := s.transforms.Lookup(t.Type)
	if !ok || def.DetectFields == nil {
		writeError(w, http.StatusBadRequest, "transform type "+t.Type+" cannot detect its fields")
		return
	}
	res := fieldsResponse{}
	project, err := model.LoadProject(s.dir)
	var diags model.Errors
	if (err != nil && !errors.As(err, &diags)) || diags.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "the project has errors; check project.yaml")
		return
	}
	var env *model.Environment
	if project != nil {
		if env, err = project.SelectEnvironment(""); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if env != nil {
		res.Environment = env.Name
	}
	with, err := pipeline.ExpandConfig(t.With, model.PipelineScope(p, project, env, nil))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the configuration of "+t.ID+" cannot be resolved: "+err.Error())
		return
	}
	res.Variables = hasVariables(t.With)
	if res.Detection, err = def.DetectFields(paths.WithBase(r.Context(), s.dir), with, transform.DetectLimit); err != nil {
		problem := fieldsProblem{Error: err.Error(), BaseDir: filepath.Clean(s.dir)}
		writeJSON(w, http.StatusUnprocessableEntity, problem)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// hasVariables reports ${…} references in a configuration.
func hasVariables(v any) bool {
	switch v := v.(type) {
	case string:
		return strings.Contains(v, "${")
	case map[string]any:
		for _, e := range v {
			if hasVariables(e) {
				return true
			}
		}
	case []any:
		for _, e := range v {
			if hasVariables(e) {
				return true
			}
		}
	}
	return false
}
