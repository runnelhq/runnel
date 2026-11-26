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
		{"type: cron", "type: http", `missing required field "method"`},
		{"type: cron", "type: hourly", `unknown trigger type "hourly" (expected cron, interval, file or http)`},
		{"type: cron", "type: interval", "schedule is not used by interval triggers (fields: every, overlap, history)"},
		{"type: cron", "type: interval", `missing required field "every" (e.g. 15m)`},
		{"type: cron", "type: file", `missing required field "folder"`},
		{"type: cron", "type: file", `missing required field "param"`},
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
		file := filepath.Join(dir, TriggersDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.yaml", triggerSource)
	write("b.yaml", triggerSource) // same name
	write("c.yaml", strings.Replace(triggerSource, "name: nightly", "name: weekly", 1))
	write("d.yaml", "kind: [\n")
	// Subfolders are read (E12); a name used in a subfolder clashes, hidden folders are left out.
	write("nightly/e.yaml", strings.Replace(triggerSource, "name: nightly", "name: hourly", 1))
	write("nightly/f.yaml", strings.Replace(triggerSource, "name: nightly", "name: weekly", 1))
	write(".old/g.yaml", strings.Replace(triggerSource, "name: nightly", "name: old", 1))
	list, err := LoadTriggers(dir)
	var names []string
	for _, tr := range list {
		names = append(names, tr.Name+"="+TriggerFileName(dir, tr.File()))
	}
	if got := strings.Join(names, " "); got != "nightly=a.yaml weekly=c.yaml hourly=nightly/e.yaml" {
		t.Errorf("triggers %s", got)
	}
	for _, want := range []string{`trigger name "nightly" is already used by a.yaml`, "d.yaml", `trigger name "weekly" is already used by c.yaml`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("problems %v, want %s", err, want)
		}
	}
	for rel, want := range map[string]bool{"triggers/a.yaml": true, "triggers/sub/a.yaml": true, "workflows/a.yaml": false,
		"triggers/.old/a.yaml": false, "triggers/a.txt": false, "triggers.yaml": false} {
		if IsTriggerFile(rel) != want {
			t.Errorf("IsTriggerFile(%s) = %v", rel, !want)
		}
	}
	if project, ok := TriggerProjectDir(filepath.Join(dir, TriggersDir, "nightly", "e.yaml")); ok {
		t.Errorf("without project.yaml: %s", project)
	}
	if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte("kind: Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if project, ok := TriggerProjectDir(filepath.Join(dir, TriggersDir, "nightly", "e.yaml")); !ok || project != dir {
		t.Errorf("TriggerProjectDir = %s, %v", project, ok)
	}
	if empty, err := TriggerFiles(t.TempDir()); err != nil || len(empty) != 0 {
		t.Errorf("no triggers folder: %v, %v", empty, err)
	}
}

const intervalSource = `kind: Trigger
apiVersion: runnel/v1
name: poll
type: interval
target: workflows/import.workflow.yaml
with:
  every: 15m
`

const fileSource = `kind: Trigger
apiVersion: runnel/v1
name: inbox
type: file
target: workflows/import.workflow.yaml
with:
  folder: data/in
  pattern: "*.CSV"
  param: FILE
`

func TestParseIntervalAndFileTriggers(t *testing.T) {
	tr, err := ParseTrigger("triggers/poll.yaml", []byte(intervalSource))
	if err != nil {
		t.Fatal(err)
	}
	if tr.EveryValue() != 15*time.Minute || tr.Describe() != "every 15m" || tr.OverlapMode() != OverlapSkip {
		t.Errorf("interval %+v", tr)
	}
	tr, err = ParseTrigger("triggers/inbox.yaml", []byte(fileSource))
	if err != nil {
		t.Fatal(err)
	}
	if tr.PollInterval() != DefaultFileInterval || tr.FileMode() != FileEach || tr.FilePattern() != "*.CSV" ||
		tr.Describe() != "new *.CSV in data/in" || tr.Folder("proj") != filepath.Join("proj", "data", "in") {
		t.Errorf("file %+v", tr)
	}
	abs := filepath.Join(t.TempDir(), "in")
	tr, err = ParseTrigger("x.yaml", []byte(strings.Replace(fileSource, "data/in", strings.ReplaceAll(abs, `\`, "/"), 1)))
	if err != nil || tr.Folder("proj") != abs {
		t.Errorf("absolute folder: %v %v", tr, err)
	}
	for _, c := range []struct {
		src, old, new, want string
	}{
		{intervalSource, "every: 15m", "every: 500ms", "every must be at least 1s"},
		{intervalSource, "every: 15m", "every: soon", "every:"},
		{intervalSource, "every: 15m", "every: 15m\n  schedule: \"* * * * *\"", "schedule is not used by interval triggers"},
		{fileSource, "param: FILE", "param: FILE\n  mode: all", `invalid mode "all" (expected each or batch)`},
		{fileSource, "param: FILE", "param: FILE\n  existing: skip", `invalid existing "skip" (expected process or ignore)`},
		{fileSource, `"*.CSV"`, `"[x"`, `invalid pattern "[x"`},
		{fileSource, `"*.CSV"`, `"in/*.csv"`, "matches file names only"},
		{fileSource, "param: FILE", "param: FILE\n  interval: 0s", "interval must be at least 1s"},
		{fileSource, "param: FILE", "param: FILE\n  every: 1m", "every is not used by file triggers"},
		{fileSource, "param: FILE", "param: FILE\n  overlap: queue", "overlap: queue is not supported yet"},
	} {
		src := strings.Replace(c.src, c.old, c.new, 1)
		_, err := ParseTrigger("triggers/t.yaml", []byte(src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q → %q: got %v, want %q", c.old, c.new, err, c.want)
		}
	}
}

// The folder of a file trigger is folder (R5a); the old name path is not used by file
// triggers. maxConcurrentRuns of an http trigger is 4 unless set; an explicit 0 is no limit.
func TestFileTriggerFolderAndHTTPLimit(t *testing.T) {
	_, err := ParseTrigger("triggers/inbox.yaml", []byte(strings.Replace(fileSource, "folder: data/in", "path: data/in", 1)))
	if err == nil || !strings.Contains(err.Error(), "triggers/inbox.yaml:7:9: path is not used by file triggers (fields: folder,") ||
		!strings.Contains(err.Error(), `missing required field "folder"`) {
		t.Errorf("old name: %v", err)
	}
	base := "kind: Trigger\napiVersion: runnel/v1\nname: api\ntype: http\ntarget: workflows/import.workflow.yaml\nwith:\n  method: GET\n  path: /x\n  auth: none\n"
	for extra, want := range map[string]int{"": DefaultHTTPMaxConcurrent, "  maxConcurrentRuns: 0\n": 0, "  maxConcurrentRuns: 2\n": 2} {
		tr, err := ParseTrigger("triggers/api.yaml", []byte(base+extra))
		if err != nil {
			t.Fatal(err)
		}
		if got := tr.HTTPMaxConcurrent(); got != want {
			t.Errorf("%q: %d, want %d", extra, got, want)
		}
	}
	if _, err := ParseTrigger("triggers/api.yaml", []byte(base+"  maxConcurrentRuns: -1\n")); err == nil || !strings.Contains(err.Error(), "maxConcurrentRuns must not be negative") {
		t.Errorf("negative: %v", err)
	}
	// An explicit 0 is a value: other trigger types report it as unused.
	cron := "kind: Trigger\napiVersion: runnel/v1\nname: n\ntype: cron\ntarget: workflows/import.workflow.yaml\nwith:\n  schedule: \"@daily\"\n  maxConcurrentRuns: 0\n"
	if _, err := ParseTrigger("triggers/n.yaml", []byte(cron)); err == nil || !strings.Contains(err.Error(), "maxConcurrentRuns is not used by cron triggers") {
		t.Errorf("cron: %v", err)
	}
}

// maxConcurrent of an http trigger is maxConcurrentRuns since R5b; the old name is an
// unknown field.
func TestHTTPTriggerOldMaxConcurrent(t *testing.T) {
	src := "kind: Trigger\napiVersion: runnel/v1\nname: api\ntype: http\ntarget: workflows/import.workflow.yaml\nwith:\n  method: GET\n  path: /x\n  auth: none\n  maxConcurrent: 2\n"
	_, err := ParseTrigger("triggers/api.yaml", []byte(src))
	if err == nil || !strings.Contains(err.Error(), `triggers/api.yaml:10:3: unknown field "maxConcurrent"`) {
		t.Errorf("old name: %v", err)
	}
}
