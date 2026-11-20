package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// timestamps and run ids vary between runs; replace them for comparisons.
var (
	timePattern  = regexp.MustCompile(`(?m)^\d\d:\d\d:\d\d\.\d{3} `)
	runIDPattern = regexp.MustCompile(`run [0-9A-HJKMNP-TV-Z]{26}`)
	durPattern   = regexp.MustCompile(`(in|after) [0-9.]+(ms|µs|s)`)
)

func normalizeLog(s string) string {
	s = timePattern.ReplaceAllString(s, "TIME ")
	s = runIDPattern.ReplaceAllString(s, "run RUNID")
	return durPattern.ReplaceAllString(s, "$1 DURATION")
}

func TestRunHello(t *testing.T) {
	chdirRepoRoot(t)
	t.Setenv("COMPUTERNAME", "TESTPC")
	t.Setenv("TEMP", `C:\Temp`)

	code, out, errOut := run(t, "run", "--env", "prod", "--param", "GREETING=Moin", "examples/hello/workflows/hello.workflow.yaml")
	if code != 0 || errOut != "" {
		t.Fatalf("exit code %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	want := `TIME INFO  workflow "hello" started (environment prod), run RUNID
TIME INFO  [greet] Moin, Production! (production on TESTPC, files in \\fileserver\exchange\runnel-hello)
TIME INFO  output MESSAGE = "Moin, Production!"
TIME INFO  workflow "hello" succeeded in DURATION
`
	if got := normalizeLog(out); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunVerbose(t *testing.T) {
	chdirRepoRoot(t)
	t.Setenv("COMPUTERNAME", "TESTPC")
	t.Setenv("TEMP", `C:\Temp`)

	code, out, _ := run(t, "run", "-v", "examples/hello/workflows/hello.workflow.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s", code, out)
	}
	for _, want := range []string{
		`workflow "hello" started (environment dev)`,
		"DEBUG [pause] started (core.wait)",
		"DEBUG [pause] waiting 1s",
		"[greet] Hello, World! (development on TESTPC, files in C:\\Temp\\runnel-hello)",
		"DEBUG [done] succeeded in",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunExpressionsExample(t *testing.T) {
	chdirRepoRoot(t)
	for _, tt := range []struct {
		orders string
		want   []string
	}{
		{"", []string{"[few] 7 orders – importing at once", `output SIZE = "small"`}},
		{"25", []string{"[many] 25 orders – more than 10, splitting the import", `output SIZE = "large"`}},
	} {
		args := []string{"run", "examples/expressions/workflows/orders.workflow.yaml"}
		if tt.orders != "" {
			args = []string{"run", "--param", "ORDERS=" + tt.orders, args[1]}
		}
		code, out, _ := run(t, args...)
		if code != 0 {
			t.Fatalf("exit code %d:\n%s", code, out)
		}
		for _, want := range append(tt.want, "Checking", `output CHECKED = "succeeded after`) {
			if !strings.Contains(out, want) {
				t.Errorf("ORDERS=%q: missing %q in:\n%s", tt.orders, want, out)
			}
		}
	}
}

func TestRunFilesExample(t *testing.T) {
	chdirRepoRoot(t)
	archive := t.TempDir()
	code, out, errOut := run(t, "run", "--param", "ARCHIVE="+archive, "examples/files/workflows/archive.workflow.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "[copy] copied 2 files to ") || !strings.Contains(out, "[report] Archived 2 files, 81 bytes in the folder") ||
		!strings.Contains(out, `output FILES = "2"`) {
		t.Errorf("output:\n%s", out)
	}
	day, err := os.ReadDir(archive)
	if err != nil || len(day) != 1 {
		t.Fatalf("archive: %v %v", day, err)
	}
	files, _ := os.ReadDir(filepath.Join(archive, day[0].Name()))
	if len(files) != 2 || files[0].Name() != "orders-1.csv" {
		t.Errorf("archived files: %v", files)
	}

	// Without order files, the failure link reports it and the workflow succeeds.
	code, out, _ = run(t, "run", "--param", "INBOX="+t.TempDir(), "--param", "ARCHIVE="+archive, "examples/files/workflows/archive.workflow.yaml")
	if code != 0 || !strings.Contains(out, "[empty] No order files in") || !strings.Contains(out, `output FILES = "0"`) {
		t.Errorf("empty inbox: exit code %d:\n%s", code, out)
	}
}

func TestRunSubWorkflowsExample(t *testing.T) {
	chdirRepoRoot(t)
	code, out, errOut := run(t, "run", "examples/sub-workflows/workflows/nightly.workflow.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"[north] import-region/check: Importing 12 orders of region north",
		"[south] import-region/check: Importing 25 orders of region south",
		"[report] north: 12 orders, south: 25 orders",
		"output TOTAL = 37",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	code, out, _ = run(t, "run", "--param", "SOUTH_ORDERS=500", "examples/sub-workflows/workflows/nightly.workflow.yaml")
	if code != 1 || !strings.Contains(out, "[failed] Import failed: workflow workflows/import-region.workflow.yaml failed: 500 orders in region south") {
		t.Errorf("too many orders: exit code %d:\n%s", code, out)
	}
}

func TestRunPipelineExample(t *testing.T) {
	chdirRepoRoot(t)
	code, out, errOut := run(t, "run", "--param", "ROWS=100", "examples/pipelines/pipelines/numbers.pipeline.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		`pipeline "numbers" started, run `,
		"[numbers] read 0, written 100 in ",
		"[both] read 200, written 200 in ",
		"[show] read 200, written 200 in ",
		`pipeline "numbers" succeeded in `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// A value that is not a whole number is reported when the transform runs.
	code, out, _ = run(t, "run", "--param", "ROWS=-5", "examples/pipelines/pipelines/numbers.pipeline.yaml")
	if code != 1 || !strings.Contains(out, `[numbers] failed after`) || !strings.Contains(out, `rows: "-5" is not a whole number of 0 or more`) {
		t.Errorf("exit code %d:\n%s", code, out)
	}
}

func TestRunCSVExample(t *testing.T) {
	chdirRepoRoot(t)
	target := filepath.Join(t.TempDir(), "orders.csv")
	code, out, errOut := run(t, "run", "--param", "OUTPUT="+target, "examples/pipelines/pipelines/orders.pipeline.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"[read] read 3 rows from examples/pipelines/data/orders.csv",
		"[large] kept 2 of 3 records, dropped 1",
		"[show] #1 {Nr: 1001, Betrag: 1234.5}",
		"[write] wrote 2 rows to " + target,
		`pipeline "orders" succeeded in `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	// The order of 89,90 is below MIN_AMOUNT (100).
	want := "\xEF\xBB\xBFNr,Datum,Kunde,Betrag\r\n1001,2026-11-19,Müller GmbH,1234.50\r\n" +
		"1003,2026-11-20,\"Weber, Anna\",12500.00\r\n"
	if string(b) != want {
		t.Errorf("written file %q, want %q", b, want)
	}
}

func TestRunBasicTransformsExample(t *testing.T) {
	chdirRepoRoot(t)
	target := filepath.Join(t.TempDir(), "crm.csv")
	rejected := filepath.Join(t.TempDir(), "rejected.csv")
	code, out, errOut := run(t, "run", "--param", "BATCH=42", "--param", "OUTPUT="+target, "--param", "REJECTED="+rejected,
		"examples/pipelines/pipelines/customers.pipeline.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{"[types] read 4, written 3, errors 1 in ", "[active] kept 2 of 3 records, dropped 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The customer with "k. A." went over the error link, with the reason.
	b, err := os.ReadFile(rejected)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\xEF\xBB\xBFkdnr;name;umsatz;ort;telefon;_error;_errorField;_errorTransform\r\n" +
		"4714;Klein KG;k. A.;Essen;;\"record 4, field \"\"umsatz\"\": \"\"k. A.\"\" is not a number\";umsatz;types\r\n"; string(b) != want {
		t.Errorf("rejected %q, want %q", b, want)
	}
	b, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := "\xEF\xBB\xBFexternalId,companyName,revenue,source,batch\r\n4711,Müller GmbH,12500,runnel,42\r\n" +
		"4713,\"Weber, Anna\",980.5,runnel,42\r\n"
	if string(b) != want {
		t.Errorf("written file %q, want %q", b, want)
	}
}

func TestRunSwitchExample(t *testing.T) {
	chdirRepoRoot(t)
	dir := t.TempDir()
	code, out, errOut := run(t, "run", "--param", "OUT_DIR="+dir, "examples/pipelines/pipelines/segments.pipeline.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"[read] read 4 rows from examples/pipelines/data/customers.csv, 1 with errors",
		`[invalid] #1 {kdnr: "4714", name: "Klein KG", umsatz: "k. A.", _error: `,
		"[segment] key-accounts 1, standard 1, dropped 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for file, want := range map[string]string{
		"customers-key-accounts.csv": "\xEF\xBB\xBFkdnr,name,umsatz\r\n4711,Müller GmbH,12500\r\n",
		"customers-standard.csv":     "\xEF\xBB\xBFkdnr,name,umsatz\r\n4713,\"Weber, Anna\",980.5\r\n",
	} {
		if b, err := os.ReadFile(filepath.Join(dir, file)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v, want %q", file, b, err, want)
		}
	}
}

func TestRunParallelExample(t *testing.T) {
	chdirRepoRoot(t)
	code, out, errOut := run(t, "run", "--param", "ROWS=3000", "examples/pipelines/pipelines/parallel.pipeline.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"[check] read 3000, written 1000 in ",
		"[left] read 500, written 500 in ",
		"[right] read 500, written 500 in ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "[check] kept "); n != 4 {
		t.Errorf("%d log lines of the copies, want 4:\n%s", n, out)
	}
}

func TestRunPipelineFromWorkflowExample(t *testing.T) {
	chdirRepoRoot(t)
	dir := t.TempDir()
	code, out, errOut := run(t, "run", "-v", "--param", "OUT_DIR="+dir, "examples/pipelines/workflows/import.workflow.yaml")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		`[prepare] pipeline "customers" started, run `,
		"[prepare] customers/active: kept 2 of 3 records, dropped 1",
		"[prepare] customers/types: read 4, written 3, errors 1 in ",
		"[check] 1 customer(s) rejected, see customers-rejected.csv",
		"[done] Batch 7: 2 customer(s) exported",
		"output EXPORTED = 2",
		"output REJECTED = 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, file := range []string{"customers-crm.csv", "customers-rejected.csv"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			t.Errorf("%s: %v", file, err)
		}
	}
}

func writeWorkflow(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.workflow.yaml")
	if err := os.WriteFile(path, []byte("kind: Workflow\napiVersion: runnel/v1\nname: test\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunFailed(t *testing.T) {
	file := writeWorkflow(t, `
actions:
  - { id: start, type: core.start }
  - { id: check, type: core.wait, with: { duration: "${WAIT}" } }
  - { id: report, type: core.log, with: { message: "check failed, aborting" } }
  - { id: stop, type: core.abort, with: { message: "input not ready" } }
links:
  - { from: start, to: check }
  - { from: check, to: report, when: failure }
  - { from: report, to: stop }
`)
	code, out, _ := run(t, "run", "--param", "WAIT=later", file)
	if code != exitRunFailed {
		t.Errorf("exit code %d, want %d", code, exitRunFailed)
	}
	want := `TIME INFO  workflow "test" started, run RUNID
TIME ERROR [check] failed after DURATION: invalid duration "later" (examples: 30s, 5m, 2h, 1h30m)
TIME INFO  [report] check failed, aborting
TIME ERROR workflow "test" failed after DURATION: input not ready
`
	if got := normalizeLog(out); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunNotStarted(t *testing.T) {
	chdirRepoRoot(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"invalid workflow", []string{"run", "examples/broken/graph.workflow.yaml"}, `unknown action "imprt"`},
		{"invalid pipeline", []string{"run", "examples/broken/cycle.pipeline.yaml"}, "closes a cycle (read → filter → write → read)"},
		{"unknown action type", []string{"run", writeWorkflow(t, `
actions:
  - { id: start, type: core.start }
  - { id: copy, type: file.shred }
links:
  - { from: start, to: copy }
`)}, `unknown action type "file.shred"`},
		{"missing required parameter", []string{"run", writeWorkflow(t, `
parameters:
  - { name: TARGET, required: true }
actions:
  - { id: start, type: core.start }
`)}, "missing required parameter(s) [TARGET]"},
		{"parameter not in allowed values", []string{"run", "--param", "GREETING=Servus", "examples/hello/workflows/hello.workflow.yaml"},
			`parameter GREETING (string): "Servus" is not one of the allowed values Hello, Hi, Moin`},
		{"parameter of wrong type", []string{"run", "--param", "N=many", writeWorkflow(t, `
parameters:
  - { name: N, type: integer }
actions:
  - { id: start, type: core.start }
`)}, `parameter N (integer): "many" is not an integer`},
		{"unknown environment", []string{"run", "--env", "qa", "examples/hello/workflows/hello.workflow.yaml"}, `environment "qa" not found`},
		{"env without project", []string{"run", "--env", "dev", writeWorkflow(t, "actions:\n  - { id: start, type: core.start }\n")}, "no project (project.yaml) found"},
		{"directory instead of file", []string{"run", "examples/hello"}, "examples/hello is a directory; pass a workflow file"},
		{"missing file", []string{"run", "does-not-exist.workflow.yaml"}, "does-not-exist.workflow.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := run(t, tt.args...)
			if code != exitNotRun {
				t.Errorf("exit code %d, want %d", code, exitNotRun)
			}
			if out != "" {
				t.Errorf("unexpected run output:\n%s", out)
			}
			if !strings.Contains(errOut, tt.want) && !strings.Contains(strings.ToLower(errOut), tt.want) {
				t.Errorf("stderr %q, want containing %q", errOut, tt.want)
			}
		})
	}
}

func TestRunUndeclaredParamWarning(t *testing.T) {
	file := writeWorkflow(t, "actions:\n  - { id: start, type: core.start }\n")
	code, _, errOut := run(t, "run", "--param", "EXTRA=1", file)
	if code != 0 || !strings.Contains(errOut, `warning: parameter "EXTRA" is not declared`) {
		t.Errorf("exit code %d, stderr %q", code, errOut)
	}
}

func TestRunCancelled(t *testing.T) {
	file := writeWorkflow(t, `
actions:
  - { id: start, type: core.start }
  - { id: wait, type: core.wait, with: { duration: 1h } }
links:
  - { from: start, to: wait }
`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var out, errOut bytes.Buffer
	code := ExecuteContext(ctx, []string{"run", file}, &out, &errOut)
	if code != ExitCancelled || !strings.Contains(out.String(), `workflow "test" cancelled after`) {
		t.Errorf("exit code %d, output:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestRunActionTimeout(t *testing.T) {
	file := writeWorkflow(t, `
actions:
  - { id: start, type: core.start }
  - { id: wait, type: core.wait, timeout: 50ms, with: { duration: 1h } }
links:
  - { from: start, to: wait }
`)
	code, out, _ := run(t, "run", file)
	if code != exitRunFailed || !strings.Contains(out, "[wait] failed after") || !strings.Contains(out, "timed out after 50ms") {
		t.Errorf("exit code %d, output:\n%s", code, out)
	}
}

func TestRunCollectorExample(t *testing.T) {
	chdirRepoRoot(t)
	code, out, errOut := run(t, "run", "-v", "examples/hello/workflows/collector.workflow.yaml")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q, output:\n%s", code, errOut, out)
	}
	// The dummy starts once, after all three checks.
	if strings.Count(out, "[checks-done] started (core.dummy)") != 1 {
		t.Errorf("checks-done should start exactly once:\n%s", out)
	}
	last := strings.LastIndex(out, "[check-inbox] succeeded")
	if dummy := strings.Index(out, "[checks-done] started"); last < 0 || dummy < last {
		t.Errorf("checks-done must start after the slowest check:\n%s", out)
	}
}
