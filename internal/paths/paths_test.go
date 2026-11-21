package paths

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve(t *testing.T) {
	base := t.TempDir()
	ctx := WithBase(context.Background(), base)
	cases := map[string]string{
		"data/in.csv":     filepath.Join(base, "data", "in.csv"),
		"../shared/x.csv": filepath.Join(filepath.Dir(base), "shared", "x.csv"),
		"":                "",
		"/var/in.csv":     "/var/in.csv",
	}
	if runtime.GOOS == "windows" {
		cases[`C:\in\x.csv`] = `C:\in\x.csv`
		cases[`\\server\share\x.csv`] = `\\server\share\x.csv`
		cases[`D:x.csv`] = `D:x.csv`
		cases[`\in\x.csv`] = `\in\x.csv`
		cases[`data\in.csv`] = filepath.Join(base, "data", "in.csv")
	}
	for in, want := range cases {
		if got := Resolve(ctx, in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
	// Without a base, relative paths stay relative (working directory).
	if got := Resolve(context.Background(), "data/in.csv"); got != "data/in.csv" {
		t.Errorf("without base: %q", got)
	}
	if Base(WithBase(context.Background(), "")) != "" {
		t.Error("an empty base must not be set")
	}
}
