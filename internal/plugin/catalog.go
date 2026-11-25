package plugin

import (
	"context"
	"errors"
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
	"github.com/runnelhq/runnel/internal/transform"
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
	tdefs       []transform.Definition
	conns       []connection.Definition
	tests       map[string]bool // connection types the plugin can test
	desc        Description     // its types as described, for the editor (S52a)
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

// Transforms returns base with the transform types of the trusted plugins of the project
// in projectDir and hints for the namespaces of plugins that cannot be used (S50a).
func Transforms(ctx context.Context, base *transform.Registry, projectDir string, store plugintrust.Store) *transform.Registry {
	if projectDir == "" {
		return base
	}
	plugins, _ := model.LoadPlugins(projectDir)
	if len(plugins) == 0 {
		return base
	}
	var defs []transform.Definition
	hints := map[string]string{}
	for _, name := range slices.Sorted(mapKeys(plugins)) {
		e := lookup(ctx, projectDir, plugins[name], store)
		defs = append(defs, e.tdefs...)
		if e.hint != "" {
			hints[plugins[name].Namespace] = e.hint
		}
	}
	return base.With(defs, hints)
}

// ConnectionTypes returns base with the connection types of the trusted plugins of the
// project in projectDir and hints for the namespaces of plugins that cannot be used (S49c).
func ConnectionTypes(ctx context.Context, base *connection.Registry, projectDir string, store plugintrust.Store) *connection.Registry {
	if projectDir == "" {
		return base
	}
	plugins, _ := model.LoadPlugins(projectDir)
	if len(plugins) == 0 {
		return base
	}
	var defs []connection.Definition
	hints := map[string]string{}
	for _, name := range slices.Sorted(mapKeys(plugins)) {
		e := lookup(ctx, projectDir, plugins[name], store)
		defs = append(defs, e.conns...)
		if e.hint != "" {
			hints[plugins[name].Namespace] = e.hint
		}
	}
	return base.With(defs, hints)
}

// TestConnection tests a resolved connection of a type of a plugin with connection.test:
// the plugin of the type starts (after checking its approval again) and ends afterwards.
func TestConnection(ctx context.Context, projectDir string, store plugintrust.Store, c connection.Resolved, mask func(string) string) (string, error) {
	plugins, _ := model.LoadPlugins(projectDir)
	for _, name := range slices.Sorted(mapKeys(plugins)) {
		p := plugins[name]
		if c.Type != p.Namespace && !strings.HasPrefix(c.Type, p.Namespace+"-") {
			continue
		}
		e := lookup(ctx, projectDir, p, store)
		if e.hint != "" {
			return "", errors.New(e.hint)
		}
		if !e.tests[c.Type] {
			return "", fmt.Errorf("connections of type %s cannot be tested (plugin %s offers no test)", c.Type, p.Name)
		}
		pr, err := Start(ctx, p, Options{ProjectDir: projectDir, RunnelVersion: version.Version, Env: environ(projectDir), Mask: mask})
		if err != nil {
			return "", err
		}
		defer func() { _ = pr.Close() }()
		resolved := map[string]any{"name": c.Name, "type": c.Type}
		maps.Copy(resolved, c.With)
		return pr.TestConnection(ctx, resolved)
	}
	return "", fmt.Errorf("connections of type %s cannot be tested yet", c.Type)
}

// Types returns the types of a plugin as the catalog knows them, and why it cannot be
// used (not trusted, changed, failing to start); a trusted plugin is started for describe
// unless its types are cached (S52a).
func Types(ctx context.Context, projectDir string, p *model.Plugin, store plugintrust.Store) (Description, string) {
	e := lookup(ctx, projectDir, p, store)
	return e.desc, e.hint
}

// states caches the approval of plugins by fingerprint (names, sizes and modification
// times of their files and of the files of approvals), so that showing the plugins does
// not hash their folders on every request (S52a).
var states = struct {
	sync.Mutex
	m map[string]stateEntry
}{m: map[string]stateEntry{}}

type stateEntry struct {
	fingerprint string
	status      plugintrust.Status
}

// Check returns the approval of a plugin like store.Check, hashing its folder only when
// a file of it or of the approvals changed.
func Check(projectDir string, p *model.Plugin, store plugintrust.Store) (plugintrust.Status, error) {
	dir, _ := filepath.Abs(p.Dir)
	key := projectDir + "\x00" + dir
	fp := fingerprint(p.Dir, store)
	states.Lock()
	e, ok := states.m[key]
	states.Unlock()
	if ok && e.fingerprint == fp {
		return e.status, nil
	}
	st, err := store.Check(projectDir, p.Name, p.Dir)
	if err != nil {
		return st, err
	}
	states.Lock()
	if len(states.m) >= 1024 {
		clear(states.m)
	}
	states.m[key] = stateEntry{fingerprint: fp, status: st}
	states.Unlock()
	return st, nil
}

// Forget drops what the catalog knows about a plugin, so that the next use checks and
// describes it again, e.g. after a plugin failed to start ("Try again" in the editor).
func Forget(p *model.Plugin) {
	key, _ := filepath.Abs(p.Dir)
	catalog.Lock()
	delete(catalog.entries, key)
	catalog.Unlock()
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
		if err = describe(ctx, projectDir, p, store, &e); err != nil {
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

// describe starts a trusted plugin, reads its types into e and ends it again.
func describe(ctx context.Context, projectDir string, p *model.Plugin, store plugintrust.Store, e *entry) error {
	pr, err := Start(ctx, p, Options{ProjectDir: projectDir, RunnelVersion: version.Version, Env: environ(projectDir)})
	if err != nil {
		return err
	}
	defer func() { _ = pr.Close() }()
	d, err := pr.Describe(ctx)
	if err != nil {
		return err
	}
	e.desc = d
	for _, a := range d.Actions {
		e.defs = append(e.defs, definition(p.Name, a))
	}
	for _, t := range d.Transforms {
		e.tdefs = append(e.tdefs, transformDefinition(projectDir, p, store, t))
	}
	e.tests = map[string]bool{}
	for _, c := range d.Connections {
		e.conns = append(e.conns, connection.Definition{Type: c.Type, Label: c.Label, Description: c.Description, Schema: c.Schema, Plugin: p.Name})
		e.tests[c.Type] = c.Test
	}
	return nil
}

// definition is the action type of a plugin: the configuration is checked against the
// schema of describe, the action runs in the process of the plugin for the run.
func definition(plugin string, d action.Descriptor) action.Definition {
	return action.Definition{
		Type: d.Type, Category: d.Category, Plugin: plugin, Label: d.Label, Description: d.Description, Example: d.Example, Schema: d.Schema,
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
	config, err := withConnections(ctx, a.schema, a.with, ec.Connection)
	if err != nil {
		return action.Result{}, err
	}
	out, err := ec.Plugins.Execute(ctx, a.plugin, a.typ, config, ec)
	if err != nil {
		return action.Result{}, err
	}
	return action.Result{Output: out}, nil
}

// withConnections returns the configuration with the connections it names resolved:
// fields of format runnel-connection get {"name", "type", …fields} (ADR 0019).
func withConnections(ctx context.Context, sc *schema.Schema, with map[string]any, resolve connection.Resolver) (map[string]any, error) {
	config := maps.Clone(with)
	for _, prop := range sc.Properties {
		name, ok := config[prop.Name].(string)
		if prop.Schema == nil || prop.Schema.Format != schema.FormatConnection || !ok || name == "" {
			continue
		}
		if resolve == nil {
			return nil, connection.ErrNotAvailable
		}
		c, err := resolve(ctx, name)
		if err != nil {
			return nil, err
		}
		resolved := map[string]any{"name": c.Name, "type": c.Type}
		maps.Copy(resolved, c.With)
		config[prop.Name] = resolved
	}
	return config, nil
}

// transformDefinition is the transform type of a plugin: the configuration is checked
// against the schema of describe, the transform runs as a stream in the process of the
// plugin for the run (S50a).
func transformDefinition(projectDir string, p *model.Plugin, store plugintrust.Store, tt TransformType) transform.Definition {
	d := tt.Descriptor
	def := transform.Definition{
		Type: d.Type, Category: d.Category, Plugin: p.Name, Label: d.Label, Description: d.Description, Example: d.Example, Schema: d.Schema,
		RecordErrors: d.RecordErrors, Parallel: d.Parallel,
		New: func(with map[string]any) (transform.Transform, error) {
			if err := schema.CheckConfig(d.Schema, with); err != nil {
				return nil, err
			}
			return &pluginTransform{plugin: p.Name, typ: d.Type, with: with, schema: d.Schema,
				probe: prober{projectDir: projectDir, plugin: p, store: store}, init: tt.Init}, nil
		},
	}
	if d.DetectFields {
		pb := prober{projectDir: projectDir, plugin: p, store: store}
		def.DetectFields = func(ctx context.Context, with map[string]any, limit int) (*transform.Detection, error) {
			return pb.detectFields(ctx, d.Type, d.Schema, with, limit)
		}
	}
	return def
}

// pluginTransform is a configured transform of a plugin.
type pluginTransform struct {
	plugin string
	typ    string
	with   map[string]any
	schema *schema.Schema
	probe  prober
	init   bool // the plugin answers transform.init
}

// Process runs the transform as a stream in the plugin, with the connections it names
// resolved; it may emit to the transforms named in fields of format runnel-target.
func (t *pluginTransform) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if out.Plugins() == nil {
		return fmt.Errorf("transforms of plugins cannot run here")
	}
	config, err := withConnections(ctx, t.schema, t.with, out.Connection)
	if err != nil {
		return err
	}
	var targets []string
	for _, prop := range t.schema.Properties {
		if prop.Schema == nil || prop.Schema.Format != schema.FormatTarget {
			continue
		}
		switch v := t.with[prop.Name].(type) {
		case string:
			if v != "" {
				targets = append(targets, v)
			}
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok && s != "" {
					targets = append(targets, s)
				}
			}
		}
	}
	return out.Plugins().Stream(ctx, t.plugin, t.typ, config, targets, in, out)
}
