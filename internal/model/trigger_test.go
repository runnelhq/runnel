package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const triggerSource = `kind: Trigger
apiVersion: runnel/v1
name: nightly
type: cron
target: workflows/import.workflow.yaml
environment: prod
params:
  DAYS: 3
with:
  schedule: "0 2 * * 1-5"
  timezone: Europe/Berlin
`

func TestParseTrigger(t *testing.T) {
	tr, err := ParseTrigger("triggers/nightly.yaml", []byte(triggerSource))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsEnabled() || tr.OverlapMode() != OverlapSkip || tr.Params["DAYS"] != uint64(3) {
		t.Errorf("trigger %+v", tr)
	}
	// Friday 23:00 in Berlin: the next time is Monday 02:00 in Berlin.
	berlin, _ := time.LoadLocation("Europe/Berlin")
	next := tr.Next(time.Date(2026, 11, 20, 23, 0, 0, 0, berlin).UTC())
	if want := time.Date(2026, 11, 23, 2, 0, 0, 0, berlin); !next.Equal(want) {
		t.Errorf("next %v, want %v", next, want)
	}
	// Daylight saving: 02:30 does not exist on 2027-03-28 in Berlin; the day is skipped.
	tr2, err := ParseTrigger("x.yaml", []byte(strings.Replace(triggerSource, `"0 2 * * 1-5"`, `"30 2 * * *"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	next = tr2.Next(time.Date(2027, 3, 27, 12, 0, 0, 0, berlin))
	if next.Day() != 29 || next.Hour() != 2 || next.Minute() != 30 {
		t.Errorf("over daylight saving: %v", next)
	}
	// Disabled triggers have no next time in the scheduler, but a schedule.
	tr3, err := ParseTrigger("x.yaml", []byte(triggerSource+"enabled: false\n"))
	if err != nil || tr3.IsEnabled() {
		t.Errorf("disabled: %v %v", tr3, err)
	}
}

func TestParseTriggerErrors(t *testing.T) {
	for _, c := range []struct {
		old, new, want string
	}{
		{"type: cron", "type: http", `trigger type "http" is not supported yet (roadmap S38a, ADR 0018)`},
		{"type: cron", "type: file", `trigger type "file" is not supported yet (roadmap S38)`},
		{"type: cron", "type: hourly", `unknown trigger type "hourly" (expected cron)`},
		{"type: cron\n", "", `missing required field "type"`},
		{"target: workflows/import.workflow.yaml", "target: data/in.csv", "must be a workflow (*.workflow.yaml) or pipeline (*.pipeline.yaml) file"},
		{"target: workflows/import.workflow.yaml", "target: ../other/x.workflow.yaml", "must be a path inside the project"},
		{`"0 2 * * 1-5"`, `"0 25 * * *"`, `invalid schedule "0 25 * * *": end of range (25) above maximum (23)`},
		{`"0 2 * * 1-5"`, `"@every 5m"`, "@every is not a schedule; repeating at a fixed interval follows with type interval"},
		{`"0 2 * * 1-5"`, `"CRON_TZ=UTC 0 2 * * *"`, "set the time zone with timezone:"},
		{"  timezone: Europe/Berlin", "  timezone: Mars/Olympus", `unknown time zone "Mars/Olympus"`},
		{"  timezone: Europe/Berlin", "  timezone: Europe/Berlin\n  overlap: queue", "overlap: queue is not supported yet"},
		{"  timezone: Europe/Berlin", "  timezone: Europe/Berlin\n  overlap: never", `invalid overlap "never" (expected skip or allow)`},
		{"  DAYS: 3", "  DAYS: \"{{ 1 + 2 }}\"", "parameter DAYS: expressions {{ … }} are not supported in triggers"},
		{"name: nightly", "name: 9-nightly", `invalid trigger name "9-nightly"`},
		{"  timezone: Europe/Berlin", "  timezone: Europe/Berlin\n  period: 5m", `unknown field "period"`},
	} {
		src := strings.Replace(triggerSource, c.old, c.new, 1)
		_, err := ParseTrigger("triggers/nightly.yaml", []byte(src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q → %q: got %v, want %q", c.old, c.new, err, c.want)
		}
	}
}

func TestLoadTriggers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.MkdirAll(filepath.Join(dir, TriggersDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, TriggersDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.yaml", triggerSource)
	write("b.yaml", triggerSource) // same name
	write("c.yaml", strings.Replace(triggerSource, "name: nightly", "name: weekly", 1))
	write("d.yaml", "kind: [\n")
	list, err := LoadTriggers(dir)
	if len(list) != 2 || list[0].Name != "nightly" || list[1].Name != "weekly" {
		t.Errorf("triggers %v", list)
	}
	if err == nil || !strings.Contains(err.Error(), `trigger name "nightly" is already used by a.yaml`) || !strings.Contains(err.Error(), "d.yaml") {
		t.Errorf("problems %v", err)
	}
	if !IsTriggerFile("triggers/a.yaml") || IsTriggerFile("triggers/sub/a.yaml") || IsTriggerFile("workflows/a.yaml") {
		t.Error("IsTriggerFile")
	}
}
