package shell

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/paths"
)

// needPowerShell skips where no PowerShell is installed; in the CI (Linux with pwsh,
// Windows with both editions) it must be there, so that these tests always run there.
func needPowerShell(t *testing.T) {
	t.Helper()
	if _, err := findPowerShell(editionAuto); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("PowerShell is missing in the CI: %v", err)
		}
		t.Skipf("no PowerShell: %v", err)
	}
}

func powershell(t *testing.T, dir string, with map[string]any) (map[string]any, []string, error) {
	t.Helper()
	a, err := newPowershell(with)
	if err != nil {
		return nil, nil, err
	}
	var logs []string
	ec := &action.ExecContext{Log: func(_ slog.Level, m string) { logs = append(logs, m) }}
	res, err := a.Execute(paths.WithBase(context.Background(), dir), ec)
	out, _ := res.Output.(map[string]any)
	return out, logs, err
}

// Parameters keep their type; json reads the last JSON value after other output; the
// secret from env does not appear in the output.
func TestPowerShell(t *testing.T) {
	needPowerShell(t)
	dir := t.TempDir()
	script := `param([string]$Name, [int]$Count, [string[]]$Tags, [bool]$Flag)
Write-Output "Grüße an $Name"
Write-Output "token $env:TOKEN"
[pscustomobject]@{ name = $Name; twice = $Count * 2; tags = $Tags.Count; flag = $Flag } | ConvertTo-Json`
	out, _, err := powershell(t, dir, map[string]any{"script": script, "json": true,
		"params": map[string]any{"Name": "Ada; Remove-Item *", "Count": 21, "Tags": []any{"a", "b"}, "Flag": true},
		"env":    map[string]any{"TOKEN": "s3cret-value"}})
	if err != nil {
		t.Fatalf("%v\n%#v", err, out)
	}
	j := out["json"].(map[string]any)
	if j["name"] != "Ada; Remove-Item *" || j["twice"] != float64(42) || j["tags"] != float64(2) || j["flag"] != true {
		t.Errorf("json %#v", j)
	}
	stdout := out["stdout"].(string)
	// Windows PowerShell 5.1 as well, where it exists (the CI on Windows).
	if runtime.GOOS == "windows" {
		out, _, err := powershell(t, dir, map[string]any{"script": "param([int]$N) $N + 1", "edition": "windows", "params": map[string]any{"N": 41}})
		if err != nil || strings.TrimSpace(out["stdout"].(string)) != "42" {
			t.Errorf("Windows PowerShell: %#v %v", out, err)
		}
	}
	if !strings.Contains(stdout, "Grüße an Ada; Remove-Item *") || strings.Contains(stdout, "s3cret-value") || !strings.Contains(stdout, "token ***") {
		t.Errorf("stdout %q", stdout)
	}
}

// An exception and exit codes other than 0 fail the action with the message.
func TestPowerShellErrors(t *testing.T) {
	needPowerShell(t)
	dir := t.TempDir()
	_, _, err := powershell(t, dir, map[string]any{"script": `throw "no files in the inbox"`})
	if err == nil || !strings.Contains(err.Error(), "the script ended with exit code 1: no files in the inbox") {
		t.Errorf("throw: %v", err)
	}
	_, _, err = powershell(t, dir, map[string]any{"script": `Get-Item -LiteralPath ./missing-file`})
	if err == nil || !strings.Contains(err.Error(), "exit code 1") {
		t.Errorf("error with ErrorActionPreference Stop: %v", err)
	}
	_, _, err = powershell(t, dir, map[string]any{"script": "exit 3"})
	if err == nil || !strings.Contains(err.Error(), "ended with exit code 3") {
		t.Errorf("exit 3: %v", err)
	}
	// A script file relative to the project folder.
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "hello.ps1"), []byte("param($Who)\n\"hello $Who\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, logs, err := powershell(t, dir, map[string]any{"file": "scripts/hello.ps1", "params": map[string]any{"Who": "Runnel"}})
	if err != nil || strings.TrimSpace(out["stdout"].(string)) != "hello Runnel" || !strings.Contains(logs[len(logs)-1], "hello.ps1 ended with exit code 0") {
		t.Errorf("file: %#v %v %v", out, logs, err)
	}
	if _, _, err := powershell(t, dir, map[string]any{"file": "scripts/missing.ps1"}); err == nil || !strings.Contains(err.Error(), "script file scripts/missing.ps1 not found") {
		t.Errorf("missing file: %v", err)
	}
	// The timeout ends the script.
	if _, _, err := powershell(t, dir, map[string]any{"script": "Start-Sleep -Seconds 60", "timeout": "2s"}); err == nil || !strings.Contains(err.Error(), "took longer than 2s") {
		t.Errorf("timeout: %v", err)
	}
}

func TestPowerShellConfig(t *testing.T) {
	for want, with := range map[string]map[string]any{
		"give the script in script or its file in file": {},
		"give either script or file, not both":          {"script": "x", "file": "x.ps1"},
		`invalid edition "core"`:                        {"script": "x", "edition": "core"},
		`invalid timeout "soon"`:                        {"script": "x", "timeout": "soon"},
		`env: invalid name ""`:                          {"script": "x", "env": map[string]any{"": "1"}},
	} {
		if _, err := newPowershell(with); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", with, err, want)
		}
	}
	if runtime.GOOS != "windows" {
		if _, err := findPowerShell(editionWindows); err == nil || !strings.Contains(err.Error(), "exists only on Windows") {
			t.Errorf("windows edition elsewhere: %v", err)
		}
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		if _, err := findPowerShell(editionPwsh); err == nil || err.Error() != "PowerShell 7 (pwsh) is not installed" {
			t.Errorf("pwsh missing: %v", err)
		}
	}
}

func TestLastJSON(t *testing.T) {
	for text, want := range map[string]string{
		`{"a": 1}`:                             "map[a:1]",
		"starting\nfound 2\n[\n  1,\n  2\n]\n": "[1 2]",
		"note {x}\n{\"ok\": true}":             "map[ok:true]",
	} {
		v, err := lastJSON(text)
		if err != nil || fmt.Sprint(v) != want {
			t.Errorf("%q: %v %v", text, v, err)
		}
	}
	if _, err := lastJSON("no json here"); err == nil {
		t.Error("text accepted as JSON")
	}
}
