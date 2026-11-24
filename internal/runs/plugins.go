package runs

import (
	"context"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/plugin"
	"github.com/runnelhq/runnel/internal/vars"
)

// WithPlugins returns reg with the action types of the trusted plugins of the project in
// projectDir and hints for those that cannot be used (ADR 0019, S49b2); reg itself
// without a project, without plugins or without a configuration folder for approvals.
func WithPlugins(reg *action.Registry, projectDir string) *action.Registry {
	if reg == nil || projectDir == "" {
		return reg
	}
	store, err := plugin.DefaultStore()
	if err != nil {
		return reg
	}
	return plugin.Registry(context.Background(), reg, projectDir, store)
}

// pluginSession runs the actions of plugins of a run; nil without a project.
func pluginSession(projectDir, environment string, mask *vars.Mask) action.PluginRunner {
	if projectDir == "" {
		return nil
	}
	store, err := plugin.DefaultStore()
	if err != nil {
		return nil
	}
	var m func(string) string
	if mask != nil {
		m = mask.String
	}
	return plugin.NewSession(projectDir, environment, store, m)
}
