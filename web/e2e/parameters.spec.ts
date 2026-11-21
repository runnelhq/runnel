import { readFileSync } from "node:fs";
import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// Parameters, outputs and repetition in the properties panel (mockups
// design/mockups/e7c-parameters.html and s25a-pipeline.html, state "foreach"), against
// the server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

// Its own copy of parallel.workflow.yaml (parameters DELAY_A and DELAY_B, no outputs).
const path = "workflows/params.workflow.yaml";
const original = readFileSync(new URL("../../examples/hello/workflows/parallel.workflow.yaml", import.meta.url), "utf8");

// A workflow that calls hello.workflow.yaml (parameter GREETING).
const callPath = "workflows/call.workflow.yaml";
const call = `kind: Workflow
apiVersion: runnel/v1
name: call
actions:
  - id: start
    type: core.start
  - id: greet
    type: core.run-workflow
    with:
      workflow: workflows/hello.workflow.yaml
links:
  - { from: start, to: greet }
layout:
  start: { x: 40, y: 120 }
  greet: { x: 280, y: 120 }
`;

test.beforeEach(async ({ request }) => {
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: original });
  await request.put(`/api/v1/files/${callPath}`, { headers: { "If-Match": "*" }, data: call });
});

async function open(page: Page, name = "params.workflow.yaml", node = "node-fetch-b") {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  await expect(page.getByTestId(node)).toBeVisible();
}

/** The text of the YAML view, line by line. */
async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return text;
}

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const field = (page: Page, key: string) => panel(page).locator(`[data-field="${key}"]`);
const row = (page: Page, list: string, i: number) => panel(page).getByTestId(`${list}-${i}`);

/** Opens a row (the panel closes them when it is hidden, e.g. in the YAML view). */
async function openRow(page: Page, list: string, i: number) {
  const head = row(page, list, i).getByRole("button").first();
  if ((await head.getAttribute("aria-expanded")) !== "true") await head.click();
}

test("changes the fields of a parameter", async ({ page }) => {
  await open(page);
  await expect(row(page, "parameters", 0)).toContainText("DELAY_A");
  await expect(row(page, "parameters", 0)).toContainText("string · optional");
  await openRow(page, "parameters", 0);

  const def = field(page, "parameters[0].default").getByRole("textbox");
  await def.fill("5s");
  await def.blur();
  await field(page, "parameters[0].description").getByRole("textbox").fill("Wait of branch A");
  await field(page, "parameters[0].description").getByRole("textbox").blur();
  await panel(page).getByRole("checkbox", { name: /Required/ }).click();
  await expect(row(page, "parameters", 0)).toContainText("string · required");
  // Allowed values as chips: Enter adds, × removes.
  const values = field(page, "parameters[0].enum").getByRole("textbox");
  await values.fill("5s");
  await values.press("Enter");
  await values.fill("10s");
  await values.press("Enter");
  await expect(field(page, "parameters[0].enum")).toContainText("5s");
  await expect.poll(() => yaml(page)).toContain(
    "  - name: DELAY_A\n    default: 5s\n    description: Wait of branch A\n    required: true\n    enum: [5s, 10s]\n",
  );
  await openRow(page, "parameters", 0);
  await row(page, "parameters", 0).getByRole("button", { name: "Remove 10s" }).click();
  await expect.poll(() => yaml(page)).toContain("    enum: [5s]\n");

  // A type that does not fit the default is reported at the field and in Problems.
  await openRow(page, "parameters", 0);
  await field(page, "parameters[0].type").getByRole("combobox").selectOption("integer");
  await expect(field(page, "parameters[0].default").getByRole("alert")).toContainText("is not an integer");
  await expect(row(page, "parameters", 0)).toContainText("✖");
  await page.getByTestId("error-count").first().click();
  const problems = page.getByRole("region", { name: "Problems and log" }).getByRole("table").getByRole("row");
  await expect(problems.filter({ hasText: "DELAY_A · Default" }).first()).toBeVisible();
  await field(page, "parameters[0].type").getByRole("combobox").selectOption("string");
  await expect(field(page, "parameters[0].default").getByRole("alert")).toHaveCount(0);
});

test("adds, renames, moves and deletes parameters", async ({ page }) => {
  await open(page);
  await panel(page).getByRole("button", { name: "+ Add parameter" }).click();
  // The new entry is open, its name selected for typing.
  const name = field(page, "parameters[2].name").getByRole("textbox");
  await expect(name).toBeFocused();
  await expect(name).toHaveValue("PARAM_1");
  await page.keyboard.type("DELAY_A");
  await expect(field(page, "parameters[2].name")).toContainText("“DELAY_A” is already used");
  await name.fill("COUNT");
  await name.press("Enter");
  await expect(row(page, "parameters", 2)).toContainText("COUNT");
  expect(await yaml(page)).toContain("  - name: DELAY_B\n    default: 1s\n  - name: COUNT\n");

  await openRow(page, "parameters", 2);
  await row(page, "parameters", 2).getByRole("button", { name: "Move up" }).click();
  await expect(row(page, "parameters", 1)).toContainText("COUNT");
  expect(await yaml(page)).toContain("  - name: COUNT\n  - name: DELAY_B\n");

  await openRow(page, "parameters", 1);
  await row(page, "parameters", 1).getByRole("button", { name: "Delete parameter" }).click();
  await expect(page.getByRole("status")).toContainText("Deleted parameter COUNT");
  await expect(panel(page).getByRole("list", { name: "Parameters" }).getByRole("listitem")).toHaveCount(2);
  await page.getByRole("status").getByRole("button", { name: "Undo" }).click();
  await expect(panel(page).getByRole("list", { name: "Parameters" }).getByRole("listitem")).toHaveCount(3);
});

test("adds an output with an expression", async ({ page }) => {
  await open(page);
  await panel(page).getByRole("button", { name: /^Outputs/ }).click();
  await panel(page).getByRole("button", { name: "+ Add output" }).click();
  await expect(field(page, "outputs[0].name").getByRole("textbox")).toHaveValue("OUTPUT_1");
  // An output needs a value: reported until it is set.
  await expect(field(page, "outputs[0].value").getByRole("alert")).toContainText('missing required field "value"');
  const value = field(page, "outputs[0].value").getByRole("textbox");
  await value.fill("{{ results.combine.status }}");
  await value.blur();
  await expect(field(page, "outputs[0].value").getByRole("alert")).toHaveCount(0);
  expect(await yaml(page)).toContain('outputs:\n  - name: OUTPUT_1\n    value: "{{ results.combine.status }}"\nactions:');
});

test("repeats an action for each entry and gives parameters to a sub-workflow", async ({ page }) => {
  await open(page, "call.workflow.yaml", "node-greet");
  await page.getByTestId("node-greet").click();
  // Closed while the action runs once.
  const section = panel(page).getByRole("button", { name: /^For each entry/ });
  await expect(section).toHaveAttribute("aria-expanded", "false");
  await expect(section).toContainText("off");
  await section.click();
  const list = field(page, "forEach").getByRole("textbox");
  await list.fill("[1, 2, 3]");
  await list.blur();
  await expect(page.getByTestId("node-greet").getByTestId("for-each")).toHaveText("↻ for each");
  await field(page, "parallel").getByRole("button", { name: "More" }).click();
  await expect(page.getByTestId("node-greet").getByTestId("for-each")).toHaveText("↻ for each · 2 at a time");
  await panel(page).getByRole("checkbox", { name: /Stop at the first error/ }).click();
  await expect(panel(page)).toContainText("2 at a time");

  // The parameters of the called workflow: the help names those it declares.
  const params = field(page, "with.params");
  await expect(params).toContainText("Declared by hello.workflow.yaml: GREETING");
  await params.getByRole("button", { name: "+ Add parameter" }).click();
  await params.getByRole("textbox", { name: "Name 1" }).fill("GREETING");
  await params.getByTestId("map-value-0").getByRole("textbox").fill("Moin");
  await params.getByTestId("map-value-0").getByRole("textbox").blur();
  await expect.poll(() => yaml(page)).toContain(
    '      workflow: workflows/hello.workflow.yaml\n      params:\n        GREETING: Moin\n    forEach: "{{ [1, 2, 3] }}"\n    parallel: 2\n    stopOnError: false\n',
  );
  await page.getByTestId("node-greet").click();
  await params.getByRole("button", { name: "Remove GREETING" }).click();
  await expect.poll(() => yaml(page)).not.toContain("params:");
});

test("pipelines have parameters, but no outputs", async ({ page, request }) => {
  await request.put("/api/v1/files/pipelines/params.pipeline.yaml", {
    headers: { "If-Match": "*" },
    data: "kind: Pipeline\napiVersion: runnel/v1\nname: p\ntransforms:\n  - id: gen\n    type: core.generate-rows\n    with:\n      rows: 3\n",
  });
  await open(page, "params.pipeline.yaml", "node-gen");
  await expect(panel(page).getByRole("button", { name: /^Outputs/ })).toHaveCount(0);
  await panel(page).getByRole("button", { name: "+ Add parameter" }).click();
  await expect(field(page, "parameters[0].name").getByRole("textbox")).toHaveValue("PARAM_1");
  expect(await yaml(page)).toContain("name: p\nparameters:\n  - name: PARAM_1\ntransforms:");
});

// Values typed before the server has answered the previous change are kept (a slow
// server used to drop the first of two quickly added values).
test("keeps quickly added allowed values with a slow server", async ({ page }) => {
  await page.route("**/api/v1/workflows/edit", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 500));
    await route.continue();
  });
  await open(page);
  await openRow(page, "parameters", 0);
  const values = field(page, "parameters[0].enum").getByRole("textbox");
  for (const v of ["5s", "10s", "20s"]) {
    await values.fill(v);
    await values.press("Enter");
  }
  for (const v of ["5s", "10s", "20s"]) {
    await expect(field(page, "parameters[0].enum").getByRole("button", { name: `Remove ${v}` })).toBeVisible();
  }
  await expect.poll(() => yaml(page)).toContain("    enum: [5s, 10s, 20s]\n");
  // Removing works on the current list, too.
  await openRow(page, "parameters", 0);
  await field(page, "parameters[0].enum").getByRole("button", { name: "Remove 10s" }).click();
  await expect.poll(() => yaml(page)).toContain("    enum: [5s, 20s]\n");
});
