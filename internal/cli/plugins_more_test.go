package cli

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Approvals: "yes" in full also trusts, no answer (end of input) does not; untrust of a
// plugin that is not trusted says so; a plugin without a program for this system is
// listed with a note.
func TestPluginsTrustAnswers(t *testing.T) {
	dir := pluginProject(t)
	var buf bytes.Buffer
	if err := trustPlugins(&buf, strings.NewReader(""), dir, pluginsOptions{}, time.Now()); err != nil || !strings.Contains(buf.String(), "Plugin iban is not trusted.") {
		t.Errorf("no answer: %v\n%s", err, buf.String())
	}
	buf.Reset()
	if err := trustPlugins(&buf, strings.NewReader(" YES \r\n"), dir, pluginsOptions{}, time.Now()); err != nil || !strings.Contains(buf.String(), "Plugin iban is trusted.") {
		t.Errorf("YES: %v\n%s", err, buf.String())
	}
	if code, out, _ := run(t, "plugins", "untrust", dir, "--plugin", "iban"); code != 0 || !strings.Contains(out, "Plugin iban is no longer trusted.") {
		t.Errorf("untrust: %d %s", code, out)
	}
	if code, out, _ := run(t, "plugins", "untrust", dir, "--plugin", "iban"); code != 0 || !strings.Contains(out, "Plugin iban was not trusted.") {
		t.Errorf("untrust again: %d %s", code, out)
	}

	other := map[string]string{"windows": "linux", "linux": "windows", "darwin": "windows"}[runtime.GOOS]
	if other == "" {
		other = "windows"
	}
	writeFile(t, filepath.Join(dir, "plugins", "elsewhere", "plugin.yaml"),
		"kind: Plugin\napiVersion: runnel/v1\nname: elsewhere\nversion: 1.0.0\nnamespace: elsewhere\nexecutable:\n  "+other+": bin/x\n")
	if code, out, _ := run(t, "plugins", dir); code != 0 || !strings.Contains(out, "– (none for "+runtime.GOOS+")") {
		t.Errorf("no program for %s: %d\n%s", runtime.GOOS, code, out)
	}
}

// trust and untrust in a project without plugins or without project, and --service on
// other systems than Windows.
func TestPluginsTrustRefusals(t *testing.T) {
	empty := t.TempDir()
	writeFile(t, filepath.Join(empty, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: empty\n")
	if code, out, _ := run(t, "plugins", "trust", empty, "--yes"); code != 0 || !strings.Contains(out, "No plugins in "+filepath.Join(displayPath(empty), "plugins")) {
		t.Errorf("trust without plugins: %d %s", code, out)
	}
	for _, args := range [][]string{{"plugins", "trust", t.TempDir(), "--yes"}, {"plugins", "untrust", t.TempDir()}} {
		if code, _, errOut := run(t, args...); code != 1 || !strings.Contains(errOut, "no project (project.yaml) found") {
			t.Errorf("%v: %d %s", args, code, errOut)
		}
	}
	if runtime.GOOS != "windows" {
		dir := pluginProject(t)
		if code, _, errOut := run(t, "plugins", "trust", dir, "--yes", "--service"); code != 1 || !strings.Contains(errOut, "--service: approvals for the service exist only on Windows") {
			t.Errorf("service: %d %s", code, errOut)
		}
	}
}
