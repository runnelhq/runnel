package cli

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/winsvc"
)

func TestServiceConfig(t *testing.T) {
	o := installOptions{name: "runnel-import", account: "localservice", start: "delayed", port: 7301, stopTimeout: 45 * time.Second}
	c, err := serviceConfig(o, `D:\flows`, `C:\Tools\runnel.exe`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"service", "run", "--name", "runnel-import", "--port", "7301", "--stop-timeout", "45s", `D:\flows`}
	if !slices.Equal(c.Args, want) || c.Exe != `C:\Tools\runnel.exe` || c.Account != winsvc.AccountLocalService ||
		c.DisplayName != "Runnel (runnel-import)" || c.Description != `Runnel server for the project D:\flows (http://127.0.0.1:7301)` {
		t.Errorf("config %+v", c)
	}
	for _, c := range []struct {
		change func(*installOptions)
		want   string
	}{
		{func(o *installOptions) { o.name = "my service" }, `invalid service name "my service"`},
		{func(o *installOptions) { o.start = "boot" }, `invalid start type "boot" (expected auto, delayed or manual)`},
		{func(o *installOptions) { o.port = 0 }, "invalid --port 0 (expected 1–65535)"},
		{func(o *installOptions) { o.stopTimeout = -time.Second }, "invalid --stop-timeout -1s"},
	} {
		bad := o
		c.change(&bad)
		if _, err := serviceConfig(bad, `D:\flows`, "runnel.exe"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("got %v, want %q", err, c.want)
		}
	}
}

func TestServiceNotOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the service commands change the system on Windows; tested by hand (TESTPLAN W48)")
	}
	code, _, errOut := run(t, "service", "status")
	if code != 1 || !strings.Contains(errOut, "runnel service is only available on Windows") {
		t.Errorf("exit code %d, stderr %q", code, errOut)
	}
}
