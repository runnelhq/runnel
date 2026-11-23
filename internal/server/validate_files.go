package server

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/runs"
	"github.com/runnelhq/runnel/internal/triggers"
)

// validateText checks project.yaml, an environment or a trigger file from the text in the
// editor (E10), like "runnel validate": fields and, for a trigger, its target, environment
// and parameters against the project and its name against the other trigger files.
func (s *Server) validateText(kind, name string, data []byte) (model.Errors, error) {
	var err error
	switch kind {
	case "project":
		exists := func(env string) bool {
			_, err := os.Stat(filepath.Join(s.dir, model.EnvironmentsDir, env+".yaml"))
			return err == nil
		}
		var p *model.Project
		if p, err = model.ReadProject(name, data, exists); err == nil {
			return p.InlineSecretWarnings(), nil
		}
	case "environment":
		var env *model.Environment
		if env, err = model.ReadEnvironment(name, data); err == nil {
			return env.InlineSecretWarnings(), nil
		}
	case "connection":
		var c *model.Connection
		if c, err = model.ReadConnection(name, data); err == nil {
			p, _ := model.LoadProject(s.dir)
			diags := runs.CheckConnection(c, p, "")
			diags.Sort()
			return diags, nil
		}
	case "trigger":
		var t *model.Trigger
		if t, err = model.ParseTrigger(name, data); err == nil {
			return s.checkTrigger(t, name), nil
		}
	}
	if diags, ok := errors.AsType[model.Errors](err); ok {
		diags.Sort()
		return diags, nil
	}
	return nil, err
}

// checkTrigger checks a trigger against the project (a project with errors is left out,
// as in the scheduler) and its name against the saved trigger files.
func (s *Server) checkTrigger(t *model.Trigger, name string) model.Errors {
	p, _ := model.LoadProject(s.dir)
	diags := triggers.CheckTrigger(t, p, s.dir)
	others, _ := model.LoadTriggers(s.dir)
	for _, o := range others {
		if o.Name != t.Name {
			continue
		}
		if rel, err := filepath.Rel(s.dir, o.File()); err == nil && filepath.ToSlash(rel) != name {
			diags = append(diags, t.ErrorAt("$.name", "trigger name %q is already used by %s", t.Name, model.TriggerFileName(s.dir, o.File())))
			break
		}
	}
	diags.Sort()
	return diags
}
