package model

import (
	"slices"
	"strings"
	"testing"
	"time"
)

const connText = `# The ERP database.
kind: Connection
apiVersion: runnel/v1
name: erp
type: sqlserver
with:
  host: ${ERP_HOST}   # from the environment
  database: ERP
  password: ${secret:erp-pw}
`

func TestEditFileConnection(t *testing.T) {
	cases := []struct {
		f    FileField
		want string
	}{
		{FileField{Field: "with.host", Value: "db01"}, "  host: db01   # from the environment\n"},
		{FileField{Field: "with.port", Value: "1433"}, "  password: ${secret:erp-pw}\n  port: \"1433\"\n"},
		{FileField{Field: "with.encrypt", Value: false}, "  encrypt: false\n"},
		{FileField{Field: "with.password", Value: nil}, "  database: ERP\n"},
		{FileField{Field: "description", Value: "ERP, read only"}, "name: erp\ndescription: ERP, read only\ntype: sqlserver\n"},
		{FileField{Field: "type", Value: "postgres"}, "type: postgres\n"},
		{FileField{Field: "with.headers", Map: []KeyValue{{Key: "Accept", Value: "application/json"}}}, "  headers:\n    Accept: application/json\n"},
	}
	for _, c := range cases {
		out, _, err := EditFile(KindConnection, "connections/erp.yaml", []byte(connText), c.f)
		if err != nil {
			t.Fatalf("%+v: %v", c.f, err)
		}
		if !strings.Contains(string(out), c.want) || !strings.HasPrefix(string(out), "# The ERP database.\n") {
			t.Errorf("%+v:\n%s\nwant containing %q", c.f, out, c.want)
		}
		if c.f.Field == "with.password" && strings.Contains(string(out), "password") {
			t.Errorf("not removed:\n%s", out)
		}
	}
}

func TestEditFileNewWithAndCRLF(t *testing.T) {
	text := strings.ReplaceAll("kind: Connection\napiVersion: runnel/v1\nname: api\ntype: http\n", "\n", "\r\n")
	out, _, err := EditFile(KindConnection, "connections/api.yaml", []byte(text), FileField{Field: "with.baseUrl", Value: "https://h/"})
	if err != nil || string(out) != text+"with:\r\n  baseUrl: https://h/\r\n" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestEditFileRefusals(t *testing.T) {
	for _, c := range []struct {
		f    FileField
		want string
	}{
		{FileField{Field: "name", Value: "x"}, "cannot be set"},
		{FileField{Field: "target", Value: "x"}, "cannot be set"},
		{FileField{Field: "with.a b", Value: "x"}, "cannot be set"},
		{FileField{Field: "type", Value: nil}, "cannot be removed"},
		{FileField{Field: "kind.x", Value: "x"}, "cannot be set"},
	} {
		if _, _, err := EditFile(KindConnection, "connections/erp.yaml", []byte(connText), c.f); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c.f, err)
		}
	}
	if _, _, err := EditFile(KindConnection, "connections/erp.yaml", []byte("with: {host: a}\nname: x\n"), FileField{Field: "with.host", Value: "b"}); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Errorf("flow style: %v", err)
	}
	if _, _, err := EditFile(KindConnection, "connections/erp.yaml", []byte("kind: [\n"), FileField{Field: "description", Value: "b"}); err == nil {
		t.Error("broken YAML accepted")
	}
}

const triggerText = `# Imports every night.
kind: Trigger
apiVersion: runnel/v1
name: nightly
type: cron
target: workflows/import.workflow.yaml
with:
  schedule: "0 2 * * 1-5"   # weekdays
  timezone: Europe/Berlin
  overlap: skip
`

// The form of a trigger (E20a): fields of the trigger in their place, a new type removes
// the fields under with: it does not use.
func TestEditFileTrigger(t *testing.T) {
	cases := []struct {
		f       FileField
		want    string
		removed []string
	}{
		{FileField{Field: "environment", Value: "prod"}, "target: workflows/import.workflow.yaml\nenvironment: prod\nwith:\n", nil},
		{FileField{Field: "enabled", Value: false}, "target: workflows/import.workflow.yaml\nenabled: false\nwith:\n", nil},
		{FileField{Field: "params.DAY", Value: "{{ today() }}"}, "target: workflows/import.workflow.yaml\nparams:\n  DAY: \"{{ today() }}\"\nwith:\n", nil},
		{FileField{Field: "description", Value: "Nightly import"}, "name: nightly\ndescription: Nightly import\ntype: cron\n", nil},
		{FileField{Field: "with.schedule", Value: "0 3 * * *"}, "  schedule: 0 3 * * *   # weekdays\n", nil},
		{FileField{Field: "type", Value: "interval"}, "type: interval\ntarget: workflows/import.workflow.yaml\nwith:\n  overlap: skip\n", []string{"schedule", "timezone"}},
		{FileField{Field: "type", Value: "http"}, "type: http\ntarget: workflows/import.workflow.yaml\n", []string{"schedule", "timezone", "overlap"}},
		{FileField{Field: "with.auth", Map: []KeyValue{{Key: "token", Value: "${secret:hook}"}}}, "  auth:\n    token: ${secret:hook}\n", nil},
	}
	for _, c := range cases {
		out, removed, err := EditFile(KindTrigger, "triggers/nightly.yaml", []byte(triggerText), c.f)
		if err != nil {
			t.Fatalf("%+v: %v", c.f, err)
		}
		if !strings.Contains(string(out), c.want) || !strings.HasPrefix(string(out), "# Imports every night.\n") || !slices.Equal(removed, c.removed) {
			t.Errorf("%+v:\n%s\nwant containing %q, removed %v (got %v)", c.f, out, c.want, c.removed, removed)
		}
		if c.f.Value == "http" && strings.Contains(string(out), "with:") {
			t.Errorf("empty with: left:\n%s", out)
		}
	}
	for _, f := range []FileField{{Field: "target", Value: nil}, {Field: "params", Value: "x"}, {Field: "name", Value: "x"}, {Field: "kind", Value: "x"}} {
		if _, _, err := EditFile(KindTrigger, "triggers/nightly.yaml", []byte(triggerText), f); err == nil {
			t.Errorf("%+v accepted", f)
		}
	}
}

func TestDescribeSchedule(t *testing.T) {
	for spec, want := range map[string]string{
		"0 2 * * 1-5":       "At 02:00, Monday to Friday",
		"30 6,18 * * *":     "At 06:30 and 18:30, every day",
		"15 * * * *":        "Every hour, at minute 15, every day",
		"*/15 * * * *":      "Every 15 minutes, every day",
		"*/10 8-17 * * MON": "Every 10 minutes from 08:00 to 17:59, on Monday",
		"0 0 1,15 * *":      "At 00:00, on day 1 and 15 of the month",
		"0 9 1 1 *":         "At 09:00, on day 1 of the month in January",
		"0 9 * 12 0,6":      "At 09:00, on Sunday and Saturday in December",
		"@daily":            "At 00:00, every day",
		"0 9 1 * 1":         "",
		"0-30 * * * *":      "",
		"0 2 * *":           "",
	} {
		if got := DescribeSchedule(spec); got != want {
			t.Errorf("%q: %q, want %q", spec, got, want)
		}
	}
	at := time.Date(2026, 11, 23, 20, 0, 0, 0, time.UTC) // a Monday
	times, err := ScheduleTimes("0 2 * * 1-5", "Europe/Berlin", at, 3)
	if err != nil || len(times) != 3 || times[0].Format(time.RFC3339) != "2026-11-24T02:00:00+01:00" || times[2].Weekday() != time.Thursday {
		t.Fatalf("%v %v", times, err)
	}
	if _, err := ScheduleTimes("0 2 * * 1-5", "Mars/Base", at, 3); err == nil {
		t.Error("unknown time zone accepted")
	}
	if _, err := ScheduleTimes("61 * * * *", "", at, 3); err == nil {
		t.Error("invalid schedule accepted")
	}
}

// The forms of environments and project.yaml (E20b): variables and secrets by name, the
// fields of the project; the name of an environment stays.
func TestEditFileEnvironmentAndProject(t *testing.T) {
	env := "kind: Environment\napiVersion: runnel/v1\nname: prod\nvariables:\n  ERP_HOST: db01   # the server\nsecretStore: inline\nsecrets:\n  token: abc\n"
	for _, c := range []struct {
		f    FileField
		want string
	}{
		{FileField{Field: "variables.ERP_HOST", Value: "db02"}, "  ERP_HOST: db02   # the server\n"},
		{FileField{Field: "variables.INBOX", Value: `\\fs01\in`}, "  ERP_HOST: db01   # the server\n  INBOX: \\\\fs01\\in\n"},
		{FileField{Field: "variables.ERP_HOST", Value: nil}, "name: prod\nsecretStore: inline\n"},
		{FileField{Field: "description", Value: "Production"}, "name: prod\ndescription: Production\nvariables:\n"},
		{FileField{Field: "secretStore", Value: "env"}, "secretStore: env\n"},
		{FileField{Field: "secrets.api", Value: "s3cret"}, "  token: abc\n  api: s3cret\n"},
	} {
		out, _, err := EditFile(KindEnvironment, "environments/prod.yaml", []byte(env), c.f)
		if err != nil || !strings.Contains(string(out), c.want) {
			t.Errorf("%+v: %v\n%s\nwant containing %q", c.f, err, out, c.want)
		}
	}
	if _, _, err := EditFile(KindEnvironment, "environments/prod.yaml", []byte(env), FileField{Field: "name", Value: "qa"}); err == nil {
		t.Error("name of an environment set")
	}
	proj := "kind: Project\napiVersion: runnel/v1\nname: orders\nvariables:\n  BASE: C:\\data\n"
	for _, c := range []struct {
		f    FileField
		want string
	}{
		{FileField{Field: "name", Value: "shop"}, "name: shop\n"},
		{FileField{Field: "defaultEnvironment", Value: "dev"}, "name: orders\ndefaultEnvironment: dev\nvariables:\n"},
		{FileField{Field: "settings.maxConcurrentRuns", Value: 4}, "  BASE: C:\\data\nsettings:\n  maxConcurrentRuns: 4\n"},
		{FileField{Field: "workflow.defaultJoin", Value: "first"}, "workflow:\n  defaultJoin: first\n"},
	} {
		out, _, err := EditFile(KindProject, "project.yaml", []byte(proj), c.f)
		if err != nil || !strings.Contains(string(out), c.want) {
			t.Errorf("%+v: %v\n%s\nwant containing %q", c.f, err, out, c.want)
		}
	}
	if _, _, err := EditFile(KindProject, "project.yaml", []byte(proj), FileField{Field: "name", Value: nil}); err == nil {
		t.Error("name of the project removed")
	}
}
