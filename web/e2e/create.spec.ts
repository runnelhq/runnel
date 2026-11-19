import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";

// Creating on the canvas (mockup design/mockups/e6b-create.html): palette, new actions,
// renaming, links and the link menu. Runs against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs), like edit.spec.ts.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/parallel.workflow.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = readFileSync(new URL("../../examples/hello/workflows/parallel.workflow.yaml", import.meta.url), "utf8");

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("parallel.workflow.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-fetch-b")).toBeVisible();
}

async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  // The text of each line without its number.
  const lines = await page.getByTestId("code").locator(":scope > div > span:nth-child(2)").allTextContents();
  const text = lines.join("\n") + "\n";
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return text;
}

/** Drops an action from the palette right of report-error. */
async function drop(page: Page, type: string) {
  const target = (await page.getByTestId("node-report-error").boundingBox())!;
  await page.getByTestId(`palette-${type}`).dragTo(page.getByTestId("canvas"), {
    targetPosition: await canvasPoint(page, target.x + target.width + 160, target.y + target.height / 2),
  });
}

async function canvasPoint(page: Page, x: number, y: number) {
  const c = (await page.getByTestId("canvas").boundingBox())!;
  return { x: x - c.x, y: y - c.y };
}

/** Draws a link from the output of one node to the input of another. */
async function connect(page: Page, from: string, to: string) {
  const out = (await page.getByTestId(`node-${from}`).locator(".react-flow__handle.source").boundingBox())!;
  const inp = (await page.getByTestId(`node-${to}`).locator(".react-flow__handle.target").boundingBox())!;
  await page.mouse.move(out.x + out.width / 2, out.y + out.height / 2);
  await page.mouse.down();
  await page.mouse.move((out.x + inp.x) / 2, (out.y + inp.y) / 2, { steps: 5 });
  await page.mouse.move(inp.x + inp.width / 2, inp.y + inp.height / 2, { steps: 5 });
  await page.mouse.up();
}

test("the palette lists the actions of the catalog by category", async ({ page }) => {
  await page.goto("/");
  const palette = page.getByRole("region", { name: "Palette" });
  await expect(palette.getByRole("group", { name: "General" })).toBeVisible();
  await expect(palette.getByTestId("palette-core.log")).toContainText("Log");
  await expect(palette.getByText("Open a workflow on the canvas to add actions.")).toBeVisible();
  await expect(palette.getByTestId("palette-core.log")).toHaveAttribute("draggable", "false");

  await open(page);
  await expect(palette.getByText("Drag an action onto the canvas.")).toBeVisible();
  await expect(palette.getByTestId("palette-core.log")).toHaveAttribute("draggable", "true");
  // The workflow has a start action already.
  await expect(palette.getByTestId("palette-core.start")).toHaveAttribute("aria-disabled", "true");
  await expect(palette.getByTestId("palette-core.start")).toHaveAttribute("title", "The workflow already has a start action");

  // "/" jumps into the search field; the search matches type, name and description.
  await page.getByTestId("canvas").click({ position: { x: 20, y: 20 } });
  await page.keyboard.press("/");
  await expect(palette.getByRole("textbox", { name: "Search actions" })).toBeFocused();
  await page.keyboard.type("wai");
  await expect(palette.getByRole("listitem")).toHaveCount(1);
  await expect(palette.getByTestId("palette-core.wait")).toBeVisible();
  await page.keyboard.type("xyz");
  await expect(palette.getByText("No action matches “waixyz”.")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(palette.getByTestId("palette-core.log")).toBeVisible();
});

test("drops an action from the palette and renames it", async ({ page }) => {
  await open(page);
  await drop(page, "core.log");
  const input = page.getByRole("textbox", { name: "Action id" });
  await expect(input).toBeFocused();
  await expect(input).toHaveValue("log-1");
  await expect(page.getByText("Enter: rename · Esc: keep “log-1”")).toBeVisible();
  await expect(page.getByTestId("unsaved")).toHaveText("● Unsaved changes");

  // Invalid and used ids are refused.
  await input.fill("9lives");
  await expect(page.getByRole("alert").filter({ hasText: "letters, digits" })).toBeVisible();
  await input.fill("combine");
  await expect(page.getByText("“combine” is already used")).toBeVisible();
  await input.fill("notify");
  await input.press("Enter");
  await expect(page.getByTestId("node-notify")).toBeVisible();
  await expect(page.getByTestId("node-log-1")).toHaveCount(0);

  const text = await yaml(page);
  expect(text).toContain("  - id: notify\n    type: core.log");
  const [, x, y] = text.match(/notify: \{ x: (\d+), y: (\d+) \}/)!.map(Number);
  expect(x % 8).toBe(0);
  expect(y % 8).toBe(0);
  expect(x).toBeGreaterThan(520);

  // Undo takes back the rename, then the new action.
  await page.keyboard.press("Control+z");
  await expect(page.getByTestId("node-log-1")).toBeVisible();
  await page.keyboard.press("Control+z");
  await expect(page.getByTestId("node-log-1")).toHaveCount(0);
});

test("Esc keeps the proposed id; a double-click renames an action and its links", async ({ page }) => {
  await open(page);
  await drop(page, "core.wait");
  await page.getByRole("textbox", { name: "Action id" }).press("Escape");
  await expect(page.getByTestId("node-wait-1")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Action id" })).toHaveCount(0);

  await page.getByTestId("node-combine").getByText("combine", { exact: true }).dblclick();
  const input = page.getByRole("textbox", { name: "Action id" });
  await input.fill("merge");
  await input.press("Enter");
  await expect(page.getByTestId("node-merge")).toBeVisible();
  const text = await yaml(page);
  expect(text).toContain("{ from: fetch-a, to: merge }");
  expect(text).toContain("{ from: fetch-b, to: merge }");
  expect(text).toMatch(/\n {2}merge: {6}\{ x: 520, y: 200 \}/);
  // Only ids change; comments mentioning "combine" stay as they are.
  expect(text).not.toMatch(/id: combine|combine:|to: combine/);
  expect(text).toContain('"combine" waits until both');
});

test("draws links; a link back to an earlier action becomes a loop link", async ({ page }) => {
  await open(page);
  await drop(page, "core.log");
  await page.getByRole("textbox", { name: "Action id" }).press("Enter");
  await expect(page.getByTestId("node-log-1")).toBeVisible();

  await connect(page, "report-error", "log-1");
  await expect.poll(() => yaml(page)).toContain("  - { from: report-error, to: log-1 }");
  await expect(page.getByTestId("link-6")).toHaveAttribute("data-kind", "success");

  // combine → fetch-a closes a cycle (fetch-a → combine), so it is marked as loop.
  await connect(page, "combine", "fetch-a");
  await expect(page.getByTestId("link-7")).toHaveText("✔ ↻");
  expect(await yaml(page)).toContain("  - { from: combine, to: fetch-a, loop: true }");
  // The only error is the missing message of the new, not yet configured action – no cycle.
  await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
  await expect(page.getByTestId("error-count")).toHaveAttribute("title", /log-1.*message/);
});

test("the link menu switches the kind and deletes links", async ({ page }) => {
  await open(page);
  const link = page.getByTestId("link-0"); // start → fetch-a
  await link.click();
  const menu = page.getByRole("menu", { name: "Link" });
  await expect(menu.getByRole("menuitemradio", { name: "On success" })).toHaveAttribute("aria-checked", "true");
  await menu.getByRole("menuitemradio", { name: "On failure" }).click();
  await expect(menu).toHaveCount(0);
  await expect(link).toHaveAttribute("data-kind", "failure");
  expect(await yaml(page)).toContain("  - { from: start, to: fetch-a, when: failure }");

  await page.getByTestId("link-0").click();
  await page.getByRole("menuitemradio", { name: "Always" }).click();
  await expect(page.getByTestId("link-0")).toHaveAttribute("data-kind", "always");
  await page.getByTestId("link-0").click();
  await page.getByRole("menuitemradio", { name: "On success" }).click();
  await expect(page.getByTestId("link-0")).toHaveAttribute("data-kind", "success");
  expect(await yaml(page)).toContain("  - { from: start, to: fetch-a }\n");

  // Escape closes the menu; "Delete link" removes the link, with undo.
  await page.getByTestId("link-0").click();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("menu")).toHaveCount(0);
  await page.getByTestId("link-0").click();
  await page.getByRole("menuitem", { name: /Delete link/ }).click();
  await expect(page.getByRole("status")).toContainText("Deleted 1 link");
  await expect(page.getByTestId("link-5")).toHaveCount(0);
  expect(await yaml(page)).not.toContain("{ from: start, to: fetch-a }");
  await page.getByRole("status").getByRole("button", { name: "Undo" }).click();
  await expect(page.getByTestId("link-5")).toBeVisible();

  // A selected link is also deleted with the Delete key.
  await page.getByTestId("link-0").click();
  await page.keyboard.press("Escape");
  await page.keyboard.press("Delete");
  await expect(page.getByTestId("link-5")).toHaveCount(0);
});
