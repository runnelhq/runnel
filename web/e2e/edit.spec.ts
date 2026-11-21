import { readFileSync } from "node:fs";
import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// Editing on the canvas (mockup design/mockups/e6a-edit.html). These tests run against a
// server on a temporary copy of examples/hello (e2e/serve-copy.mjs) and save files there.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/parallel.workflow.yaml";
const fileURL = `/api/v1/files/${path}`;
// The original from the repository (the server works on a copy of it).
const original = readFileSync(new URL("../../examples/hello/workflows/parallel.workflow.yaml", import.meta.url), "utf8");

// Every test starts from the original file.
test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("parallel.workflow.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-fetch-b")).toBeVisible();
}

/** Drags a node by dx/dy screen pixels (zoom is 100 %). */
async function drag(page: Page, id: string, dx: number, dy: number) {
  const box = (await page.getByTestId(`node-${id}`).boundingBox())!;
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx / 2, y + dy / 2, { steps: 5 });
  await page.mouse.move(x + dx, y + dy, { steps: 5 });
  await page.mouse.up();
}

/** Switches to the YAML view and returns the whole text once the editor shows it. */
async function showYAML(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await page.getByTestId("yaml-editor").locator(".cm-content").waitFor();
  return editorText(page);
}

test("moves a node, marks the file as changed and saves it", async ({ page, request }) => {
  await open(page);
  await expect(page.getByRole("button", { name: /^Save/ })).toBeDisabled();
  await expect(page.getByText("Read-only")).toHaveCount(0);

  await drag(page, "fetch-b", 83, 21);
  await expect(page.getByRole("tab", { name: /parallel\.workflow\.yaml/ })).toHaveAttribute("data-dirty", "true");
  await expect(page.getByTestId("unsaved")).toHaveText("● Unsaved changes");
  const code = await showYAML(page);
  expect(code).toMatch(/fetch-b: \{ x: \d+, y: \d+ \}/);
  const [, x, y] = code.match(/fetch-b: \{ x: (\d+), y: (\d+) \}/)!.map(Number);
  // Moved right and down, snapped to the 8 px grid.
  expect(x).toBeGreaterThan(280);
  expect(y).toBeGreaterThan(300);
  expect(x % 8).toBe(0);
  expect(y % 8).toBe(0);

  await page.keyboard.press("Control+s");
  await expect(page.getByTestId("unsaved")).toHaveCount(0);
  await expect(page.getByRole("tab", { name: /parallel\.workflow\.yaml/ })).not.toHaveAttribute("data-dirty");

  // Only the layout line changed; comments and the rest stay as they were.
  const saved = await (await request.get(fileURL)).text();
  expect(saved).toBe(original.replace(/  fetch-b: +\{ x: 280, y: 300 \}/, `  fetch-b: { x: ${x}, y: ${y} }`));
});

test("deletes a node with its links and undoes it", async ({ page }) => {
  await open(page);
  await page.getByTestId("node-combine").click();
  await page.keyboard.press("Delete");

  await expect(page.getByTestId("node-combine")).toHaveCount(0);
  await expect(page.getByRole("status")).toContainText("Deleted combine and 2 links");
  await expect(page.getByTestId("file-summary")).toHaveText("Workflow · 4 actions · 4 links");

  await page.getByRole("status").getByRole("button", { name: "Undo" }).click();
  await expect(page.getByTestId("node-combine")).toBeVisible();
  await expect(page.getByTestId("unsaved")).toHaveCount(0); // back to the saved text

  await page.getByRole("button", { name: "Redo" }).click();
  await expect(page.getByTestId("node-combine")).toHaveCount(0);
  await page.keyboard.press("Control+z");
  await expect(page.getByTestId("node-combine")).toBeVisible();
  await page.keyboard.press("Control+y");
  await expect(page.getByTestId("node-combine")).toHaveCount(0);
});

test("deletes several selected nodes", async ({ page }) => {
  await open(page);
  await page.getByTestId("node-fetch-a").click();
  await page.getByTestId("node-fetch-b").click({ modifiers: ["Control"] });
  await page.keyboard.press("Delete");
  await expect(page.getByRole("status")).toContainText("Deleted 2 actions and 6 links");
  await expect(page.getByTestId("file-summary")).toHaveText("Workflow · 3 actions · 0 links");
});

test("asks when the file was changed in another browser", async ({ page, browser }) => {
  await open(page);
  await drag(page, "combine", 40, 0);
  await expect(page.getByTestId("unsaved")).toBeVisible();

  // A second browser saves a change first.
  const context = await browser.newContext({ baseURL: "http://127.0.0.1:7398", viewport: page.viewportSize() });
  const other = await context.newPage();
  await open(other);
  await drag(other, "fetch-a", 0, 40);
  // Right away: saving waits for the move to be applied first.
  const saved = other.waitForResponse((r) => r.request().method() === "PUT" && r.status() === 200);
  await other.keyboard.press("Control+s");
  await saved;
  await expect(other.getByTestId("unsaved")).toHaveCount(0);
  await context.close();

  await page.getByRole("button", { name: /^Save/ }).click();
  const dialog = page.getByRole("alertdialog", { name: "File changed outside the editor" });
  await expect(dialog).toBeVisible();

  // Cancel keeps the changes; Reload shows the file of the other browser.
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByTestId("unsaved")).toBeVisible();
  await page.getByRole("button", { name: /^Save/ }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Reload" }).click();
  await expect(page.getByTestId("unsaved")).toHaveCount(0);
  expect(await showYAML(page)).toMatch(/fetch-a: \{ x: 280, y: 1[3-4]\d \}/);
});

test("overwrites a file changed elsewhere when asked to", async ({ page, request }) => {
  await open(page);
  await drag(page, "combine", 40, 0);
  await expect(page.getByTestId("unsaved")).toBeVisible();
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original + "# changed elsewhere\n" });

  await page.keyboard.press("Control+s");
  await page.getByRole("alertdialog").getByRole("button", { name: "Overwrite" }).click();
  await expect(page.getByTestId("unsaved")).toHaveCount(0);
  const saved = await (await request.get(fileURL)).text();
  expect(saved).toMatch(/combine: \{ x: 5[5-6]\d, y: 200 \}/);
  expect(saved).not.toContain("# changed elsewhere");
});

test("asks before closing a file with unsaved changes", async ({ page, request }) => {
  await open(page);
  await drag(page, "combine", 40, 0);
  await expect(page.getByTestId("unsaved")).toBeVisible();

  await page.getByRole("button", { name: "Close parallel.workflow.yaml" }).click();
  const dialog = page.getByRole("alertdialog", { name: "Save changes?" });
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByRole("tab")).toHaveCount(1);

  // Unsaved changes survive switching tabs.
  await page.getByRole("tree").getByText("hello.workflow.yaml").click();
  await page.getByRole("tab", { name: /parallel\.workflow\.yaml/ }).click();
  await expect(page.getByTestId("unsaved")).toBeVisible();

  await page.getByRole("button", { name: "Close parallel.workflow.yaml" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Don’t save" }).click();
  await expect(page.getByRole("tab", { name: /parallel\.workflow\.yaml/ })).toHaveCount(0);
  expect(await (await request.get(fileURL)).text()).toBe(original);
});

test("creates the layout block on the first move", async ({ page, request }) => {
  const withoutLayout = original.slice(0, original.indexOf("layout:"));
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: withoutLayout });
  await open(page);
  await expect(page.getByTestId("auto-layout")).toBeVisible();

  await drag(page, "start", 0, 40);
  const code = await showYAML(page);
  expect(code).toContain("layout:");
  for (const id of ["start", "fetch-a", "fetch-b", "combine", "report-error"]) {
    expect(code).toContain(`${id}: { x:`);
  }
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await expect(page.getByTestId("auto-layout")).toHaveCount(0);
});
