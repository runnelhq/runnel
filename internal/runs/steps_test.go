package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/vars"
)

// stepAction is an action for the tests of the step data: it echoes its secret in the
// output, fails on request, the first attempts when flaky, and returns size entries.
type stepAction struct {
	Text       string `json:"text" doc:"Text"`
	Password   string `json:"password" doc:"Password" schema:"format=runnel-secret"`
	Connection string `json:"connection" doc:"Connection" schema:"connection=http"`
	Fail       string `json:"fail" doc:"Fails if true"`
	Flaky      int    `json:"flaky" doc:"Attempts that fail first"`
	Size       int    `json:"size" doc:"Entries of the output"`
}

var flaky = struct {
	sync.Mutex
	n map[string]int
}{n: map[string]int{}}

func (a *stepAction) Execute(_ context.Context, ec *action.ExecContext) (action.Result, error) {
	flaky.Lock()
	flaky.n[ec.RunID+a.Text]++
	n := flaky.n[ec.RunID+a.Text]
	flaky.Unlock()
	if n <= a.Flaky {
		return action.Result{}, fmt.Errorf("flaky attempt %d", n)
	}
	if a.Fail == "true" {
		return action.Result{}, errors.New("failed " + a.Text + " with " + a.Password)
	}
	out := map[string]any{"text": a.Text, "echo": "token " + a.Password}
	if a.Size > 0 {
		items := make([]any, a.Size)
		for i := range items {
			items[i] = fmt.Sprintf("item %06d %s", i, strings.Repeat("x", 90))
		}
		out["items"] = items
	}
	return action.Result{Output: out}, nil
}

func stepRegistry() *action.Registry {
	reg := secretRegistry()
	reg.Register(action.Definition{
		Type: "test.step", Category: action.CategoryGeneral, Label: "Step", Description: "Echoes its input for a test",
		Config: stepAction{},
		New: func(with map[string]any) (action.Action, error) {
			a := &stepAction{}
			return a, action.DecodeConfig(with, a)
		},
	})
	return reg
}

const stepsWorkflow = `kind: Workflow
apiVersion: runnel/v1
name: steps
parameters:
  - { name: NAME, default: ada }
actions:
  - { id: start, type: core.start }
  - id: echo
    type: test.step
    retry: { attempts: 3 }
    with:
      text: "hello {{ params.NAME }}"
      password: ${secret:RUNNEL_TEST_PW}
      connection: api
      flaky: 1
  - id: each
    type: test.step
    forEach: "{{ [1, 2, 3] }}"
    continueOnEntryError: true
    with:
      text: "row {{ row }}"
      password: ${secret:RUNNEL_TEST_PW}
      fail: "{{ row == 2 }}"
  - { id: never, type: core.log, with: { message: x } }
links:
  - { from: start, to: echo }
  - { from: echo, to: each }
  - { from: each, to: never }
`

// stepsProject is a project with the secret store env and the connection api.
func stepsProject(t *testing.T) string {
	t.Helper()
	t.Setenv("RUNNEL_TEST_PW", "Pa55word!")
	t.Setenv("RUNNEL_TEST_TOKEN", "t0ken-Secret")
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "project.yaml"), "kind: Project\napiVersion: runnel/v1\nname: t\nsecretStore: env\nvariables:\n  HOST: api.example.com\n")
	writeTestFile(t, filepath.Join(dir, "connections", "api.yaml"), "kind: Connection\napiVersion: runnel/v1\nname: api\ntype: http\n"+
		"with:\n  baseUrl: https://${HOST}/v1\n  auth: bearer\n  token: ${secret:RUNNEL_TEST_TOKEN}\n")
	return dir
}

// runSteps runs a workflow of the project with or without step data and waits for its end.
func runSteps(t *testing.T, m *Manager, dir, wf string, keep bool) string {
	t.Helper()
	run, _, err := m.Start("workflows/steps.workflow.yaml", Spec{Name: "workflows/steps.workflow.yaml", Data: []byte(wf),
		ProjectDir: dir, Registry: stepRegistry(), StepData: keep})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, m, run.ID, finished)
	return run.ID
}

func stepData(t *testing.T, m *Manager, id, action string, run, entry int) StepData {
	t.Helper()
	d, err := m.StepData(id, action, run, entry)
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	data, _ := json.Marshal(d)
	for _, secret := range []string{"Pa55word!", "t0ken-Secret"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("%s: secret value in the step data: %s", action, data)
		}
	}
	return d
}

// The data of each step of a run of the editor: the input after variables and
// expressions with its sources, secrets as reference, the resolved connection, the
// output with known secrets masked, attempts, entries of forEach and skipped steps.
func TestStepDataCapture(t *testing.T) {
	dir := stepsProject(t)
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	id := runSteps(t, m, dir, stepsWorkflow, true)

	echo := stepData(t, m, id, "echo", 0, 0)
	if echo.Status != workflow.Succeeded || echo.Attempts != 2 || !reflect.DeepEqual(echo.AttemptErrors, []string{"flaky attempt 1"}) {
		t.Errorf("echo: %s, %d attempts %v", echo.Status, echo.Attempts, echo.AttemptErrors)
	}
	if echo.Input["text"] != "hello ada" || echo.InputSources["text"] != "hello {{ params.NAME }}" ||
		echo.Input["password"] != "${secret:RUNNEL_TEST_PW}" || !reflect.DeepEqual(echo.Secrets, []string{"password"}) {
		t.Errorf("echo input: %v, sources %v, secrets %v", echo.Input, echo.InputSources, echo.Secrets)
	}
	if _, ok := echo.InputSources["password"]; ok {
		t.Errorf("source of a secret: %v", echo.InputSources)
	}
	c := echo.Connection
	if c == nil || c.Name != "api" || c.Type != "http" || c.With["baseUrl"] != "https://api.example.com/v1" ||
		c.With["token"] != "${secret:RUNNEL_TEST_TOKEN}" || !reflect.DeepEqual(c.Secrets, []string{"token"}) {
		t.Errorf("connection: %+v", c)
	}
	if out, _ := echo.Output.(map[string]any); out["echo"] != "token ***" || out["text"] != "hello ada" {
		t.Errorf("echo output: %v", echo.Output)
	}

	each := stepData(t, m, id, "each", 0, 0)
	want := []EntryState{{1, workflow.Succeeded}, {2, workflow.Failed}, {3, workflow.Succeeded}}
	if each.Status != workflow.Failed || !reflect.DeepEqual(each.Entries, want) || each.Input != nil ||
		!strings.Contains(each.Error, "1 of 3 entries failed") {
		t.Errorf("each: %s %v input %v error %q", each.Status, each.Entries, each.Input, each.Error)
	}
	if out, _ := each.Output.(map[string]any); out["failed"] != int64(1) {
		t.Errorf("each output: %v", each.Output)
	}
	e2 := stepData(t, m, id, "each", 0, 2)
	if e2.Entry != 2 || e2.EntryStatus != workflow.Failed || e2.Error != "failed row 2 with ***" || e2.Input["text"] != "row 2" ||
		e2.InputSources["text"] != "row {{ row }}" || e2.Row != 2 || e2.Attempts != 1 || e2.Output != nil {
		t.Errorf("entry 2: %+v", e2)
	}

	never := stepData(t, m, id, "never", 0, 0)
	if never.Status != workflow.Skipped || never.Reason != workflow.SkipReason || never.Input != nil {
		t.Errorf("never: %+v", never)
	}

	steps, err := m.Steps(id)
	if err != nil || len(steps) != 4 || steps[1].ActionID != "echo" || steps[2].Entries != 3 || steps[3].Status != workflow.Skipped {
		t.Errorf("steps %+v, %v", steps, err)
	}
	for _, c := range []struct {
		action      string
		run, entry  int
		want        error
		description string
	}{
		{"nope", 0, 0, ErrNoStep, "unknown action"},
		{"echo", 2, 0, ErrNoStep, "unknown execution"},
		{"echo", 0, 1, ErrNoStep, "entry without forEach"},
		{"each", 0, 4, ErrNoStep, "unknown entry"},
	} {
		if _, err := m.StepData(id, c.action, c.run, c.entry); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.description, err)
		}
	}
}

// Runs not started in the editor keep nothing; a new run of the editor drops the data
// of the previous one.
func TestStepDataOnlyLastEditorRun(t *testing.T) {
	dir := stepsProject(t)
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	other := runSteps(t, m, dir, stepsWorkflow, false)
	if _, err := m.Steps(other); !errors.Is(err, ErrNoStepData) {
		t.Errorf("run without step data: %v", err)
	}
	first := runSteps(t, m, dir, stepsWorkflow, true)
	second := runSteps(t, m, dir, stepsWorkflow, true)
	if _, err := m.StepData(first, "echo", 0, 0); !errors.Is(err, ErrNoStepData) {
		t.Errorf("previous run: %v", err)
	}
	if _, err := m.StepData(second, "echo", 0, 0); err != nil {
		t.Errorf("last run: %v", err)
	}
	// A run without step data (e.g. of a trigger) keeps the data of the editor's last run.
	runSteps(t, m, dir, stepsWorkflow, false)
	if _, err := m.Steps(second); err != nil {
		t.Errorf("after another run: %v", err)
	}
	_ = m.Shutdown(context.Background())
	if _, err := m.Steps(second); !errors.Is(err, ErrNoStepData) {
		t.Errorf("after shutdown: %v", err)
	}
}

// An action that starts more than once (join each) keeps each execution.
func TestStepDataRuns(t *testing.T) {
	dir := stepsProject(t)
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	id := runSteps(t, m, dir, `kind: Workflow
apiVersion: runnel/v1
name: steps
actions:
  - { id: start, type: core.start }
  - { id: a, type: core.wait, with: { duration: 1ms } }
  - { id: b, type: core.wait, with: { duration: 20ms } }
  - { id: c, type: test.step, join: each, with: { text: "after {{ result.id }}" } }
links:
  - { from: start, to: a }
  - { from: start, to: b }
  - { from: a, to: c }
  - { from: b, to: c }
`, true)
	first, second := stepData(t, m, id, "c", 1, 0), stepData(t, m, id, "c", 0, 0)
	if first.Run != 1 || first.Runs != 2 || first.Input["text"] != "after a" || second.Run != 2 || second.Input["text"] != "after b" {
		t.Errorf("first %+v\nsecond %+v", first, second)
	}
}

// A step keeps at most MaxStepBytes of its output; a run at most MaxStepDataBytes,
// further steps only their status.
func TestStepDataLimits(t *testing.T) {
	dir := stepsProject(t)
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	id := runSteps(t, m, dir, `kind: Workflow
apiVersion: runnel/v1
name: steps
actions:
  - { id: start, type: core.start }
  - id: big
    type: test.step
    forEach: "{{ map(1..30, #) }}"
    with: { text: "entry {{ row }}", size: 15000 }
links:
  - { from: start, to: big }
`, true)
	if d, _ := get(m, id); d.Status != workflow.Succeeded {
		t.Fatalf("%s: %s", d.Status, d.Error)
	}
	e1 := stepData(t, m, id, "big", 0, 1)
	items, _ := e1.Output.(map[string]any)["items"].([]any)
	if len(e1.Truncated) != 3 || len(items) == 0 || len(items) >= 15000 {
		t.Fatalf("entry 1: %d items, %+v", len(items), e1.Truncated)
	}
	// The last entry kept is cut, then the list and the object with it.
	last, cut := e1.Truncated[0], e1.Truncated[1]
	if last.Path != fmt.Sprintf("items[%d]", len(items)-1) || last.Kind != "text" ||
		cut.Part != "output" || cut.Path != "items" || cut.Kind != "list" || cut.Length != 15000 || cut.Kept != len(items) ||
		e1.Truncated[2].Kind != "object" || e1.Truncated[2].Path != "" {
		t.Errorf("truncation: %+v", e1.Truncated)
	}
	if size, _ := json.Marshal(e1.Output); len(size) > MaxStepBytes+1000 || len(size) < MaxStepBytes*9/10 {
		t.Errorf("output of %d bytes", len(size))
	}
	dropped := 0
	for i := 1; i <= 30; i++ {
		if d := stepData(t, m, id, "big", 0, i); d.Dropped {
			dropped++
			if d.Output != nil || d.EntryStatus != workflow.Succeeded {
				t.Errorf("dropped entry %d: %+v", i, d)
			}
		}
	}
	if dropped < 5 || dropped > 15 {
		t.Errorf("%d of 30 entries dropped", dropped)
	}
}

// Cutting is deterministic: lists and objects keep their first entries, texts their
// beginning (never in the middle of a character); secrets are masked first.
func TestLimitValue(t *testing.T) {
	mask := &vars.Mask{}
	mask.Add("Pa55word!")
	long := strings.Repeat("ä", 100)
	v := map[string]any{"b": []any{"x Pa55word!", long, "z"}, "a": 1, "first name": "ada"}
	got, n, cuts := limitValue(v, "output", 80, mask)
	again, _, _ := limitValue(v, "output", 80, mask)
	if !reflect.DeepEqual(got, again) {
		t.Errorf("not deterministic: %v / %v", got, again)
	}
	m := got.(map[string]any)
	b := m["b"].([]any)
	if m["a"] != 1 || b[0] != "x ***" || len(b) != 2 || n > 80 || n < 70 {
		t.Fatalf("%v (%d bytes)", got, n)
	}
	text := b[1].(string)
	if !strings.HasPrefix(long, text) || len(text)%2 != 0 || len(text) == 0 {
		t.Errorf("text cut to %q", text)
	}
	want := []Truncation{{Part: "output", Path: "b[1]", Kind: "text", Length: 200, Kept: len(text)},
		{Part: "output", Path: "b", Kind: "list", Length: 3, Kept: 2},
		{Part: "output", Kind: "object", Length: 3, Kept: 2}}
	if !reflect.DeepEqual(cuts, want) {
		t.Errorf("cuts %+v", cuts)
	}
	if p := keyPath("a", "first name"); p != `a["first name"]` {
		t.Errorf("path %s", p)
	}
	// Types other than texts, numbers, lists and maps as JSON.
	type point struct{ X int }
	if got, _, _ := limitValue([]map[string]any{{"p": point{1}}}, "output", 100, mask); !reflect.DeepEqual(got, []any{map[string]any{"p": map[string]any{"X": 1.0}}}) {
		t.Errorf("%#v", got)
	}
}

// Fields for secrets show their reference, or *** for a text without one; mappings
// value by value.
func TestSecretRef(t *testing.T) {
	got := secretRef(map[string]any{"TOKEN": "${secret:t}", "PLAIN": "abc", "N": 1})
	if !reflect.DeepEqual(got, map[string]any{"TOKEN": "${secret:t}", "PLAIN": "***", "N": "***"}) {
		t.Errorf("%v", got)
	}
}

// The prepared recorder is only set when asked for.
func TestStepDataOnlyWhenAsked(t *testing.T) {
	dir := stepsProject(t)
	p, err := Prepare(Spec{Name: "workflows/steps.workflow.yaml", Data: []byte(stepsWorkflow), ProjectDir: dir, Registry: stepRegistry()})
	if err != nil || p.steps != nil {
		t.Errorf("%v, %v", p.steps, err)
	}
}

// Without continueOnEntryError the first failed entry stops forEach: the later entries
// did not run.
func TestStepDataForEachStops(t *testing.T) {
	dir := stepsProject(t)
	m := NewManager()
	defer func() { _ = m.Shutdown(context.Background()) }()
	id := runSteps(t, m, dir, strings.Replace(stepsWorkflow, "    continueOnEntryError: true\n", "", 1), true)
	each := stepData(t, m, id, "each", 0, 0)
	want := []EntryState{{1, workflow.Succeeded}, {2, workflow.Failed}, {3, workflow.Skipped}}
	if each.Status != workflow.Failed || !reflect.DeepEqual(each.Entries, want) {
		t.Errorf("each: %s %v", each.Status, each.Entries)
	}
	if e3 := stepData(t, m, id, "each", 0, 3); e3.Input != nil || e3.Attempts != 0 {
		t.Errorf("entry 3: %+v", e3)
	}
}

// benchOutput is the output of a step like a page of an API: 2,000 objects.
func benchOutput() map[string]any {
	items := make([]any, 2000)
	for i := range items {
		items[i] = map[string]any{"id": int64(i), "customer": map[string]any{"name": fmt.Sprintf("Customer %d", i), "city": "Berlin"},
			"total": 19.99, "paid": i%2 == 0, "note": strings.Repeat("n", 200)}
	}
	return map[string]any{"items": items, "next": "https://api.example.com/v1/orders?page=2"}
}

// Copying, masking and cutting the output of a step (about 650 KB as JSON; with a
// limit of 64 KB it is cut).
func BenchmarkLimitValue(b *testing.B) {
	out := benchOutput()
	mask := &vars.Mask{}
	mask.Add("Pa55word!")
	for _, budget := range []int{MaxStepBytes, 64 << 10} {
		b.Run(fmt.Sprintf("budget=%dKB", budget>>10), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				limitValue(out, "output", budget, mask)
			}
		})
	}
}

// A run with forEach over 500 entries, without (as for runnel run, triggers and the
// service) and with the data of the steps (runs of the editor).
func BenchmarkForEachStepData(b *testing.B) {
	const wf = `kind: Workflow
apiVersion: runnel/v1
name: each
actions:
  - { id: start, type: core.start }
  - id: each
    type: test.step
    forEach: "{{ map(1..500, #) }}"
    with: { text: "row {{ row }}", size: 10 }
links:
  - { from: start, to: each }
`
	reg := stepRegistry()
	for _, keep := range []bool{false, true} {
		b.Run(fmt.Sprintf("stepData=%v", keep), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				p, err := Prepare(Spec{Name: "each.workflow.yaml", Data: []byte(wf), Registry: reg, StepData: keep})
				if err != nil {
					b.Fatal(err)
				}
				if res := p.Runner.Run(context.Background()); res.Status != workflow.Succeeded {
					b.Fatal(res.Err)
				}
			}
		})
	}
}
