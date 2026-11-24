package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pluginHead = "kind: Plugin\napiVersion: runnel/v1\nname: iban\nversion: 1.0.0\n"

func writePlugin(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, PluginsDir, name, PluginFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plugin.yaml: name like the folder, SemVer, a namespace of its own, executable as text
// or by operating system inside the folder of the plugin (ADR 0019).
func TestReadPlugin(t *testing.T) {
	file := "plugins/iban/plugin.yaml"
	ok := pluginHead + "namespace: iban\nexecutable:\n  windows: bin/iban.exe\n  linux: bin/iban\nargs: [--quiet]\n"
	p, err := ReadPlugin(file, []byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if exe, found := p.ExecutableFor("windows"); !found || exe != "bin/iban.exe" {
		t.Errorf("windows: %q %v", exe, found)
	}
	if _, found := p.ExecutableFor("darwin"); found {
		t.Error("darwin has no executable")
	}
	if p.Dir != filepath.FromSlash("plugins/iban") || p.File() != file {
		t.Errorf("dir %q file %q", p.Dir, p.File())
	}
	script, err := ReadPlugin(file, []byte(pluginHead+"namespace: iban\nexecutable: python\nargs: [main.py]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if exe, _ := script.ExecutableFor("linux"); exe != "python" {
		t.Errorf("script %q", exe)
	}

	for _, tt := range []struct{ body, want string }{
		{"kind: Plugin\napiVersion: runnel/v1\nname: other\nversion: 1.0.0\nnamespace: iban\nexecutable: x\n", `plugin name "other" must match its folder ("iban")`},
		{"kind: Plugin\napiVersion: runnel/v1\nname: iban\nnamespace: iban\nexecutable: x\n", `missing required field "version"`},
		{pluginHead[:len(pluginHead)-len("version: 1.0.0\n")] + "version: v1\nnamespace: iban\nexecutable: x\n", `invalid version "v1"`},
		{pluginHead + "executable: x\n", `missing required field "namespace"`},
		{pluginHead + "namespace: file\nexecutable: x\n", `namespace "file" is reserved`},
		{pluginHead + "namespace: mcp-github\nexecutable: x\n", `namespace "mcp-github" is reserved`},
		{pluginHead + "namespace: Iban\nexecutable: x\n", `invalid namespace "Iban"`},
		{pluginHead + "namespace: iban\n", `missing required field "executable"`},
		{pluginHead + "namespace: iban\nexecutable: ../other/x.exe\n", "must stay inside the folder of the plugin"},
		{pluginHead + "namespace: iban\nexecutable:\n  windows: bin/x.exe\n  plan9: bin/x\n", `unknown operating system "plan9"`},
		{pluginHead + "namespace: iban\nexecutable: [a, b]\n", "executable must be a text or a mapping"},
		{"kind: Workflow\napiVersion: runnel/v1\nname: iban\nversion: 1.0.0\nnamespace: iban\nexecutable: x\n", `kind must be "Plugin"`},
	} {
		_, err := ReadPlugin(file, []byte(tt.body))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q: %v, want %q", tt.body, err, tt.want)
		}
	}
}

// LoadPlugins reads all plugins of a project; two plugins with one namespace are both
// refused, the others load (ADR 0013).
func TestLoadPlugins(t *testing.T) {
	dir := t.TempDir()
	plugin := func(name, ns string) string {
		return "kind: Plugin\napiVersion: runnel/v1\nname: " + name + "\nversion: 1.0.0\nnamespace: " + ns + "\nexecutable: x\n"
	}
	writePlugin(t, dir, "iban", plugin("iban", "iban"))
	writePlugin(t, dir, "a", plugin("a", "shared"))
	writePlugin(t, dir, "b", plugin("b", "shared"))
	writePlugin(t, dir, "broken", "kind: Plugin\n")
	if err := os.MkdirAll(filepath.Join(dir, PluginsDir, "empty"), 0o755); err != nil { // no plugin.yaml: not a plugin
		t.Fatal(err)
	}
	plugins, err := LoadPlugins(dir)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	for _, want := range []string{`plugins/a/plugin.yaml:5:12: namespace "shared" is also used by plugin b`, `namespace "shared" is also used by plugin a`, "plugins/broken/plugin.yaml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in\n%s", want, msg)
		}
	}
	if len(plugins) != 1 || plugins["iban"] == nil {
		t.Errorf("plugins %v", plugins)
	}

	// The project has the plugins without errors.
	if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte("kind: Project\napiVersion: runnel/v1\nname: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := LoadProject(dir)
	if p == nil || strings.Join(p.PluginNames(), ",") != "iban" {
		t.Errorf("project plugins %v", p)
	}
}

// FuzzReadPlugin checks that no plugin.yaml makes ReadPlugin panic.
func FuzzReadPlugin(f *testing.F) {
	f.Add([]byte(pluginHead + "namespace: iban\nexecutable:\n  windows: bin/iban.exe\nargs: [a]\n"))
	f.Add([]byte(pluginHead + "namespace: iban\nexecutable: [1, {a: b}]\n"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		if p, err := ReadPlugin("plugins/iban/plugin.yaml", data); err == nil {
			_, _ = p.ExecutableFor("windows")
		}
	})
}
