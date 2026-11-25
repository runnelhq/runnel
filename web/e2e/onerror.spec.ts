import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// The error workflow in the editor (ADR 0027, S72b): the field "On error" in project.yaml and
// in the settings of a workflow and a pipeline, and the links between a failed run and the run
// of its error workflow in the list of runs. Against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs); nothing is saved except the files of the test.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const fail = "workflows/e2e-onerror-fail.workflow.yaml";
const report = "workflows/e2e-onerror-report.workflow.yaml";
const pipeline = "pipelines/e2e-onerror.pipeline.yaml";
const trigger = "triggers/e2e-onerror.yaml";

const files: Record<string, string> = {
  [fail]: `kind: Workflow
apiVersion: runnel/v1
name: e2e-onerror-fail
settings:
  onError: workflows/e2e-missing.workflow.yaml
actions:
  - id: start
    type: core.start
  - id: wait
    type: core.wait
    with:
      duration: oops
links:
  - { from: start, to: wait }
`,
  [report]: `kind: Workflow
apiVersion: runnel/v1
name: e2e-onerror-report
parameters:
  - name: FAILED_RUN
actions:
  - id: start
    type: core.start
  - id: log
    type: core.log
    with:
      message: "failed: \${FAILED_RUN}"
links:
  - { from: start, to: log }
`,
  [pipeline]: `kind: Pipeline
apiVersion: runnel/v1
name: e2e-onerror
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 1 } }
  - { id: done, type: core.dummy }
links:
  - { from: gen, to: done }
`,
};

test.beforeEach(async ({ request }) => {
  for (const [path, data] of Object.entries(files)) {
    expect((await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data })).ok()).toBe(true);
  }
});

test.afterAll(async ({ request }) => {
  for (const path of [...Object.keys(files), trigger]) await request.delete(`/api/v1/files/${path}`, { headers: { "If-Match": "*" } });
});

const openFile = async (page: Page, name: string) => {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
};
const yaml = async (page: Page, back: "Canvas" | "Form") => {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: back, exact: true }).click();
  return text;
};

test("sets the error workflow of the project in project.yaml", async ({ page }) => {
  await openFile(page, "project.yaml");
  const form = page.getByTestId("settings-form");
  const field = form.locator('[data-field="settings.onError"]');
  await expect(field).toContainText("Workflow started when a run fails (not for runs in the editor)");
  const select = field.getByRole("combobox");
  await expect(select).toHaveValue("");
  await expect(select.locator("option").first()).toHaveText("None");
  // The workflows of the project, no pipelines.
  await expect(select.locator("option", { hasText: "workflows/on-error.workflow.yaml" })).toHaveCount(1);
  await expect(select.locator("option", { hasText: pipeline })).toHaveCount(0);

  await select.selectOption("workflows/on-error.workflow.yaml");
  await expect.poll(() => yaml(page, "Form")).toContain("settings:\n  onError: workflows/on-error.workflow.yaml\n");
  await field.getByRole("combobox").selectOption("");
  await expect.poll(() => yaml(page, "Form")).not.toContain("onError");
});

test("sets the error workflow of a workflow, with its problem at the field", async ({ page }) => {
  await openFile(page, "e2e-onerror-fail.workflow.yaml");
  const panel = page.getByRole("region", { name: "Properties" });
  const field = panel.locator('[data-field="settings.onError"]');
  const select = field.getByRole("combobox");
  // The file is missing: the value stays selectable and the problem shows at the field.
  await expect(select).toHaveValue("workflows/e2e-missing.workflow.yaml");
  await expect(field.getByRole("alert")).toContainText("error workflow workflows/e2e-missing.workflow.yaml not found");
  // The project sets none; the workflow itself is not offered.
  await expect(select.locator("option").nth(0)).toHaveText("Project default (none)");
  await expect(select.locator("option").nth(1)).toHaveText("None");
  await expect(select.locator("option", { hasText: fail })).toHaveCount(0);

  await select.selectOption("none");
  await expect.poll(() => yaml(page, "Canvas")).toContain("settings:\n  onError: none\nactions:\n");
  await expect(field.getByRole("alert")).toHaveCount(0);
  await field.getByRole("combobox").selectOption(report);
  await expect.poll(() => yaml(page, "Canvas")).toContain(`settings:\n  onError: ${report}\nactions:\n`);
  await field.getByRole("combobox").selectOption("");
  await expect.poll(() => yaml(page, "Canvas")).not.toContain("settings:");
});

test("sets the error workflow of a pipeline", async ({ page }) => {
  await openFile(page, "e2e-onerror.pipeline.yaml");
  const field = page.getByRole("region", { name: "Properties" }).getByTestId("pipeline-properties").locator('[data-field="settings.onError"]');
  await expect(field).toContainText("Workflow started when a run of this pipeline fails");
  await field.getByRole("combobox").selectOption("none");
  await expect.poll(() => yaml(page, "Canvas")).toContain("settings:\n  onError: none\ntransforms:\n");
});

// A failed run of a trigger starts the error workflow; the list links both runs. The trigger
// runs every second until the error run is there (the server picks it up within 5 s).
test("links a failed run and the run of its error workflow in the list of runs", async ({ page, request }) => {
  const put = (path: string, data: string) => request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data });
  await put(fail, files[fail].replace("workflows/e2e-missing.workflow.yaml", report));
  await put(trigger, `kind: Trigger\napiVersion: runnel/v1\nname: e2e-onerror\ntype: interval\ntarget: ${fail}\nwith:\n  every: 1s\n`);
  type Run = { parent?: string; status: string };
  const errorRuns = async () => {
    const res = await request.get(`/api/v1/runs?${new URLSearchParams({ path: report, trigger: "error" })}`);
    return ((await res.json()) as { runs?: Run[] }).runs ?? [];
  };
  await expect.poll(async () => (await errorRuns()).some((r) => r.status === "succeeded" && r.parent), { timeout: 20_000 }).toBe(true);
  await request.delete(`/api/v1/files/${trigger}`, { headers: { "If-Match": "*" } });

  await page.goto("/");
  await page.getByRole("button", { name: /^Runs/ }).click();
  const view = page.getByTestId("runs-view");
  // The trigger filter offers the error workflow; its runs show "Error workflow".
  await view.getByRole("combobox", { name: "Trigger" }).selectOption({ label: "Error workflow" });
  await view.getByRole("combobox", { name: "File" }).selectOption(report);
  const rows = view.getByRole("table", { name: "Runs" }).getByRole("row").filter({ hasText: "e2e-onerror-report.workflow.yaml" });
  await expect(rows.filter({ hasText: "Succeeded" }).first()).toContainText("Error workflow");

  // The error run names the failed run; a click selects it, and it names the error run.
  await rows.filter({ hasText: "Succeeded" }).first().click();
  const details = page.getByTestId("run-details");
  const relation = details.getByTestId("run-relation");
  await expect(relation).toContainText("for run e2e-onerror-fail.workflow.yaml · ");
  const id = (await details.locator("dd").first().textContent())!;
  const failed = (await relation.getByRole("button").getAttribute("title"))!;
  await relation.getByRole("button").click();
  await expect(details.locator("dd").first()).toHaveText(failed);
  await expect(details.getByRole("alert")).toContainText('invalid duration "oops"');
  await expect(relation).toContainText(`reported by e2e-onerror-report.workflow.yaml · ${id.slice(0, 10)}`);
  await relation.getByRole("button").click();
  await expect(details.locator("dd").first()).toHaveText(id);
});
