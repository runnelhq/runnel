package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/version"
)

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := version.Get().String() + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestVersionShort(t *testing.T) {
	code, out, _ := run(t, "version", "--short")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if want := version.Version + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestVersionJSON(t *testing.T) {
	code, out, _ := run(t, "version", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var got version.Info
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if got != version.Get() {
		t.Errorf("JSON = %+v, want %+v", got, version.Get())
	}
}

func TestVersionFlagsMutuallyExclusive(t *testing.T) {
	code, _, errOut := run(t, "version", "--short", "--json")
	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("stderr = %q, want error message", errOut)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := run(t, "does-not-exist")
	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, `unknown command "does-not-exist"`) {
		t.Errorf("stderr = %q, want unknown command error", errOut)
	}
}

// TestUsageErrors: an invalid command line starts nothing and ends with exit code 2
// (R5a), not 1, which means "failed" or "problems found"; "runnel health" ends with 3.
func TestUsageErrors(t *testing.T) {
	chdirRepoRoot(t)
	hello := "examples/hello/workflows/hello.workflow.yaml"
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"run", "--bogus", hello}, exitUsage, "unknown flag: --bogus"},
		{[]string{"run"}, exitUsage, "accepts 1 arg(s), received 0"},
		{[]string{"run", hello, hello}, exitUsage, "accepts 1 arg(s), received 2"},
		{[]string{"run", hello, "--param"}, exitUsage, "flag needs an argument: --param"},
		{[]string{"run", "-x", hello}, exitUsage, "unknown shorthand flag"},
		{[]string{"validate", "--bogus"}, exitUsage, "unknown flag: --bogus"},
		{[]string{"validate", "--strict=maybe", "examples/hello"}, exitUsage, "invalid argument"},
		{[]string{"triggers", "a", "b"}, exitUsage, "accepts at most 1 arg(s)"},
		{[]string{"triggers", "--bogus"}, exitUsage, "unknown flag: --bogus"},
		{[]string{"catalog", "--bogus"}, exitUsage, "unknown flag: --bogus"},
		{[]string{"schema"}, exitUsage, "accepts 1 arg(s), received 0"},
		{[]string{"runs", "--bogus"}, exitUsage, "unknown flag: --bogus"},
		{[]string{"serve", "a", "b"}, exitUsage, "accepts at most 1 arg(s)"},
		{[]string{"plugins", "--bogus"}, exitUsage, "unknown flag: --bogus"},
		{[]string{"version", "extra"}, exitUsage, "unknown command"},
		{[]string{"vesion"}, exitUsage, `unknown command "vesion"`},
		{[]string{"health", "extra"}, healthExitUnreachable, "unknown command"},
		{[]string{"health", "--bogus"}, healthExitUnreachable, "unknown flag: --bogus"},
	} {
		code, out, errOut := run(t, tt.args...)
		if code != tt.code || out != "" || !strings.Contains(errOut, tt.want) || !strings.Contains(errOut, "--help' for usage") {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q; want %d and %q", tt.args, code, out, errOut, tt.code, tt.want)
		}
	}
}
