import { expect, test, type Page } from "@playwright/test";

// Lists in the properties panel (mockup design/mockups/e14-lists.html, step E14a): tables,
// cards and chips built from the schema. Against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "pipelines/lists.pipeline.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = `kind: Pipeline
apiVersion: runnel/v1
name: lists
transforms:
  - id: gen
    type: core.generate-rows
    with:
      rows: 3
      rowNumberField: n
  - id: tag
    type: core.constant
    with:
      fields:
        - { name: source, value: runnel }
  - id: order
    type: core.sort
    with:
      fields:
        - { name: n }
  - id: days
    type: core.group-by
links:
  - { from: gen, to: tag }
  - { from: tag, to: order }
  - { from: order, to: days }
`;

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page, id: string) {
  await page.goto("/");
  await page.getByRole("tree").getByText("lists.pipeline.yaml", { exact: true }).click();
  await page.getByTestId(`node-${id}`).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue(id);
}

/** The saved file after the editor has saved it. */
async function saved(page: Page) {
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  return (await page.request.get(fileURL)).text();
}

async function type(page: Page, box: ReturnType<Page["getByRole"]>, text: string) {
  await box.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type(text);
  await box.blur();
}

test("a table adds, changes, reorders and removes entries", async ({ page }) => {
  await open(page, "tag");
  const list = page.getByTestId("list-fields");
  await expect(list.getByRole("row", { name: "field 1" }).getByRole("textbox").first()).toHaveText("source");

  // A new entry is written once its required name is there; a number stays a number.
  await list.getByRole("button", { name: "Add field" }).click();
  const fresh = list.getByRole("row", { name: "New field" });
  await type(page, fresh.getByRole("textbox").nth(1), "5");
  await type(page, fresh.getByRole("textbox").first(), "count");
  await expect(list.getByRole("row", { name: "field 2" })).toBeVisible();
  let text = await saved(page);
  expect(text).toContain("        - { name: source, value: runnel }\n        - { name: count, value: 5 }\n");

  // Alt+↓ on the handle moves an entry; × removes it.
  await list.getByRole("button", { name: /^Move field 1/ }).press("Alt+ArrowDown");
  await expect(list.getByRole("row", { name: "field 1" }).getByRole("textbox").first()).toHaveText("count");
  await list.getByRole("button", { name: "Remove field 2" }).click();
  text = await saved(page);
  expect(text).toContain("      fields:\n        - { name: count, value: 5 }\n  - id: order");
});

test("true/false as a box, only written when set", async ({ page }) => {
  await open(page, "order");
  const row = page.getByTestId("list-fields").getByRole("row", { name: "field 1" });
  // The box follows the file once the change is applied.
  await row.getByRole("checkbox").first().click();
  await expect(row.getByRole("checkbox").first()).toBeChecked();
  expect(await saved(page)).toContain("        - { name: n, descending: true }\n");
  await row.getByRole("checkbox").first().click();
  await expect(row.getByRole("checkbox").first()).not.toBeChecked();
  expect(await saved(page)).toContain("        - { name: n }\n");
});

test("chips for texts, cards with fields that apply only sometimes", async ({ page }) => {
  await open(page, "days");
  // groupBy: a list of texts.
  await page.getByRole("button", { name: "Add field" }).click();
  await page.getByRole("textbox", { name: "New field" }).fill("source");
  await page.getByRole("textbox", { name: "New field" }).press("Enter");
  await expect(page.getByRole("listitem").filter({ hasText: "source" })).toBeVisible();

  // aggregates: cards; separator only for concat.
  const cards = page.getByTestId("list-aggregates");
  await cards.getByRole("button", { name: "Add aggregate" }).click();
  const card = cards.getByRole("listitem", { name: "New aggregate" });
  await expect(card.getByText("Separator", { exact: true })).toHaveCount(0);
  await card.getByRole("combobox").selectOption("concat");
  await expect(card.getByText("Separator", { exact: true })).toBeVisible();
  await card.getByRole("combobox").selectOption("count");
  await type(page, card.getByRole("textbox").first(), "orders");
  await expect(cards.getByRole("listitem", { name: "aggregate 1" })).toBeVisible();
  const text = await saved(page);
  expect(text).toContain("      groupBy: [source]\n");
  expect(text).toContain("        - { name: orders, function: count }\n");
  expect(text).not.toContain("separator");
});
