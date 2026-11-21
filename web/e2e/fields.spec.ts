import { readFileSync } from "node:fs";
import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// "Detect fields" (mockup design/mockups/s46-fields.html): csv.input proposes its fields
// from the first rows of its file; they are checked in a dialog and written to
// "fields:". Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs);
// relative paths of inputs refer to the folder the server was started in (web/).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "pipelines/fields.pipeline.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = readFileSync(new URL("../../examples/pipelines/pipelines/customers.pipeline.yaml", import.meta.url), "utf8").replace(
  "path: examples/pipelines/data/customers.csv",
  "path: ../examples/pipelines/data/customers.csv",
);

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("fields.pipeline.yaml", { exact: true }).click();
  await page.getByTestId("node-read").click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("read");
}

/** The text of the YAML view. */
async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return text;
}

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const dialog = (page: Page) => page.getByRole("dialog", { name: "Detect fields" });
const row = (page: Page, name: string) => dialog(page).getByTestId("detected-field").filter({ hasText: new RegExp(`^${name}`) });

test("detects the fields of a CSV file and writes them", async ({ page }) => {
  await open(page);
  const fields = panel(page).locator('[data-field="with.fields"]');
  await expect(fields).toContainText("Without fields, all columns are read as text");
  await fields.getByRole("button", { name: "Detect fields…" }).click();

  // The proposal: a type only if every value fits, with the reason.
  await expect(dialog(page)).toContainText("Read 4 rows (all)");
  await expect(dialog(page).getByTestId("detected-field")).toHaveCount(5);
  await expect(row(page, "kdnr").getByRole("combobox")).toHaveValue("int");
  await expect(row(page, "umsatz").getByRole("combobox")).toHaveValue("string");
  await expect(row(page, "umsatz")).toContainText("3 of 4 values are decimal (#.##0,00); “k. A.” in line 5 is not. Kept as text.");
  await expect(row(page, "telefon")).toContainText("Leading zero: kept as text");

  // Changed in the dialog: telefon is not read.
  await row(page, "telefon").getByRole("checkbox").uncheck();
  await dialog(page).getByRole("button", { name: "Write 4 fields" }).click();
  await expect(dialog(page)).toHaveCount(0);
  await expect(page.getByRole("status").filter({ hasText: "Wrote 4 fields to read" })).toBeVisible();
  await expect(panel(page).getByTestId("configured-fields").locator("li")).toHaveCount(4);
  await expect(fields).toContainText("Change single fields in the YAML view.");
  expect(await yaml(page)).toContain(
    "      delimiter: \";\"\n      fields:\n        - { name: kdnr, type: int }\n        - { name: name }\n        - { name: umsatz }\n        - { name: ort }\n  - id: types\n",
  );

  // Detecting again keeps the listed fields; the column not read so far is new.
  await page.getByTestId("node-read").click();
  await fields.getByRole("button", { name: "Detect fields…" }).click();
  await expect(row(page, "kdnr")).toContainText("listed");
  await expect(row(page, "telefon")).toContainText("new");
  await expect(row(page, "telefon").getByRole("checkbox")).not.toBeChecked();
  await dialog(page).getByRole("button", { name: "Cancel" }).click();
  await expect(dialog(page)).toHaveCount(0);
});

test("undoes writing the fields", async ({ page }) => {
  await open(page);
  await panel(page).getByRole("button", { name: "Detect fields…" }).click();
  await dialog(page).getByRole("button", { name: "Write 5 fields" }).click();
  await page.getByRole("status").filter({ hasText: "Wrote 5 fields to read" }).getByRole("button", { name: "Undo" }).click();
  expect(await yaml(page)).toBe(original);
});

test("tells why the file cannot be read", async ({ page, request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original.replace("data/customers.csv", "data/customer.csv") });
  await open(page);
  await panel(page).getByRole("button", { name: "Detect fields…" }).click();
  const alert = dialog(page).getByRole("alert");
  await expect(alert).toContainText("The input cannot be read");
  await expect(alert).toContainText("customer.csv");
  await expect(alert).toContainText("Relative paths refer to the folder runnel was started in");
  await expect(dialog(page).getByRole("button", { name: "Try again" })).toBeVisible();
});
