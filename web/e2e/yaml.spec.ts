import { readFileSync } from "node:fs";
import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// Editing YAML (mockup design/mockups/e9-yaml.html): typing in the YAML view of a
// workflow, checked after the typing pause with the problems at the line, one undo history
// with the canvas, and the canvas while the text is no workflow. Against the server on a
// temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/typed.workflow.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = readFileSync(new URL("../../examples/hello/workflows/hello.workflow.yaml", import.meta.url), "utf8");

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function openYAML(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("typed.workflow.yaml", { exact: true }).click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(editor(page)).toContainText("name: hello");
}

const editor = (page: Page) => page.getByTestId("yaml-editor");
const line = (page: Page, text: string) => editor(page).locator(".cm-line", { hasText: text });

/** Puts the cursor at the end of the line with text and replaces its last n characters. */
async function retype(page: Page, text: string, n: number, typed: string) {
  await line(page, text).click();
  await page.keyboard.press("End");
  for (let i = 0; i < n; i++) await page.keyboard.press("Backspace");
  await page.keyboard.type(typed);
}


test("checks while typing and shows the problem at the line", async ({ page, request }) => {
  await openYAML(page);
  await expect(page.getByText("Read-only")).toHaveCount(0);
  await retype(page, "type: core.wait", 4, "wiat");
  await expect(page.getByTestId("checking")).toBeVisible();
  await expect(page.getByTestId("file-summary")).toHaveText(/^YAML · UTF-8 · \d+ lines · Ln 23, Col 20$/);
  await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
  await expect(page.getByTestId("checking")).toHaveCount(0);
  // At the line: wavy underline, a mark in the gutter and the message at the cursor.
  await expect(editor(page).locator(".rx-error")).toContainText("core.wiat");
  await expect(editor(page).locator(".ry-problems .ry-mark-error")).toHaveCount(1);
  await expect(page.locator(".ry-tooltip")).toContainText('unknown action type "core.wiat"');
  // Below the editor, also in the YAML view.
  await page.getByTestId("error-count").click();
  await expect(page.getByRole("table", { name: "Problems" })).toContainText('unknown action type "core.wiat"');

  await retype(page, "type: core.wiat", 4, "wait");
  await retype(page, "duration: 1s", 2, "2s");
  await expect(page.getByTestId("error-count")).toHaveCount(0);
  await expect(page.getByRole("tab", { name: /typed\.workflow\.yaml/ })).toContainText("●");
  await page.keyboard.press("Control+s");
  await expect(page.getByRole("tab", { name: /typed\.workflow\.yaml/ })).not.toContainText("●");
  expect(await (await request.get(fileURL)).text()).toBe(original.replace("duration: 1s", "duration: 2s"));
});

test("shares the undo history with the canvas", async ({ page }) => {
  await openYAML(page);
  await retype(page, "name: hello", 0, "-typed");
  await expect(page.getByTestId("checking")).toHaveCount(0);
  // The typing pause is one step: undone on the canvas …
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await expect(page.getByTestId("node-greet")).toBeVisible();
  await page.getByRole("button", { name: "Undo" }).click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  expect(await editorText(page)).toBe(original);
  // … and done again in the YAML view with Ctrl+Y.
  await editor(page).locator(".cm-content").click();
  await page.keyboard.press("Control+y");
  await expect(line(page, "name: hello-typed")).toBeVisible();
  // Ctrl+Z right after typing takes back what was typed since the last pause.
  await retype(page, "name: hello-typed", 0, "x");
  await page.keyboard.press("Control+z");
  await expect(line(page, "name: hello-typed")).toHaveText("name: hello-typed");
});

test("keeps a syntax error and shows where it is on the canvas", async ({ page }) => {
  await openYAML(page);
  // "type: core.wait" without its colon: no longer a workflow.
  await line(page, "type: core.wait").click();
  await page.keyboard.press("Home");
  await page.keyboard.press("End");
  for (let i = 0; i < "core.wait".length; i++) await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("Backspace");
  await page.keyboard.press("Backspace");
  await page.keyboard.type(" ");
  await expect(page.getByTestId("not-a-graph")).toBeVisible();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  const alert = page.getByRole("alert");
  await expect(alert).toContainText("This workflow can’t be shown as a graph");
  await alert.getByRole("button", { name: /^Show YAML at line \d+$/ }).click();
  await expect(line(page, "type core.wait")).toBeVisible();
});

test("finds text with Ctrl+F", async ({ page }) => {
  await openYAML(page);
  await editor(page).locator(".cm-content").click();
  await page.keyboard.press("Control+f");
  const find = editor(page).getByRole("textbox", { name: "Find" });
  await expect(find).toBeFocused();
  await find.fill("GREETING");
  await find.press("Enter"); // the panel searches on Enter and while typing
  await expect(editor(page).locator(".cm-searchMatch").first()).toContainText("GREETING");
});
