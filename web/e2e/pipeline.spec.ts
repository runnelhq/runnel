import { readFileSync } from "node:fs";
import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// The canvas of a pipeline (mockup design/mockups/s25a-pipeline.html, states canvas, link
// and problems): transforms from the palette, data and error links, the properties of
// transforms, links and the pipeline. Against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs), like edit.spec.ts.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

// customers.pipeline.yaml of examples/pipelines, with the layout of the mockup.
const path = "pipelines/canvas.pipeline.yaml";
const fileURL = `/api/v1/files/${path}`;
const original =
  readFileSync(new URL("../../examples/pipelines/pipelines/customers.pipeline.yaml", import.meta.url), "utf8") +
  `layout:
  read: { x: 16, y: 130 }
  types: { x: 256, y: 130 }
  active: { x: 496, y: 130 }
  map: { x: 736, y: 130 }
  rejected: { x: 256, y: 380 }
  tag: { x: 496, y: 380 }
  write: { x: 736, y: 380 }
`;

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("canvas.pipeline.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-write")).toBeVisible();
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

test("shows a pipeline as a graph with data and error links", async ({ page }) => {
  await open(page);
  await expect(page.getByTestId("file-summary")).toHaveText("Pipeline · 7 transforms · 6 links");
  // Links in the order of the file: types → rejected (links[1]) is an error link with ✖,
  // data links have no symbol.
  await expect(page.getByTestId("link-1")).toHaveAttribute("data-kind", "error");
  await expect(page.getByTestId("link-1")).toHaveText("✖");
  await expect(page.getByTestId("link-0")).toHaveAttribute("data-kind", "data");
  await expect(page.getByTestId("link-0")).toHaveText("");
  // An input transform takes no incoming links.
  await expect(page.getByTestId("node-read").locator(".react-flow__handle.target")).toHaveCount(0);
  await expect(page.getByTestId("node-types").locator(".react-flow__handle.target")).toHaveCount(1);
  // Without a selection, the panel shows the pipeline.
  await expect(panel(page).getByTestId("pipeline-properties")).toBeVisible();
  await expect(field(page, "name").getByRole("textbox")).toHaveValue("customers");
  await expect(panel(page)).toContainText("BATCH");

  // The palette lists the transforms by category.
  const palette = page.getByRole("region", { name: "Palette" });
  await expect(palette.getByRole("textbox", { name: "Search transforms" })).toBeVisible();
  await expect(palette.getByRole("group", { name: "Input" }).getByTestId("palette-csv.input")).toContainText("CSV Input");
  await expect(palette.getByRole("group", { name: "Combine" }).getByTestId("palette-core.sort")).toBeVisible();
  await expect(palette.getByTestId("palette-core.start")).toHaveCount(0);
  await expect(palette.getByText("Drag a transform onto the canvas, or onto a link to insert it between two steps.")).toBeVisible();
  // Every word counts on its own: the name of the transform in Apache Hop finds core.calculate.
  await palette.getByRole("textbox", { name: "Search transforms" }).fill("set field value");
  await expect(palette.getByTestId("palette-core.calculate")).toBeVisible();
  await expect(palette.getByTestId("palette-core.sort")).toHaveCount(0);
});

test("changes the type of a link in the menu and in the panel", async ({ page }) => {
  await open(page);
  await page.getByTestId("link-1").click();
  const menu = page.getByRole("menu", { name: "Link" });
  await expect(menu.getByRole("menuitemradio", { name: /Error/ })).toHaveAttribute("aria-checked", "true");
  // D makes it a data link; data links are written without a type.
  await page.keyboard.press("d");
  await expect(page.getByTestId("link-1")).toHaveAttribute("data-kind", "data");
  expect(await yaml(page)).toContain("  - { from: types, to: rejected }\n");

  // The panel shows the selected link; Error makes it an error link again.
  await page.getByTestId("link-1").click();
  await page.keyboard.press("Escape");
  const link = panel(page).getByTestId("link-properties");
  await expect(link.getByTestId("link-ends")).toHaveText("types → rejected");
  const type = link.getByRole("group", { name: "Type" });
  await expect(type.getByRole("button", { name: "Data" })).toHaveAttribute("aria-pressed", "true");
  await type.getByRole("button", { name: "Error" }).click();
  await expect(page.getByTestId("link-1")).toHaveAttribute("data-kind", "error");
  await expect(field(page, "type")).toContainText("core.convert passes records it cannot process to its error links");
  expect(await yaml(page)).toContain("  - { from: types, to: rejected, type: error }\n");

  await page.getByTestId("link-1").click();
  await menu.getByRole("menuitem", { name: /Delete link/ }).click();
  await expect(page.getByRole("status")).toContainText("Deleted 1 link");
  expect(await yaml(page)).not.toContain("to: rejected");
});

test("draws data links; a link that closes a cycle is not possible", async ({ page }) => {
  await open(page);
  // map → types would lead back: records in a pipeline only flow forward.
  await connect(page, "map", "types");
  await expect(page.getByTitle("Undo (Ctrl+Z)")).toBeDisabled();
  await connect(page, "rejected", "write");
  await expect(page.getByTestId("link-6")).toHaveAttribute("data-kind", "data");
  expect(await yaml(page)).toContain("  - { from: tag, to: write }\n  - { from: rejected, to: write }\n");
});

test("drops a transform from the palette", async ({ page }) => {
  await open(page);
  const read = (await page.getByTestId("node-read").boundingBox())!;
  const canvas = (await page.getByTestId("canvas").boundingBox())!;
  await page.getByTestId("palette-core.filter").dragTo(page.getByTestId("canvas"), {
    targetPosition: { x: read.x + read.width / 2 - canvas.x, y: read.y + 200 - canvas.y },
  });
  const id = page.getByRole("textbox", { name: "Action id" });
  await expect(id).toHaveValue("filter-1");
  await id.fill("positive");
  await id.press("Enter");
  await expect(page.getByTestId("node-positive")).toBeVisible();
  const text = await yaml(page);
  expect(text).toContain("  - id: positive\n    type: core.filter\n");
  expect(text).toMatch(/  positive: \{ x: \d+, y: \d+ \}\n/);
});

test("changes copies and the distribution of a transform", async ({ page }) => {
  await open(page);
  await page.getByTestId("node-active").click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("active");
  const copies = field(page, "copies");
  await expect(copies.getByRole("textbox")).toHaveValue("1");
  await copies.getByRole("button", { name: "More" }).click();
  await expect(page.getByTestId("node-active").getByTestId("copies")).toHaveText("×2 copies");
  await expect(panel(page)).toContainText("copies 2 · copy");
  // Distribute needs more than one outgoing data link.
  const distribution = panel(page).getByRole("group", { name: "Distribution" });
  await expect(distribution.getByRole("button", { name: "Distribute" })).toBeDisabled();
  await expect(field(page, "distribution")).toContainText("Only with more than one outgoing data link");
  await copies.getByRole("textbox").fill("4");
  await copies.getByRole("textbox").press("Enter");
  await expect(page.getByTestId("node-active").getByTestId("copies")).toHaveText("×4 copies");
  expect(await yaml(page)).toContain("  - id: active\n    type: core.filter\n    with:\n      condition: \"{{ record.umsatz > 0 }}\"\n    copies: 4\n");

  // Inputs and outputs run as one instance.
  await page.getByTestId("node-write").click();
  await expect(field(page, "copies").getByRole("button", { name: "More" })).toBeDisabled();
  await expect(field(page, "copies")).toContainText("csv.output runs as one instance");
});

test("changes the settings of the pipeline and jumps to problems", async ({ page }) => {
  await open(page);
  const batch = field(page, "settings.batchSize").getByRole("textbox");
  await batch.fill("0");
  await batch.blur();
  await expect(field(page, "settings.batchSize").getByRole("alert")).toContainText("whole number of 1 or more");
  await batch.fill("1000");
  await batch.blur();
  await panel(page).getByRole("checkbox", { name: /Strict/ }).click();
  await expect(panel(page).getByRole("checkbox", { name: /Strict/ })).toBeChecked();
  expect(await yaml(page)).toContain("settings:\n  batchSize: 1000\n  strict: true\n");

  // A buffer smaller than a batch is reported at the field.
  await field(page, "settings.bufferSize").getByRole("textbox").fill("10");
  await field(page, "settings.bufferSize").getByRole("textbox").blur();
  await expect(field(page, "settings.bufferSize").getByRole("alert")).toContainText("must be at least settings.batchSize");

  // An output in several copies (mockup S25a, state "problems"): the row selects it.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await page.request.put(fileURL, {
    headers: { "If-Match": "*" },
    data: original.replace("  - id: write\n    type: csv.output\n", "  - id: write\n    type: csv.output\n    copies: 2\n"),
  });
  await page.reload();
  await page.getByRole("tree").getByText("canvas.pipeline.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-write")).toHaveAttribute("data-severity", "error");
  await expect(page.getByTestId("node-write").getByTestId("copies")).toHaveText("×2 copies");
  await page.getByTestId("error-count").click();
  const rows = page.getByRole("region", { name: "Problems and log" }).getByRole("table").getByRole("row");
  await expect(rows.nth(1)).toContainText("csv.output cannot run in several copies");
  await expect(rows.nth(1)).toContainText("write · Copies");
  await rows.nth(1).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("write");
  await expect(field(page, "copies").getByRole("textbox")).toBeFocused();
  await expect(field(page, "copies").getByRole("alert")).toContainText("cannot run in several copies");
  // Fewer is still possible and fixes the problem.
  await field(page, "copies").getByRole("button", { name: "Fewer" }).click();
  await expect(page.getByTestId("node-write").getByTestId("copies")).toHaveCount(0);
  await expect(page.getByTestId("error-count")).toHaveCount(0);
});

test("shows the fields of the incoming records and of the links", async ({ page, request }) => {
  // A filter between tag and write that uses a field of the records.
  await request.put(fileURL, {
    headers: { "If-Match": "*" },
    data: original
      .replace("  - { from: tag, to: write }\n", "  - { from: tag, to: check }\n  - { from: check, to: write }\n")
      .replace("  - id: write\n", '  - id: check\n    type: core.filter\n    with:\n      condition: "{{ record.revenue > 0 }}"\n  - id: write\n'),
  });
  await open(page);
  await expect(page.getByTestId("error-count")).toHaveCount(0);

  // csv.input reads columns without fields: nothing is known before the pipeline runs.
  await page.getByTestId("node-active").click();
  await expect(panel(page)).toContainText("from types · open");
  await expect(panel(page).getByTestId("fields")).toHaveText("fields known only when the pipeline runs");

  // After map (select-fields), the fields are known; the one in the condition is marked.
  await page.getByTestId("node-check").click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("check");
  const fields = panel(page).getByTestId("fields").getByRole("listitem");
  await expect(fields).toHaveText(["externalIdany", "companyNameany", "revenueany", "sourcestring", "batchany"]);
  await expect(fields.nth(2)).toHaveAttribute("data-used", "true");
  await expect(fields.nth(0)).not.toHaveAttribute("data-used", "true");
  // An input has no incoming fields.
  await page.getByTestId("node-read").click();
  await expect(panel(page)).not.toContainText("Incoming fields");

  // An error link has the fields of the incoming records and the error fields.
  await page.getByTestId("link-1").click();
  await page.keyboard.press("Escape");
  await expect(panel(page).getByTestId("fields").getByRole("listitem")).toHaveText([
    "_errorstring",
    "_errorFieldstring",
    "_errorTransformstring",
    "and fields known only when the pipeline runs",
  ]);
  // A data link: the records of its source.
  await page.getByTestId("link-6").click();
  await page.keyboard.press("Escape");
  await expect(panel(page).getByTestId("link-ends")).toHaveText("check → write");
  await expect(panel(page)).toContainText("closed");
  await expect(panel(page).getByTestId("fields").getByRole("listitem")).toHaveCount(5);

  // While a link leads nowhere, the fields are not derived.
  await page.request.put(fileURL, {
    headers: { "If-Match": "*" },
    data: original.replace("  - { from: tag, to: write }\n", "  - { from: tag, to: write }\n  - { from: tag, to: nowhere }\n"),
  });
  await page.reload();
  await page.getByRole("tree").getByText("canvas.pipeline.yaml", { exact: true }).click();
  await page.getByTestId("node-write").click();
  await expect(panel(page)).toContainText("The fields are shown once the transforms and links have no errors.");
});

// A pipeline whose filter fails on some records: they go over the error link.
const checks = `kind: Pipeline
apiVersion: runnel/v1
name: checks
transforms:
  - { id: gen, type: core.generate-rows, with: { rows: 5, rowNumberField: n } }
  - id: check
    type: core.filter
    with:
      condition: '{{ record.n > 2 ? record.n < 5 : "unknown" }}'
  - { id: kept, type: core.dummy }
  - { id: rejected, type: core.dummy }
links:
  - { from: gen, to: check }
  - { from: check, to: kept }
  - { from: check, to: rejected, type: error }
`;

test("runs a pipeline and shows the records of each transform and link", async ({ page, request }) => {
  await request.put("/api/v1/files/pipelines/checks.pipeline.yaml", { headers: { "If-Match": "*" }, data: checks });
  await page.goto("/");
  await page.getByRole("tree").getByText("checks.pipeline.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-check")).toBeVisible();

  // The project has two environments: the first run asks.
  await page.getByRole("button", { name: "Run", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Run checks.pipeline.yaml" });
  await dialog.getByRole("button", { name: "Run" }).click();
  await expect(page.getByTestId("run-status")).toContainText("✔ Last run succeeded", { timeout: 10_000 });

  // Counts at the transforms: read → written, errors with ✖; an input only what it wrote.
  await expect(page.getByTestId("node-gen").getByTestId("counts")).toHaveText("5");
  await expect(page.getByTestId("node-check").getByTestId("counts")).toHaveText("5 → 2 · ✖ 2");
  await expect(page.getByTestId("node-check")).toHaveAttribute("data-run", "succeeded");
  // Records per link: gen → check, check → kept, check → rejected (error link).
  await expect(page.getByTestId("rows-0")).toHaveText("5");
  await expect(page.getByTestId("rows-1")).toHaveText("2");
  await expect(page.getByTestId("rows-2")).toHaveText("2");

  // The log follows the run, with the counts of each transform.
  const log = page.getByRole("log", { name: "Run log" });
  await expect(log).toContainText('pipeline "checks" started (environment dev)');
  await expect(log).toContainText("read 5, written 2, errors 2 in");
  await expect(log).toContainText('pipeline "checks" succeeded in');

  // The panel shows the result of the selected transform.
  await page.getByTestId("node-check").click();
  await expect(panel(page).getByTestId("run-counts")).toHaveText("5read2written✖ 2to error links");
});
