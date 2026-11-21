import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";

// Data preview (mockup design/mockups/s44-preview.html): a pipeline runs up to a transform
// with the first records of its inputs; nothing is written. Against the server on a
// temporary copy of examples/hello (e2e/serve-copy.mjs); relative paths of inputs refer
// to the project folder.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "pipelines/preview.pipeline.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = readFileSync(new URL("../../examples/pipelines/pipelines/customers.pipeline.yaml", import.meta.url), "utf8");
// The pipeline reads data/customers.csv, relative to the project folder (ADR 0020).
const customers = readFileSync(new URL("../../examples/pipelines/data/customers.csv", import.meta.url));

test.beforeEach(async ({ request }) => {
  await request.put("/api/v1/files/data/customers.csv", { headers: { "If-Match": "*" }, data: customers });
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function openPreview(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("preview.pipeline.yaml", { exact: true }).click();
  await page.getByTestId("node-types").click();
  await page.getByRole("button", { name: "Preview data" }).click();
}

const bar = (page: Page) => page.getByRole("region", { name: "Problems and log" });

test("previews the records up to a transform", async ({ page }) => {
  await openPreview(page);
  await expect(page.getByTestId("preview-hint")).toContainText("Preview up to types · first 100 records · nothing is written");
  const preview = bar(page).getByTestId("preview");
  await expect(preview.getByTestId("preview-count")).toHaveText("3 records");
  const table = preview.getByRole("table", { name: "Records" });
  await expect(table.locator("th")).toHaveText(["#", "kdnrint", "namestring", "umsatzdecimal", "ortstring", "telefonstring"]);
  await expect(table.locator("tbody tr").first()).toContainText("Müller GmbH");
  // On the canvas: the counts of the preview, the transforms after it faded.
  await expect(page.getByTestId("node-types").getByTestId("counts")).toHaveText("4 → 3 · ✖ 1");
  await expect(page.getByTestId("node-map")).toHaveAttribute("data-run", "pending");

  // The error link with the reason, then as JSON.
  await preview.getByRole("combobox", { name: "Records of" }).selectOption({ label: "types → rejected (error link)" });
  await expect(preview.getByTestId("preview-count")).toHaveText("1 record");
  await expect(table).toContainText("k. A.");
  await expect(table.locator("th", { hasText: "_error" }).first()).toBeVisible();
  await preview.getByRole("button", { name: "JSON" }).click();
  await expect(preview.getByTestId("preview-json")).toContainText('"umsatz": "k. A."');
  await preview.getByRole("button", { name: "Table" }).click();

  // Ending the preview removes it from the canvas.
  await bar(page).getByRole("button", { name: "End the preview" }).click();
  await expect(page.getByTestId("preview-hint")).toHaveCount(0);
  await expect(page.getByTestId("node-types").getByTestId("counts")).toHaveCount(0);
});

test("tells where the preview stopped", async ({ page, request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original.replace("data/customers.csv", "data/customer.csv") });
  await openPreview(page);
  const alert = bar(page).getByTestId("preview").getByRole("alert");
  await expect(alert).toContainText("The preview stopped at read");
  await expect(alert).toContainText("customer.csv");
  await expect(page.getByTestId("node-read")).toHaveAttribute("data-run", "failed");
});

test("a run ends the preview and shows its own counts", async ({ page, request }) => {
  const rowsPath = "pipelines/rows.pipeline.yaml";
  const rows = "kind: Pipeline\napiVersion: runnel/v1\nname: rows\ntransforms:\n  - { id: gen, type: core.generate-rows, with: { rows: 250 } }\n  - { id: pass, type: core.dummy }\nlinks:\n  - { from: gen, to: pass }\n";
  await request.put(`/api/v1/files/${rowsPath}`, { headers: { "If-Match": "*" }, data: rows });
  await page.goto("/");
  await page.getByRole("tree").getByText("rows.pipeline.yaml", { exact: true }).click();
  await page.getByTestId("node-pass").click();
  await page.getByRole("button", { name: "Preview data" }).click();
  // The preview reads the first 100 records per input …
  await expect(page.getByTestId("node-pass").getByTestId("counts")).toHaveText("100 → 100");

  // … a run all of them, and the canvas shows its counts.
  await page.getByRole("button", { name: "Run", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Run rows.pipeline.yaml" });
  // The first run asks for the environment (the project has two).
  await dialog.getByRole("button", { name: "Run" }).click();
  await expect(page.getByTestId("preview-hint")).toHaveCount(0);
  await expect(page.getByTestId("node-pass").getByTestId("counts")).toHaveText("250 → 250");
  await request.delete(`/api/v1/files/${rowsPath}`, { headers: { "If-Match": "*" } });
});
