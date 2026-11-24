package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/plugin/plugintest"
)

func pluginProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\n")
	writeFile(t, filepath.Join(dir, "plugins", "iban", "plugin.yaml"),
		"kind: Plugin\napiVersion: runnel/v1\nname: iban\nversion: 1.0.0\nnamespace: iban\nexecutable:\n  windows: bin/iban.exe\n  linux: bin/iban\n  darwin: bin/iban\n")
	writeFile(t, filepath.Join(dir, "plugins", "iban", "bin", "iban.exe"), "program")
	writeFile(t, filepath.Join(dir, "plugins", "broken", "plugin.yaml"), "kind: Plugin\napiVersion: runnel/v1\nname: broken\nversion: 1\nnamespace: core\nexecutable: x\n")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runnel plugins lists the plugins of a project with their approval; trust with --yes
// approves them, a changed file voids the approval, untrust withdraws it (ADR 0019).
func TestPlugins(t *testing.T) {
	dir := pluginProject(t)
	status := func() string {
		t.Helper()
		code, out, errOut := run(t, "plugins", dir)
		if code != 0 {
			t.Fatalf("plugins: %d %s", code, errOut)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "iban ") {
				return strings.Join(strings.Fields(line)[4:], " ")
			}
		}
		t.Fatalf("no iban in\n%s", out)
		return ""
	}
	code, out, _ := run(t, "plugins", dir)
	// Broken plugins are reported with their position and not listed.
	for _, want := range []string{`plugins/broken/plugin.yaml:4:10: error: invalid version "1"`, `namespace "core" is reserved`, "NAME  VERSION  NAMESPACE  PROGRAM", "iban  1.0.0    iban       bin/iban"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if s := status(); s != "not trusted" {
		t.Errorf("status %q", s)
	}

	if code, out, errOut := run(t, "plugins", "trust", dir, "--plugin", "iban", "--yes"); code != 0 || !strings.Contains(out, "hash:    sha256:") || !strings.Contains(out, "Plugin iban is trusted.") {
		t.Fatalf("trust: %d %s %s", code, out, errOut)
	}
	if s := status(); s != "trusted" {
		t.Errorf("status %q", s)
	}
	if _, out, _ := run(t, "plugins", "trust", dir, "--plugin", "iban", "--yes"); !strings.Contains(out, "Plugin iban is already trusted.") {
		t.Errorf("trust again: %s", out)
	}

	writeFile(t, filepath.Join(dir, "plugins", "iban", "bin", "iban.exe"), "other program")
	if s := status(); s != "changed since trusted" {
		t.Errorf("status %q", s)
	}

	// Asked without --yes: anything but y keeps it untrusted.
	var buf bytes.Buffer
	if err := trustPlugins(&buf, strings.NewReader("n\n"), dir, pluginsOptions{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Files of the plugin changed since it was trusted.", "runs with the rights of Runnel", "Trust plugin iban? [y/N]", "Plugin iban is not trusted."} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in\n%s", want, buf.String())
		}
	}
	buf.Reset()
	if err := trustPlugins(&buf, strings.NewReader("y\n"), dir, pluginsOptions{}, time.Now()); err != nil || !strings.Contains(buf.String(), "Plugin iban is trusted.") {
		t.Errorf("yes: %v %s", err, buf.String())
	}

	if code, out, _ := run(t, "plugins", "untrust", dir); code != 0 || !strings.Contains(out, "Plugin iban is no longer trusted.") {
		t.Errorf("untrust: %d %s", code, out)
	}
	if s := status(); s != "not trusted" {
		t.Errorf("status %q", s)
	}
}

// Unknown plugins, projects without plugins and folders without a project.
func TestPluginsErrors(t *testing.T) {
	dir := pluginProject(t)
	if code, _, errOut := run(t, "plugins", "trust", dir, "--plugin", "nope", "--yes"); code == 0 || !strings.Contains(errOut, `plugin "nope" not found`) {
		t.Errorf("unknown plugin: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "plugins", "trust", dir, "--plugin", "broken", "--yes"); code == 0 || !strings.Contains(errOut, `plugin "broken" not found (or not loaded)`) {
		t.Errorf("broken plugin: %d %s", code, errOut)
	}
	empty := t.TempDir()
	writeFile(t, filepath.Join(empty, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: empty\n")
	if code, out, _ := run(t, "plugins", empty); code != 0 || !strings.Contains(out, "No plugins in") {
		t.Errorf("no plugins: %d %s", code, out)
	}
	if code, _, errOut := run(t, "plugins", t.TempDir()); code == 0 || !strings.Contains(errOut, "no project (project.yaml) found") {
		t.Errorf("no project: %d %s", code, errOut)
	}
}

// Trusted plugins are started to list their types; untrusted ones are not (S49b1).
func TestPluginTypes(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(plugintest.ModeEnv, "normal")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\n")
	writeFile(t, filepath.Join(dir, "plugins", "demo", "plugin.yaml"),
		"kind: Plugin\napiVersion: runnel/v1\nname: demo\nversion: 1.0.0\nnamespace: demo\nexecutable: '"+exe+"'\n")
	if _, out, _ := run(t, "plugins", dir); strings.Contains(out, "Types of") {
		t.Errorf("untrusted plugin started:\n%s", out)
	}
	if code, _, errOut := run(t, "plugins", "trust", dir, "--yes"); code != 0 {
		t.Fatal(errOut)
	}
	_, out, _ := run(t, "plugins", dir)
	if !strings.Contains(out, "Types of demo:\n  demo.echo  action      Returns its text\n  demo       connection  A demo service\n") {
		t.Errorf("types:\n%s", out)
	}
	t.Setenv(plugintest.ModeEnv, "outside")
	if _, out, _ := run(t, "plugins", dir); !strings.Contains(out, `  error: plugin demo: type core.echo is outside its namespace "demo"`) {
		t.Errorf("refused types:\n%s", out)
	}
}

// runnel validate names a plugin that is not trusted at its action; once trusted, runnel
// run runs the action in the plugin (S49b2).
func TestPluginValidateAndRun(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(plugintest.ModeEnv, "normal")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\n")
	writeFile(t, filepath.Join(dir, "plugins", "demo", "plugin.yaml"),
		"kind: Plugin\napiVersion: runnel/v1\nname: demo\nversion: 1.0.0\nnamespace: demo\nexecutable: '"+exe+"'\n")
	wf := filepath.Join(dir, "workflows", "echo.workflow.yaml")
	writeFile(t, wf, "kind: Workflow\napiVersion: runnel/v1\nname: echo\nactions:\n  - id: start\n    type: core.start\n  - id: say\n    type: demo.echo\n    with:\n      text: hello\nlinks:\n  - { from: start, to: say }\n")

	code, out, _ := run(t, "validate", dir)
	if code != 1 || !strings.Contains(out, `echo.workflow.yaml:8:11: error: action "say": plugin "demo" is not trusted on this computer; check it and run "runnel plugins trust"`) {
		t.Errorf("validate untrusted: %d\n%s", code, out)
	}
	if code, _, errOut := run(t, "run", wf, "--history", "none"); code != 2 || !strings.Contains(errOut, `plugin "demo" is not trusted`) {
		t.Errorf("run untrusted: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "plugins", "trust", dir, "--yes"); code != 0 {
		t.Fatal(errOut)
	}
	if code, out, _ := run(t, "validate", dir); code != 0 {
		t.Errorf("validate trusted: %d\n%s", code, out)
	}
	code, out, errOut := run(t, "run", "-v", wf, "--history", "none")
	if code != 0 || !strings.Contains(out+errOut, "demo: echo hello with s3cret") || !strings.Contains(out+errOut, `workflow "echo" succeeded`) {
		t.Errorf("run trusted: %d\n%s%s", code, out, errOut)
	}
}
