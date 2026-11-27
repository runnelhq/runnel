package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runnelhq/runnel/internal/runs"
)

const scriptWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: script
actions:
  - { id: start, type: core.start }
  - id: sum
    type: core.script
    with:
      params:
        key: ${secret:RUNNEL_TEST_SRV_KEY}
        target: ${TARGET}
      script: |
        return { echo: "key " + params.key, target: params.target };
links:
  - { from: start, to: sum }
`

// The data of the steps of a run of the editor (E23a): the list of the actions with
// data and the data of one, with secrets only as reference or ***.
func TestStepData(t *testing.T) {
	t.Setenv("RUNNEL_TEST_SRV_KEY", "srv-Pa55word")
	dir, s := newRunProject(t)
	writeFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: demo\nsecretStore: env\n")
	writeFile(t, filepath.Join(dir, "workflows", "script.workflow.yaml"), scriptWorkflow)
	run := startRun(t, s, `{"path": "workflows/script.workflow.yaml", "environment": "dev"}`)
	if d := waitRun(t, s, run.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning }); d.Status != "succeeded" {
		t.Fatalf("%s: %s", d.Status, d.Error)
	}

	rec := do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps", "")
	expect(t, rec, http.StatusOK, `"actionId": "sum"`)
	rec = do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum", "")
	if strings.Contains(rec.Body.String(), "srv-Pa55word") {
		t.Errorf("secret value in the response: %s", rec.Body)
	}
	var d runs.StepData
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d: %s", rec.Code, rec.Body)
	}
	params, _ := d.Input["params"].(map[string]any)
	out, _ := d.Output.(map[string]any)
	if d.RunID != run.ID || d.Status != "succeeded" || d.Run != 1 || params["key"] != "${secret:RUNNEL_TEST_SRV_KEY}" ||
		params["target"] != "dev" || d.InputSources["params.target"] != "${TARGET}" || out["echo"] != "key ***" || out["target"] != "dev" ||
		len(d.Secrets) != 1 || d.Secrets[0] != "params.key" {
		t.Errorf("%s", rec.Body)
	}

	// Refused: unknown run or action, numbers that are none, other origins and hosts.
	expect(t, do(t, s, "GET", "/api/v1/runs/01NOPE/steps", ""), http.StatusNotFound, "no data kept for this run")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/nope", ""), http.StatusNotFound, "no data for this step")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum?run=2", ""), http.StatusNotFound, "no data for this step")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum?entry=1", ""), http.StatusNotFound, "no data for this step")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum?run=x", ""), http.StatusBadRequest, "expected a number from 1")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum?entry=0", ""), http.StatusBadRequest, "invalid entry")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum", "", "Origin", "http://evil.example"), http.StatusForbidden, "other origins")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps", "", "Host", "evil.example"), http.StatusForbidden, "localhost")

	// A run of a trigger ("Run target now") keeps no data and leaves that of the editor's run.
	writeFile(t, filepath.Join(dir, "triggers", "hourly.yaml"), "kind: Trigger\napiVersion: runnel/v1\nname: hourly\ntype: interval\ntarget: workflows/wait.workflow.yaml\nenabled: false\nwith:\n  every: 1h\n")
	other := startRun(t, s, `{"trigger": "triggers/hourly.yaml"}`)
	waitRun(t, s, other.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	expect(t, do(t, s, "GET", "/api/v1/runs/"+other.ID+"/steps/wait", ""), http.StatusNotFound, "no data kept for this run")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum", ""), http.StatusOK, `"runId"`)

	// A new run of the editor drops the data of the previous one.
	next := startRun(t, s, `{"path": "workflows/wait.workflow.yaml", "params": {"DELAY": "1ms"}}`)
	waitRun(t, s, next.ID, func(d runs.Detail) bool { return d.Status != runs.StatusRunning })
	expect(t, do(t, s, "GET", "/api/v1/runs/"+run.ID+"/steps/sum", ""), http.StatusNotFound, "no data kept for this run")
	expect(t, do(t, s, "GET", "/api/v1/runs/"+next.ID+"/steps/wait", ""), http.StatusOK, `"duration": "1ms"`)
}
