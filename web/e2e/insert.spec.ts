import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// Splitting a link (mockup design/mockups/e22-split-link.html): a step dropped from the
// palette onto a link, or chosen with "Insert step…" in the link menu, goes between the
// two ends of the link. Against the server on a temporary copy of examples/hello.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const workflow = "workflows/insert.workflow.yaml";
const pipeline = "pipelines/insert.pipeline.yaml";
const original = readFileSync(new URL("../../examples/hello/workflows/parallel.workflow.yaml", import.meta.url), "utf8");
const pipelineText = `kind: Pipeline
apiVersion: runnel/v1
name: insert
transforms:
  - { id: gen, type: core.generate-rows, with: { count: 1 } }
  - { id: out, type: core.dummy }
  - { id: rejected, type: core.dummy }
links:
  - { from: gen, to: out }
  - { from: gen, to: rejected, type: error }
layout:
  gen: { x: 40, y: 200 }
  out: { x: 520, y: 200 }
  rejected: { x: 520, y: 420 }
`;

test.beforeEach(async ({ request }) => {
  await request.put(`/api/v1/files/${workflow}`, { headers: { "If-Match": "*" }, data: original });
  await request.put(`/api/v1/files/${pipeline}`, { headers: { "If-Match": "*" }, data: pipelineText });
});

test.afterAll(async ({ request }) => {
  for (const f of [workflow, pipeline]) await request.delete(`/api/v1/files/${f}`, { headers: { "If-Match": "*" } });
});

async function open(page: Page, file: string, node: string) {
  await page.goto("/");
  await page.getByRole("tree").getByText(file.split("/").pop()!, { exact: true }).click();
  await expect(page.getByTestId(`node-${node}`)).toBeVisible();
}

async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return text;
}

/** Drags a palette entry over the symbol of a link and keeps the mouse down. */
async function dragOver(page: Page, type: string, link: string) {
  await page.getByTestId(`palette-${type}`).hover();
  await page.mouse.down();
  const box = (await page.getByTestId(`link-${link}`).boundingBox())!;
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  await page.mouse.move(x, y, { steps: 8 });
  // Chromium sends dragover only now and then while the mouse moves: a few more at the target.
  for (const dx of [1, 0, 1, 0]) {
    await page.mouse.move(x + dx, y);
    await page.waitForTimeout(60);
  }
}

test("a step dropped onto a link goes between its ends; the condition stays on the first link", async ({ page }) => {
  await open(page, workflow, "fetch-b");
  // link 5: fetch-b → report-error, On failure
  await dragOver(page, "core.log", "5");
  await expect(page.getByTestId("drop-link-5")).toBeVisible();
  await expect(page.getByTestId("drop-hint")).toContainText("Drop to insert between fetch-b and report-error");
  await expect(page.getByTestId("drop-hint")).toContainText("On failure stays on fetch-b → log-1");
  await page.mouse.up();

  await expect(page.getByTestId("node-log-1")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("Inserted log-1 between fetch-b and report-error");
  const text = await yaml(page);
  expect(text).not.toContain("{ from: fetch-b, to: report-error, when: failure }");
  expect(text).toContain("from: fetch-b");
  expect(text).toMatch(/from: fetch-b, to: log-1, when: failure/);
  expect(text).toMatch(/from: log-1, to: report-error \}/);

  // One step to undo.
  await page.keyboard.press("Control+z");
  await expect(page.getByTestId("node-log-1")).toHaveCount(0);
  expect(await yaml(page)).toContain("{ from: fetch-b, to: report-error, when: failure }");
});

test("Insert step… in the link menu searches the palette and inserts the chosen type", async ({ page }) => {
  await open(page, workflow, "fetch-a");
  // link 2: fetch-a → combine
  await page.getByTestId("link-2").click();
  const menu = page.getByRole("menu", { name: "Link" });
  await menu.getByRole("menuitem", { name: "Insert step…" }).click();
  const search = menu.getByRole("textbox", { name: "Search actions to insert" });
  await expect(search).toBeFocused();
  // core.start cannot go between two steps.
  await search.fill("start");
  await expect(menu.getByRole("option")).toHaveCount(0);
  await search.fill("wai");
  await expect(menu.getByRole("option", { selected: true })).toContainText("core.wait");
  await search.press("Enter");

  await expect(page.getByTestId("node-wait-1")).toBeVisible();
  const text = await yaml(page);
  expect(text).toMatch(/from: fetch-a, to: wait-1 \}/);
  expect(text).toMatch(/from: wait-1, to: combine \}/);
});

test("in a pipeline an error link stays the first link; an input cannot go between two steps", async ({ page }) => {
  await open(page, pipeline, "gen");
  // An input transform: the link is not highlighted, the hint says why.
  await dragOver(page, "csv.input", "1");
  await expect(page.getByTestId("drop-hint")).toContainText("csv.input has no input – it can't go between two steps");
  await expect(page.getByTestId("drop-link-1")).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page.mouse.up();
  await page.keyboard.press("Control+z"); // the input was placed on the canvas
  await expect(page.getByTestId("node-input-1")).toHaveCount(0);

  // link 1: gen → rejected, error link
  await dragOver(page, "core.filter", "1");
  await expect(page.getByTestId("drop-hint")).toContainText("the error link stays on gen → filter-1");
  await page.mouse.up();
  await expect(page.getByTestId("node-filter-1")).toBeVisible();
  const text = await yaml(page);
  expect(text).toMatch(/from: gen, to: filter-1, type: error/);
  expect(text).toMatch(/from: filter-1, to: rejected \}/);
});
