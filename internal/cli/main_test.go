package cli

import (
	"fmt"
	"os"
	"testing"
)

// TestMain keeps the run histories of "runnel serve" in the tests (ADR 0026) in a
// temporary folder instead of the user's data folder.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "runnel-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range []string{"XDG_STATE_HOME", "LOCALAPPDATA", "XDG_CONFIG_HOME", "APPDATA"} {
		_ = os.Setenv(name, dir)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
