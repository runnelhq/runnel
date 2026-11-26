import { expect, test, type Page } from "@playwright/test";

// Copy, cut and paste on the canvas (mockup design/mockups/e16-copy-paste.html): with the
// keys and the context menu, into another file, with new ids and updated references.
// Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398", permissions: ["clipboard-read", "clipboard-write"] });
test.describe.configure({ mode: "serial" });

const files: Record<string, string> = {
  "pipelines/clip-a.pipeline.yaml": `kind: Pipeline
apiVersion: runnel/v1
name: clip-a
transforms:
  - id: gen
    type: core.generate-rows
    with:
      count: 3
  - id: tag
    type: core.constant
    with:
      fields:
        - { name: source, value: runnel }
  - id: sink
    type: core.dummy
links:
  - { from: gen, to: tag }
  - { from: tag, to: sink }
layout:
  gen: { x: 0, y: 0 }
  tag: { x: 240, y: 0 }
  sink: { x: 480, y: 0 }
`,
  "pipelines/clip-b.pipeline.yaml": `kind: Pipeline
apiVersion: runnel/v1
name: clip-b
transforms:
  - id: sink
    type: core.dummy
layout:
  sink: { x: 0, y: 0 }
`,
  "workflows/clip.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: clip
actions:
  - id: start
    type: core.start
  - id: hello
    type: core.log
    with:
      message: hello
  - id: report
    type: core.log
    with:
      message: "status {{ results.hello.status }}"
links:
  - { from: start, to: hello }
  - { from: hello, to: report }
layout:
  start: { x: 0, y: 0 }
  hello: { x: 240, y: 0 }
  report: { x: 480, y: 0 }
`,
};

test.beforeEach(async ({ request }) => {
  for (const [path, text] of Object.entries(files)) await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: text });
});

async function open(page: Page, name: string, node: string) {
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  await expect(page.getByTestId(`node-${node}`)).toBeVisible();
}

/** Selects the given nodes: a click on the first, Ctrl+click on the others. */
async function select(page: Page, ...ids: string[]) {
  await page.getByTestId(`node-${ids[0]}`).click();
  for (const id of ids.slice(1)) await page.getByTestId(`node-${id}`).click({ modifiers: ["ControlOrMeta"] });
}

async function saved(page: Page, path: string) {
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  return (await page.request.get(`/api/v1/files/${path}`)).text();
}

const toast = (page: Page) => page.getByRole("status").filter({ hasText: /Pasted|Cut|can't|clipboard/ });

test("copies transforms into another pipeline with the link between them", async ({ page }) => {
  await page.goto("/");
  await open(page, "clip-a.pipeline.yaml", "tag");
  await select(page, "tag", "sink");
  await page.keyboard.press("ControlOrMeta+c");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("# Runnel: 2 transforms and 1 link from pipelines/clip-a.pipeline.yaml");

  await open(page, "clip-b.pipeline.yaml", "sink");
  const box = (await page.getByTestId("canvas").boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.press("ControlOrMeta+v");
  await expect(toast(page)).toContainText("Pasted 2 transforms and 1 link");
  await expect(toast(page)).toContainText("renamed sink to sink-2 · 1 link to other transforms left out");
  await expect(page.getByTestId("node-sink-2")).toBeVisible();
  // The pasted transforms are selected.
  await expect(page.getByTestId("node-tag")).toHaveClass(/selected/);
  const text = await saved(page, "pipelines/clip-b.pipeline.yaml");
  expect(text).toContain("  - id: tag\n    type: core.constant\n    with:\n      fields:\n        - { name: source, value: runnel }\n  - id: sink-2\n");
  expect(text).toContain("links:\n  - { from: tag, to: sink-2 }\n");
});

test("cuts with the context menu and undoes it", async ({ page }) => {
  await page.goto("/");
  await open(page, "clip-a.pipeline.yaml", "tag");
  await page.getByTestId("node-tag").click({ button: "right" });
  const menu = page.getByRole("menu", { name: "1 transform" });
  await expect(menu.getByRole("menuitem")).toHaveText(["CutCtrl+X", "CopyCtrl+C", "PasteCtrl+V", "DeleteDel"]);
  await menu.getByRole("menuitem", { name: /Cut/ }).click();
  await expect(toast(page)).toContainText("Cut 1 transform and 2 links");
  await expect(page.getByTestId("node-tag")).toHaveCount(0);
  await toast(page).getByRole("button", { name: "Undo" }).click();
  await expect(page.getByTestId("node-tag")).toBeVisible();

  // Pasting from the menu of the free canvas: back into the same file, with a new id.
  await page.getByTestId("canvas").locator(".react-flow__pane").click({ button: "right", position: { x: 40, y: 300 } });
  await page.getByRole("menu", { name: "Canvas" }).getByRole("menuitem", { name: /Paste/ }).click();
  await expect(page.getByTestId("node-tag-2")).toBeVisible();
});

test("updates expressions that name a pasted action", async ({ page }) => {
  await page.goto("/");
  await open(page, "clip.workflow.yaml", "report");
  await select(page, "hello", "report");
  await page.keyboard.press("ControlOrMeta+c");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("2 actions and 1 link");
  await page.keyboard.press("ControlOrMeta+v");
  await expect(toast(page)).toContainText("renamed hello, report to hello-2, report-2 · 1 link to other actions left out · 1 expression updated");
  const text = await saved(page, "workflows/clip.workflow.yaml");
  expect(text).toContain(`message: "status {{ results['hello-2'].status }}"`);
  expect(text).toContain("  - { from: hello-2, to: report-2 }\n");
});

test("does not paste actions into a pipeline", async ({ page }) => {
  await page.goto("/");
  await open(page, "clip.workflow.yaml", "report");
  await select(page, "hello");
  await page.keyboard.press("ControlOrMeta+c");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("1 action");
  await open(page, "clip-b.pipeline.yaml", "sink");
  await page.getByTestId("canvas").hover();
  await page.keyboard.press("ControlOrMeta+v");
  await expect(toast(page)).toContainText("Actions can't be pasted into a pipeline · the clipboard holds 1 action from clip.workflow.yaml");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();

  // Text that holds no steps.
  await page.evaluate(() => navigator.clipboard.writeText("hello"));
  await page.keyboard.press("ControlOrMeta+v");
  await expect(toast(page)).toContainText("The clipboard holds no actions or transforms");
});
