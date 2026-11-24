package runs

import (
	"context"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/conntest"
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

// ConnectionTypes returns the built-in connection types with those of the trusted plugins
// of the project in projectDir and hints for plugins that cannot be used (S49c).
func ConnectionTypes(projectDir string) *connection.Registry {
	if projectDir == "" {
		return builtinConnectionTypes
	}
	store, err := plugin.DefaultStore()
	if err != nil {
		return builtinConnectionTypes
	}
	return plugin.ConnectionTypes(context.Background(), builtinConnectionTypes, projectDir, store)
}

// TestConnection tests a resolved connection of the project in projectDir: built-in types
// with their own checks, types of plugins with connection.test of the plugin (S49c).
func TestConnection(ctx context.Context, projectDir string, c connection.Resolved, mask *vars.Mask) (string, error) {
	if _, ok := builtinConnectionTypes.Lookup(c.Type); ok || projectDir == "" {
		return conntest.Run(ctx, c)
	}
	store, err := plugin.DefaultStore()
	if err != nil {
		return "", err
	}
	var m func(string) string
	if mask != nil {
		m = mask.String
	}
	return plugin.TestConnection(ctx, projectDir, store, c, m)
}
