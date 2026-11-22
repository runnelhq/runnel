package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateText checks project.yaml, environments and triggers from the text in the
// editor (E10): problems with line and column, nothing for valid files.
func TestValidateText(t *testing.T) {
	dir, s := newProject(t)
	writeFile(t, filepath.Join(dir, "triggers", "nightly.yaml"),
		"kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/import.workflow.yaml\nwith: { schedule: \"0 2 * * *\" }\n")
	for _, tt := range []struct {
		path, content string
		want          []string // messages (substrings), in order; none: valid
	}{
		{"project.yaml", "kind: Project\napiVersion: runnel/v1\nname: demo\ndefaultEnvironment: dev\n", nil},
		{"project.yaml", "kind: Project\napiVersion: runnel/v1\nname: demo\ndefaultEnvironment: prod\n", []string{`default environment "prod" not found`}},
		{"project.yaml", "kind: Project\napiVersion: runnel/v1\nname: demo\nvariables: { 1x: a }\n", []string{`invalid variable name "1x"`}},
		{"environments/dev.yaml", "kind: Environment\napiVersion: runnel/v1\nname: dev\n", nil},
		{"environments/dev.yaml", "kind: Environment\napiVersion: runnel/v1\nname: prod\n", []string{`must match the file name`}},
		{"environments/dev.yaml", "kind: Environment\napiVersion: runnel/v1\nname: dev\nvariables: [\n", []string{""}},
		// Secrets in plain text (S39a2): a warning at secrets:, in the environment and in project.yaml.
		{"environments/dev.yaml", "kind: Environment\napiVersion: runnel/v1\nname: dev\nsecretStore: inline\nsecrets: { api-token: dev-123 }\n", []string{"secrets in plain text (secretStore: inline)"}},
		{"project.yaml", "kind: Project\napiVersion: runnel/v1\nname: demo\nsecrets: { api-token: x }\n", []string{"secrets in plain text"}},
		{"triggers/nightly.yaml", "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/import.workflow.yaml\nwith: { schedule: \"0 3 * * *\" }\n", nil},
		{"triggers/new.yaml", "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/none.workflow.yaml\nwith: { schedule: \"0 3 * * *\" }\n",
			[]string{`trigger name "nightly" is already used by nightly.yaml`, "none.workflow.yaml"}},
		// In a subfolder of triggers/ (E12): checked as a trigger, the name against all.
		{"triggers/nightly/new.yaml", "kind: Trigger\napiVersion: runnel/v1\nname: nightly\ntype: cron\ntarget: workflows/import.workflow.yaml\nwith: { schedule: \"0 3 * * *\" }\n",
			[]string{`trigger name "nightly" is already used by nightly.yaml`}},
		{"triggers/new.yaml", "kind: Trigger\napiVersion: runnel/v1\nname: x\ntype: cron\ntarget: workflows/import.workflow.yaml\nwith: { schedule: \"soon\" }\n", []string{"schedule"}},
	} {
		rec := do(t, s, "POST", "/api/v1/validate", mustJSON(t, map[string]string{"path": tt.path, "content": tt.content}))
		var res validateResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s %v", tt.path, rec.Code, rec.Body, err)
		}
		found := 0
		for _, p := range res.Problems {
			if found < len(tt.want) && strings.Contains(p.Message, tt.want[found]) {
				if p.Line == 0 && tt.want[found] != "" {
					t.Errorf("%s: %q has no line", tt.path, p.Message)
				}
				found++
			}
		}
		if found != len(tt.want) || (tt.want == nil && len(res.Problems) > 0) {
			t.Errorf("%s with %q: problems %+v; want %q", tt.path, tt.content, res.Problems, tt.want)
		}
	}
}
