package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// POST /forms/edit (S39c2a): reads a connection for the form and sets one field
// in its text, keeping the rest.
func TestEditConnectionForm(t *testing.T) {
	_, s := newProject(t)
	text := "kind: Connection\napiVersion: runnel/v1\nname: db\ntype: sqlite\nwith:\n  path: out/app.db  # the file\n"
	body := func(set string) string {
		b, _ := json.Marshal(map[string]any{"path": "connections/db.yaml", "content": text})
		if set == "" {
			return string(b)
		}
		return strings.TrimSuffix(string(b), "}") + `,"set":` + set + "}"
	}
	var res formEditResponse
	rec := do(t, s, "POST", "/api/v1/forms/edit", body(""))
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Connection == nil || res.Connection.Type != "sqlite" || res.Connection.With["path"] != "out/app.db" || res.Content != text {
		t.Fatalf("%s %v", rec.Body, err)
	}
	rec = do(t, s, "POST", "/api/v1/forms/edit", body(`{"field":"with.readOnly","value":true}`))
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || !strings.HasSuffix(res.Content, "  path: out/app.db  # the file\n  readOnly: true\n") || res.Connection.With["readOnly"] != true {
		t.Fatalf("%s %v", rec.Body, err)
	}
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", body(`{"field":"name","value":"x"}`)), http.StatusBadRequest, "cannot be set")
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", `{"path":"workflows/a.workflow.yaml","content":""}`), http.StatusBadRequest, "only connection, trigger and environment files")
	// A file that does not read has no connection for the form.
	if rec := do(t, s, "POST", "/api/v1/forms/edit", `{"path":"connections/db.yaml","content":"kind: ["}`); rec.Code != 200 || strings.Contains(rec.Body.String(), `"connection"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", `{"path":"../x.yaml","content":""}`), http.StatusBadRequest, "")
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", body(""), "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
}

// The form of a trigger (E20a): the fields as written, the schedule in words with the
// next times, the fields a new type removed.
func TestEditTriggerForm(t *testing.T) {
	_, s := newProject(t)
	text := "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/a.workflow.yaml\nparams:\n  DAY: today\nwith:\n  schedule: \"0 2 * * 1-5\"\n  timezone: Europe/Berlin\n"
	body := func(set string) string {
		b, _ := json.Marshal(map[string]any{"path": "triggers/nightly.yaml", "content": text})
		if set == "" {
			return string(b)
		}
		return strings.TrimSuffix(string(b), "}") + `,"set":` + set + "}"
	}
	var res formEditResponse
	rec := do(t, s, "POST", "/api/v1/forms/edit", body(""))
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Trigger == nil || res.Trigger.Type != "cron" || res.Trigger.Params["DAY"] != "today" ||
		res.Trigger.Schedule == nil || res.Trigger.Schedule.Text != "At 02:00, Monday to Friday" || len(res.Trigger.Schedule.Next) != 3 {
		t.Fatalf("%s %v", rec.Body, err)
	}
	next, err := time.Parse(time.RFC3339, res.Trigger.Schedule.Next[0])
	if _, offset := next.Zone(); err != nil || next.Hour() != 2 || (offset != 3600 && offset != 7200) {
		t.Errorf("next %v %v", next, err)
	}
	rec = do(t, s, "POST", "/api/v1/forms/edit", body(`{"field":"type","value":"interval"}`))
	res = formEditResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Trigger.Type != "interval" || res.Trigger.Schedule != nil ||
		strings.Join(res.Removed, ",") != "schedule,timezone" || strings.Contains(res.Content, "with:") {
		t.Fatalf("%s %v", rec.Body, err)
	}
	// A schedule that does not read: no words, no times; the problem comes from /validate.
	text = strings.Replace(text, "0 2 * * 1-5", "99 * * * *", 1)
	res = formEditResponse{}
	if err := json.Unmarshal(do(t, s, "POST", "/api/v1/forms/edit", body("")).Body.Bytes(), &res); err != nil || res.Trigger == nil || res.Trigger.Schedule != nil {
		t.Fatalf("%+v %v", res, err)
	}
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", body(`{"field":"target","value":null}`)), http.StatusBadRequest, "cannot be removed")
	if rec := do(t, s, "POST", "/api/v1/forms/edit", `{"path":"triggers/x.yaml","content":"with: [1]"}`); rec.Code != 200 || strings.Contains(rec.Body.String(), `"trigger"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

// GET /schemas/trigger lists the fields under with: of each type (E20a).
func TestTriggerSchemaVariants(t *testing.T) {
	_, s := newProject(t)
	var sc struct {
		Properties map[string]struct {
			Variants map[string]struct {
				Properties map[string]struct {
					Group string `json:"x-runnel-group"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"x-runnel-variants"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(do(t, s, "GET", "/api/v1/schemas/trigger", "").Body.Bytes(), &sc); err != nil {
		t.Fatal(err)
	}
	v := sc.Properties["with"].Variants
	if len(v) != 4 || len(v["cron"].Properties) != 3 || v["cron"].Properties["schedule"].Group != "When" || v["http"].Properties["auth"].Group != "Sign-in" ||
		strings.Join(v["http"].Required, ",") != "method,path,auth" {
		t.Fatalf("%+v", v)
	}
	if _, ok := v["interval"].Properties["schedule"]; ok {
		t.Error("schedule offered for interval")
	}
}

func TestTypeText(t *testing.T) {
	for _, c := range [][3]string{
		{"file: folder to watch; http: path of the address", "http", "Path of the address"},
		{"file: folder to watch; http: path of the address", "file", "Folder to watch"},
		{"cron, interval, file: what happens while the previous run still runs", "interval", "What happens while the previous run still runs"},
		{"Values of the target; empty: none", "cron", "Values of the target; empty: none"},
	} {
		if got := typeText(c[0], c[1]); got != c[2] {
			t.Errorf("%q for %s: %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// The forms of environments and project.yaml (E20b): variables as written, the secrets
// of secretStore: inline only by name, never their values.
func TestEditSettingsForm(t *testing.T) {
	_, s := newProject(t)
	env := "kind: Environment\napiVersion: runnel/v1\nname: prod\nvariables:\n  HOST: db01\n  PORT: 1433\nsecretStore: inline\nsecrets:\n  token: Pa55word\n"
	b, _ := json.Marshal(map[string]any{"path": "environments/prod.yaml", "content": env, "set": map[string]any{"field": "secrets.api", "value": "an0ther"}})
	rec := do(t, s, "POST", "/api/v1/forms/edit", string(b))
	var res formEditResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Environment == nil || res.Environment.Name != "prod" ||
		res.Environment.Variables["HOST"] != "db01" || strings.Join(res.Environment.SecretNames, ",") != "api,token" || res.Environment.SecretStore != "inline" {
		t.Fatalf("%s %v", rec.Body, err)
	}
	// The values are in the text the editor holds, but not in the fields of the form.
	if form, _ := json.Marshal(res.Environment); strings.Contains(string(form), "Pa55word") || strings.Contains(string(form), "an0ther") {
		t.Errorf("secret value in the form: %s", form)
	}
	b, _ = json.Marshal(map[string]any{"path": "project.yaml", "content": "kind: Project\napiVersion: runnel/v1\nname: demo\n", "set": map[string]any{"field": "defaultEnvironment", "value": "dev"}})
	res = formEditResponse{}
	if err := json.Unmarshal(do(t, s, "POST", "/api/v1/forms/edit", string(b)).Body.Bytes(), &res); err != nil || res.Project == nil ||
		res.Project.DefaultEnvironment != "dev" || res.Content != "kind: Project\napiVersion: runnel/v1\nname: demo\ndefaultEnvironment: dev\n" {
		t.Fatalf("%+v %v", res, err)
	}
	b, _ = json.Marshal(map[string]any{"path": "environments/prod.yaml", "content": env, "set": map[string]any{"field": "name", "value": "qa"}})
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", string(b)), http.StatusBadRequest, "cannot be set")
	expect(t, do(t, s, "POST", "/api/v1/forms/edit", `{"path":"data/in.csv","content":""}`), http.StatusBadRequest, "only connection, trigger and environment files")
}
