import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// The code fields of core.script (roadmap S73c, ADR 0028): JavaScript highlighting and
// suggestions for record, params, console and require; problems of the code at their
// line. Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs),
// which has scripts/util.js.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const workflowPath = "workflows/script-editor.workflow.yaml";
const workflow = `kind: Workflow
apiVersion: runnel/v1
name: script-editor
actions:
  - id: start
    type: core.start
  - id: total
    type: core.script
    with:
      params:
        rate: "0.19"
        orders: "3"
      script: |
        // Gross amount
        const net = 10;
        return { gross: net * (1 + params.rate) };
  - id: shell
    type: shell.powershell
    with:
      script: Write-Output "if"
links:
  - { from: start, to: total }
  - { from: total, to: shell }
layout:
  start: { x: 40, y: 120 }
  total: { x: 280, y: 120 }
  shell: { x: 520, y: 120 }
`;

const pipelinePath = "pipelines/script-editor.pipeline.yaml";
const pipeline = `kind: Pipeline
apiVersion: runnel/v1
name: script-editor
transforms:
  - id: rows
    type: core.generate-rows
    with:
      rows: 2
      fields:
        - { name: amount, value: 9.95 }
        - { name: unit price, value: 2 }
  - id: calc
    type: core.script
    with:
      script: return record;
  - id: show
    type: core.log
links:
  - { from: rows, to: calc }
  - { from: calc, to: show }
layout:
  rows: { x: 40, y: 120 }
  calc: { x: 280, y: 120 }
  show: { x: 520, y: 120 }
`;

test.beforeEach(async ({ request }) => {
  await request.put(`/api/v1/files/${workflowPath}`, { headers: { "If-Match": "*" }, data: workflow });
  await request.put(`/api/v1/files/${pipelinePath}`, { headers: { "If-Match": "*" }, data: pipeline });
});

async function open(page: Page, name: string, node: string) {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  await page.getByTestId(`node-${node}`).click();
}

async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return text;
}

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const field = (page: Page, key: string) => panel(page).locator(`[data-field="${key}"]`);
const code = (page: Page, key: string) => field(page, key).getByRole("textbox");
const suggestions = (page: Page) => page.getByRole("listbox");

/** Puts the cursor at the end of the code: the empty line after the last line of the block. */
async function toEnd(page: Page, key: string) {
  await code(page, key).click();
  await page.keyboard.press("Control+End");
}

test("highlights JavaScript and writes typed code to the file", async ({ page }) => {
  await open(page, "script-editor.workflow.yaml", "total");
  const script = field(page, "with.script");
  await expect(code(page, "with.script")).toContainText("const net = 10;");
  // Keywords, strings and comments of JavaScript; line numbers.
  await expect(script.locator(".tok-keyword").filter({ hasText: /^const$/ })).toBeVisible();
  await expect(script.locator(".tok-comment")).toHaveText("// Gross amount");
  await expect(script.locator(".cm-lineNumbers")).toContainText("3");

  // shell.powershell keeps its field without JavaScript.
  await page.getByTestId("node-shell").click();
  await expect(field(page, "with.script").locator(".tok-keyword")).toHaveCount(0);
  await expect(field(page, "with.script").locator(".cm-lineNumbers")).toHaveCount(0);

  await page.getByTestId("node-total").click();
  await toEnd(page, "with.script");
  // Tab indents inside the code (Esc and Tab leave the field).
  await page.keyboard.press("Tab");
  await page.keyboard.type("// done");
  await expect(code(page, "with.script")).toContainText("  // done");
  // Esc and then Tab leave the field (keyboard users are not trapped).
  await page.keyboard.press("Escape");
  await page.keyboard.press("Tab");
  await expect(code(page, "with.script")).not.toBeFocused();
  await expect.poll(() => yaml(page)).toContain("        return { gross: net * (1 + params.rate) };\n          // done\n");
});

test("suggests params, console and the files of scripts/", async ({ page }) => {
  await open(page, "script-editor.workflow.yaml", "total");
  await toEnd(page, "with.script");

  // The roots of the code; record only in the transform.
  await page.keyboard.type("par");
  await expect(suggestions(page).getByRole("option", { name: /\bparams\b/ })).toBeVisible();
  await expect(suggestions(page).getByRole("option", { name: /record/ })).toHaveCount(0);

  // After "params." the keys configured in params.
  await page.keyboard.type("ams.");
  await expect(suggestions(page).getByRole("option", { name: /rate/ })).toBeVisible();
  await expect(suggestions(page).getByRole("option", { name: /orders/ })).toBeVisible();
  await page.keyboard.press("Escape");
  for (let i = 0; i < "params.".length; i++) await page.keyboard.press("Backspace");

  // After "console." its methods.
  await page.keyboard.type("console.");
  for (const m of ["log", "info", "warn", "error"]) await expect(suggestions(page).getByRole("option", { name: new RegExp(`${m}\\b`) })).toBeVisible();
  await page.keyboard.type("lo");
  await suggestions(page).getByRole("option", { name: /log/ }).click();
  await expect(code(page, "with.script")).toContainText("console.log");

  // In require('…') the modules under scripts/ of the project.
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await page.keyboard.type("const u = require('");
  await suggestions(page).getByRole("option", { name: /\.\/scripts\/util\.js/ }).click();
  await expect(code(page, "with.script")).toContainText("require('./scripts/util.js')");
});

test("shows a syntax error at the field and its line", async ({ page }) => {
  await open(page, "script-editor.workflow.yaml", "total");
  await toEnd(page, "with.script");
  await page.keyboard.type("let x = ;");
  // Applied after the pause; the check reports the error at the field and marks line 4.
  const script = field(page, "with.script");
  await expect(script.getByRole("alert")).toContainText("script, line 4: syntax error");
  await expect(script.locator(".rs-error-line")).toHaveCount(1);
  await expect(script.locator(".rs-error-line")).toContainText("let x = ;");
});

test("suggests the fields of the incoming records in the transform", async ({ page }) => {
  await open(page, "script-editor.pipeline.yaml", "calc");
  // init and end are code fields too.
  await expect(field(page, "with.init").locator(".cm-lineNumbers")).toHaveCount(1);
  await expect(field(page, "with.end").locator(".cm-lineNumbers")).toHaveCount(1);
  await expect(field(page, "with.script").locator(".tok-keyword").filter({ hasText: "return" })).toBeVisible();

  await code(page, "with.script").click();
  await page.keyboard.press("Control+Home");
  await page.keyboard.type("rec");
  await expect(suggestions(page).getByRole("option", { name: /record/ })).toBeVisible();
  await page.keyboard.type("ord.");
  await expect(suggestions(page).getByRole("option", { name: /amount/ })).toBeVisible();
  // A name that is no identifier is inserted with brackets.
  await suggestions(page).getByRole("option", { name: /unit price/ }).click();
  await page.keyboard.type(";");
  await expect(code(page, "with.script")).toContainText('record["unit price"];return record;');

  // init has no record.
  await code(page, "with.init").click();
  await page.keyboard.press("Control+Space");
  await expect(suggestions(page).getByRole("option", { name: /params/ })).toBeVisible();
  await expect(suggestions(page).getByRole("option", { name: /\brecord\b/ })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");

  await expect.poll(() => yaml(page)).toContain('script: record["unit price"];return record;');

  // A syntax error in end is shown at end.
  await page.getByTestId("node-calc").click();
  await code(page, "with.end").click();
  await page.keyboard.type("const = 1;");
  await expect(field(page, "with.end").getByRole("alert")).toContainText("end, line");
  await expect(field(page, "with.script").getByRole("alert")).toHaveCount(0);
});
