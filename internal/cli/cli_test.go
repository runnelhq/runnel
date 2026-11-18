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
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("stderr = %q, want error message", errOut)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := run(t, "does-not-exist")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, `unknown command "does-not-exist"`) {
		t.Errorf("stderr = %q, want unknown command error", errOut)
	}
}
