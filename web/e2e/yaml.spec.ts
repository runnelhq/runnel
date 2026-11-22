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


// Fix: in dark mode the message on hovering a problem was light text on the light
// background of CodeMirror's hover tooltip.
test("shows the message on hover in the colours of the theme", async ({ page }) => {
  await openYAML(page);
  await page.getByRole("button", { name: "Dark", exact: true }).click();
  await retype(page, "type: core.wait", 4, "wiat");
  await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
  await line(page, "name: hello").click();
  await expect(page.locator(".ry-tooltip")).toHaveCount(0);
  await editor(page).locator(".rx-error").hover();
  const tip = page.locator(".cm-tooltip-hover .ry-tooltip");
  await expect(tip).toContainText('unknown action type "core.wiat"');
  const colours = await tip.evaluate((el) => {
    const own = getComputedStyle(el);
    const box = getComputedStyle(el.closest(".cm-tooltip-hover")!);
    return { bg: own.backgroundColor, fg: own.color, box: box.backgroundColor };
  });
  expect(colours.box).toBe("rgba(0, 0, 0, 0)");
  expect(colours.bg).not.toBe(colours.fg);
  // Dark background, light text.
  const lum = (c: string) => c.match(/\d+/g)!.slice(0, 3).reduce((a, n) => a + Number(n), 0);
  expect(lum(colours.bg)).toBeLessThan(lum(colours.fg));
});

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

// E10: project.yaml, environments and triggers are edited the same way, without canvas and log.
test.describe("other files of the project", () => {
  const envURL = "/api/v1/files/environments/prod.yaml";
  const triggerURL = "/api/v1/files/triggers/hourly.yaml";
  const env = readFileSync(new URL("../../examples/hello/environments/prod.yaml", import.meta.url), "utf8");
  const trigger = readFileSync(new URL("../../examples/hello/triggers/hourly.yaml", import.meta.url), "utf8");

  test.afterEach(async ({ request }) => {
    await request.put(envURL, { headers: { "If-Match": "*" }, data: env });
    await request.put(triggerURL, { headers: { "If-Match": "*" }, data: trigger });
  });

  test("edits an environment and checks it while typing", async ({ page, request }) => {
    await page.goto("/");
    await page.getByRole("tree").getByText("prod.yaml", { exact: true }).click();
    await expect(editor(page)).toContainText("name: prod");
    await expect(page.getByText("Read-only")).toHaveCount(0);
    await expect(page.getByRole("group", { name: "View" })).toHaveCount(0);

    await retype(page, "name: prod", 4, "live");
    await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
    await expect(page.locator(".ry-tooltip")).toContainText('environment name "live" must match the file name');
    await page.getByTestId("error-count").click();
    const bar = page.getByRole("region", { name: "Problems and log" });
    await expect(bar.getByRole("table", { name: "Problems" })).toContainText("must match the file name");
    await expect(bar.getByRole("button", { name: "Log" })).toHaveCount(0);

    await page.keyboard.press("Control+z");
    await expect(page.getByTestId("error-count")).toHaveCount(0);
    await retype(page, "STAGE: production", 10, "live");
    await expect(page.getByTestId("checking")).toHaveCount(0);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("tab", { name: /prod\.yaml/ })).not.toContainText("●");
    expect(await (await request.get(envURL)).text()).toBe(env.replace("STAGE: production", "STAGE: live"));
  });

  test("checks the target of a trigger", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("tree").getByText("hourly.yaml", { exact: true }).click();
    await expect(page.getByTestId("kind")).toHaveText("Trigger");
    await retype(page, "target: workflows/hello.workflow.yaml", "hello.workflow.yaml".length, "nope.workflow.yaml");
    await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
    await expect(page.locator(".ry-tooltip")).toContainText("nope.workflow.yaml");
  });

  test("other files stay read-only", async ({ page, request }) => {
    await request.put("/api/v1/files/notes.txt", { headers: { "If-Match": "*" }, data: "notes\n" });
    await page.goto("/");
    await page.getByRole("tree").getByText("notes.txt", { exact: true }).click();
    await expect(page.getByText("Read-only")).toBeVisible();
    await expect(page.getByTestId("code")).toContainText("notes");
    await request.delete("/api/v1/files/notes.txt");
  });
});

test("keeps the selected element when switching between canvas and YAML", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("typed.workflow.yaml", { exact: true }).click();
  const selected = (id: string) => page.locator(".react-flow__node.selected").getByTestId(`node-${id}`);

  // The YAML view opens at the selected action.
  await page.getByTestId("node-pause").click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const at = original.split("\n").findIndex((l) => l.includes("id: pause")) + 1;
  await expect(page.getByTestId("file-summary")).toContainText(`Ln ${at},`);

  // Back without moving the cursor: the action is still selected.
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await expect(selected("pause")).toBeVisible();

  // The canvas selects what the cursor of the YAML view is in.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await line(page, "type: core.success").click();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await expect(selected("done")).toBeVisible();
  await expect(selected("pause")).toHaveCount(0);

  // A link: its line in the YAML view and back.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await line(page, "{ from: start, to: greet }").click();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await expect(page.locator(".react-flow__edge.selected")).toHaveCount(1);
});

// Fix: selected text was hard to see – a pale colour, hidden on the active line, which
// CodeMirror draws above the selection.
test("shows selected text clearly, also on the line of the cursor", async ({ page }) => {
  await openYAML(page);
  for (const theme of ["Light", "Dark"]) {
    await page.getByRole("button", { name: theme, exact: true }).click();
    await line(page, "name: hello").click();
    await page.keyboard.press("Home");
    await page.keyboard.press("Shift+ArrowDown");
    await page.keyboard.press("Shift+End");
    const [active, selection] = await page.evaluate(() => [
      getComputedStyle(document.querySelector(".runnel-yaml .cm-activeLine")!).backgroundColor,
      getComputedStyle(document.querySelector(".runnel-yaml .cm-selectionBackground")!).backgroundColor,
    ]);
    const alpha = (c: string) => Number(/\/\s*([\d.]+)\)$/.exec(c)?.[1] ?? /rgba\(.*,\s*([\d.]+)\)$/.exec(c)?.[1] ?? 1);
    // The active line lets the selection through; the selection is a clear tint.
    expect(alpha(active), theme).toBeLessThan(1);
    expect(alpha(selection), theme).toBeGreaterThanOrEqual(0.3);
  }
});
