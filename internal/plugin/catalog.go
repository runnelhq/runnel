package plugin

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugintrust"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/version"
	"github.com/runnelhq/runnel/internal/winsvc"
)

// DefaultStore returns the approvals of this computer (user and Windows service).
func DefaultStore() (plugintrust.Store, error) {
	return plugintrust.DefaultStore(winsvc.TrustDir())
}

// entry is what the catalog knows about a plugin as long as its files and the approvals
// do not change: its action types, or why it cannot be used.
type entry struct {
	fingerprint string
	defs        []action.Definition
	hint        string
}

// catalog caches the types of plugins by folder, so that the hash of a plugin and its
// describe run only when files of the plugin or the approvals change (ADR 0019).
var catalog = struct {
	sync.Mutex
	entries map[string]entry
}{entries: map[string]entry{}}

// Registry returns base with the action types of the trusted plugins of the project in
// projectDir (describe) and hints for the namespaces of plugins that cannot be used: not
// trusted, changed since trusted, or failing to start. Without plugins it returns base.
func Registry(ctx context.Context, base *action.Registry, projectDir string, store plugintrust.Store) *action.Registry {
	if projectDir == "" {
		return base
	}
	plugins, _ := model.LoadPlugins(projectDir) // problems of the files are reported with the project
	if len(plugins) == 0 {
		return base
	}
	var defs []action.Definition
	hints := map[string]string{}
	for _, name := range slices.Sorted(mapKeys(plugins)) {
		e := lookup(ctx, projectDir, plugins[name], store)
		defs = append(defs, e.defs...)
		if e.hint != "" {
			hints[plugins[name].Namespace] = e.hint
		}
	}
	return base.With(defs, hints)
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// lookup returns the cached entry of a plugin, or checks its approval and describes it.
func lookup(ctx context.Context, projectDir string, p *model.Plugin, store plugintrust.Store) entry {
	key, _ := filepath.Abs(p.Dir)
	fp := fingerprint(p.Dir, store)
	catalog.Lock()
	e, ok := catalog.entries[key]
	catalog.Unlock()
	if ok && e.fingerprint == fp {
		return e
	}
	e = entry{fingerprint: fp}
	st, err := store.Check(projectDir, p.Name, p.Dir)
	switch {
	case err != nil:
		e.hint = fmt.Sprintf("plugin %q cannot be checked: %v", p.Name, err)
	case st.State == plugintrust.NotTrusted:
		e.hint = fmt.Sprintf(`plugin %q is not trusted on this computer; check it and run "runnel plugins trust"`, p.Name)
	case st.State == plugintrust.Changed:
		e.hint = fmt.Sprintf(`plugin %q changed since it was trusted; check it and run "runnel plugins trust" again`, p.Name)
	default:
		e.defs, err = describe(ctx, projectDir, p)
		if err != nil {
			e.hint = err.Error()
		}
	}
	catalog.Lock()
	catalog.entries[key] = e
	catalog.Unlock()
	return e
}

// trusted reports whether a plugin may start now: the cached entry, checked again
// against its files and the approvals (they may have changed since the registry was made).
func trusted(ctx context.Context, projectDir string, p *model.Plugin, store plugintrust.Store) error {
	if e := lookup(ctx, projectDir, p, store); e.hint != "" {
		return fmt.Errorf("%s", e.hint)
	}
	return nil
}

// fingerprint changes when a file of the plugin or a file of approvals changes: names,
// sizes and modification times, without reading the files.
func fingerprint(dir string, store plugintrust.Store) string {
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a vanished file changes the fingerprint anyway
		}
		if info, err := d.Info(); err == nil {
			b.WriteString(path + "|" + strconv.FormatInt(info.Size(), 10) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n")
		}
		return nil
	})
	for _, f := range []string{store.User, store.Service} {
		if info, err := os.Stat(f); err == nil {
			b.WriteString(f + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n")
		}
	}
	return b.String()
}

// describe starts a trusted plugin, reads its action types and ends it again.
func describe(ctx context.Context, projectDir string, p *model.Plugin) ([]action.Definition, error) {
	pr, err := Start(ctx, p, Options{ProjectDir: projectDir, RunnelVersion: version.Version, Env: environ(projectDir)})
	if err != nil {
		return nil, err
	}
	defer func() { _ = pr.Close() }()
	ds, err := pr.Describe(ctx)
	if err != nil {
		return nil, err
	}
	defs := make([]action.Definition, len(ds))
	for i, d := range ds {
		defs[i] = definition(p.Name, d)
	}
	return defs, nil
}

// definition is the action type of a plugin: the configuration is checked against the
// schema of describe, the action runs in the process of the plugin for the run.
func definition(plugin string, d action.Descriptor) action.Definition {
	category := d.Category
	if !slices.Contains(action.Categories, category) {
		category = action.CategoryGeneral
	}
	return action.Definition{
		Type: d.Type, Category: category, Label: d.Label, Description: d.Description, Example: d.Example, Schema: d.Schema,
		New: func(with map[string]any) (action.Action, error) {
			if err := schema.CheckConfig(d.Schema, with); err != nil {
				return nil, err
			}
			return &pluginAction{plugin: plugin, typ: d.Type, with: with, schema: d.Schema}, nil
		},
	}
}

// pluginAction is a configured action of a plugin.
type pluginAction struct {
	plugin string
	typ    string
	with   map[string]any
	schema *schema.Schema
}

// Execute passes the configuration to the plugin, with the connections it names resolved
// (fields of format runnel-connection, ADR 0019 Nachtrag 2026-11-24).
func (a *pluginAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if ec.Plugins == nil {
		return action.Result{}, fmt.Errorf("actions of plugins cannot run here")
	}
	config := maps.Clone(a.with)
	for _, prop := range a.schema.Properties {
		name, ok := config[prop.Name].(string)
		if prop.Schema == nil || prop.Schema.Format != "runnel-connection" || !ok || name == "" {
			continue
		}
		if ec.Connection == nil {
			return action.Result{}, connection.ErrNotAvailable
		}
		c, err := ec.Connection(ctx, name)
		if err != nil {
			return action.Result{}, err
		}
		resolved := map[string]any{"name": c.Name, "type": c.Type}
		maps.Copy(resolved, c.With)
		config[prop.Name] = resolved
	}
	out, err := ec.Plugins.Execute(ctx, a.plugin, a.typ, config, ec)
	if err != nil {
		return action.Result{}, err
	}
	return action.Result{Output: out}, nil
}
