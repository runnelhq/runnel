package model

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// Plugins (ADR 0019): actions and transforms in their own processes, one folder per
// plugin under plugins/<name>/ of a project with a plugin.yaml. Which types a plugin
// offers it says itself (describe, S49b); plugin.yaml only names its namespace.
const (
	KindPlugin     = "Plugin"
	PluginsDir     = "plugins"
	PluginFileName = "plugin.yaml"
)

// pluginOS are the keys of executable as a mapping: the operating systems of Go.
var pluginOS = []string{"darwin", "linux", "windows"}

var (
	pluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	semverPattern     = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// Plugin is the content of plugins/<name>/plugin.yaml.
type Plugin struct {
	Kind        string `yaml:"kind" json:"kind" doc:"Type of the file" schema:"required,enum=Plugin"`
	APIVersion  string `yaml:"apiVersion" json:"apiVersion" doc:"Version of the file format" schema:"required,enum=runnel/v1"`
	Name        string `yaml:"name" json:"name" doc:"Name of the plugin; must match its folder under plugins/" schema:"required,example=iban"`
	Version     string `yaml:"version" json:"version" doc:"Version of the plugin (SemVer), e.g. 1.2.0" schema:"required,example=1.0.0"`
	Description string `yaml:"description,omitempty" json:"description,omitempty" doc:"What the plugin does"`
	Namespace   string `yaml:"namespace" json:"namespace" doc:"Namespace of the types the plugin offers, e.g. iban for iban.validate; not one of Runnel's own (core, file, http, …)" schema:"required,example=iban"`
	// Executable is a path or a mapping by operating system (windows, linux, darwin).
	Executable any      `yaml:"executable" json:"executable" doc:"Program of the plugin, relative to its folder (e.g. bin/iban.exe), a command of the computer (e.g. python), or a mapping by operating system: windows, linux, darwin" schema:"required"`
	Args       []string `yaml:"args,omitempty" json:"args,omitempty" doc:"Arguments of the program, e.g. [main.py] for a script; files in the folder of the plugin are relative to it"`

	// Dir is the folder of the plugin.
	Dir string `yaml:"-" json:"-"`

	src *source
}

// ReadPlugin parses and checks a plugin.yaml; name is the path of the file, used in
// messages and to compare the name of the plugin with its folder.
func ReadPlugin(name string, data []byte) (*Plugin, error) {
	src, err := parseSource(name, data)
	if err != nil {
		return nil, err
	}
	p := &Plugin{src: src, Dir: filepath.Dir(filepath.FromSlash(name))}
	if err := src.decode(p); err != nil {
		return nil, err
	}
	errs := checkHeader(src, p.Kind, KindPlugin, p.APIVersion, p.Name)
	if folder := filepath.Base(p.Dir); p.Name != "" && p.Name != folder {
		errs = append(errs, src.errorf("$.name", "plugin name %q must match its folder (%q)", p.Name, folder))
	} else if p.Name != "" && !pluginNamePattern.MatchString(p.Name) {
		errs = append(errs, src.errorf("$.name", "invalid plugin name %q (lowercase letters, digits and -, starting with a letter)", p.Name))
	}
	switch {
	case p.Version == "":
		errs = append(errs, src.errorf("$", `missing required field "version" (e.g. 1.0.0)`))
	case !semverPattern.MatchString(p.Version):
		errs = append(errs, src.errorf("$.version", "invalid version %q (expected SemVer, e.g. 1.2.0)", p.Version))
	}
	switch {
	case p.Namespace == "":
		errs = append(errs, src.errorf("$", `missing required field "namespace" (e.g. the name of the plugin)`))
	case !pluginNamePattern.MatchString(p.Namespace):
		errs = append(errs, src.errorf("$.namespace", "invalid namespace %q (lowercase letters, digits and -, starting with a letter)", p.Namespace))
	case IsReservedNamespace(p.Namespace):
		errs = append(errs, src.errorf("$.namespace", "namespace %q is reserved for Runnel's own actions and transforms", p.Namespace))
	}
	errs = append(errs, p.checkExecutable()...)
	return p, errs.errOrNil()
}

// checkExecutable checks executable: a text or a mapping by operating system, each a
// path inside the folder of the plugin, an absolute path or a command name.
func (p *Plugin) checkExecutable() Errors {
	var errs Errors
	check := func(path, value string) {
		switch {
		case value == "":
			errs = append(errs, p.src.errorf(path, "executable must not be empty"))
		case !filepath.IsAbs(value) && escapes(value):
			errs = append(errs, p.src.errorf(path, "executable %q must stay inside the folder of the plugin (no ..)", value))
		}
	}
	switch v := p.Executable.(type) {
	case nil:
		errs = append(errs, p.src.errorf("$", `missing required field "executable" (e.g. bin/plugin.exe)`))
	case string:
		check("$.executable", v)
	case map[string]any:
		if len(v) == 0 {
			errs = append(errs, p.src.errorf("$.executable", "executable needs at least one operating system (%s)", strings.Join(pluginOS, ", ")))
		}
		for _, os := range slices.Sorted(maps.Keys(v)) {
			s, ok := v[os].(string)
			switch {
			case !slices.Contains(pluginOS, os):
				errs = append(errs, p.src.errorf("$.executable."+os, "unknown operating system %q (expected %s)", os, strings.Join(pluginOS, ", ")))
			case !ok:
				errs = append(errs, p.src.errorf("$.executable."+os, "executable for %s must be a text", os))
			default:
				check("$.executable."+os, s)
			}
		}
	default:
		errs = append(errs, p.src.errorf("$.executable", "executable must be a text or a mapping by operating system (%s)", strings.Join(pluginOS, ", ")))
	}
	return errs
}

// escapes reports whether a relative path leaves its folder.
func escapes(rel string) bool {
	clean := filepath.Clean(filepath.FromSlash(rel))
	return clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// ExecutableFor returns the executable of the plugin for an operating system (runtime.GOOS
// if empty); false if the plugin has none for it.
func (p *Plugin) ExecutableFor(goos string) (string, bool) {
	if goos == "" {
		goos = runtime.GOOS
	}
	switch v := p.Executable.(type) {
	case string:
		return v, v != ""
	case map[string]any:
		s, ok := v[goos].(string)
		return s, ok && s != ""
	}
	return "", false
}

// File returns the path of plugin.yaml as read.
func (p *Plugin) File() string { return p.src.name }

// ErrorAt returns an error at a path of plugin.yaml.
func (p *Plugin) ErrorAt(path, format string, args ...any) *Error {
	return p.src.errorf(path, format, args...)
}

// LoadPlugins reads plugins/*/plugin.yaml of a project. Problems of all plugins are
// returned at once as Errors, the plugins without errors by name; a namespace claimed by
// two plugins is an error at both, and neither is loaded.
func LoadPlugins(dir string) (map[string]*Plugin, error) {
	files, err := filepath.Glob(filepath.Join(dir, PluginsDir, "*", PluginFileName))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var errs Errors
	var plugins []*Plugin
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		p, err := ReadPlugin(filepath.ToSlash(file), data)
		if es, ok := err.(Errors); ok { //nolint:errorlint // ReadPlugin returns Errors itself
			errs = append(errs, es...)
			if es.HasErrors() {
				continue
			}
		} else if err != nil {
			return nil, err
		}
		plugins = append(plugins, p)
	}
	out := map[string]*Plugin{}
	for _, p := range plugins {
		var others []string
		for _, q := range plugins {
			if q != p && q.Namespace == p.Namespace {
				others = append(others, q.Name)
			}
		}
		if len(others) > 0 {
			errs = append(errs, p.ErrorAt("$.namespace", "namespace %q is also used by plugin %s; each plugin needs its own namespace", p.Namespace, strings.Join(others, ", ")))
			continue
		}
		out[p.Name] = p
	}
	return out, errs.errOrNil()
}

// PluginNames returns the names of the plugins of the project without errors, sorted.
func (p *Project) PluginNames() []string {
	return slices.Sorted(maps.Keys(p.Plugins))
}
