import { expect, test, type Page } from "@playwright/test";

// Lists in the properties panel (mockup design/mockups/e14-lists.html, steps E14a and
// E14b): tables, cards and chips built from the schema, the fields of the incoming records
// and the targets of the data links to choose from, problems at the entry, the help. Against the server on a temporary copy of
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
  - id: route
    type: core.switch
    with:
      cases:
        - { when: "{{ record.n == 1 }}", to: sink }
  - id: sink
    type: core.dummy
links:
  - { from: gen, to: tag }
  - { from: tag, to: order }
  - { from: order, to: days }
  - { from: gen, to: route }
  - { from: route, to: sink }
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
  await page.getByRole("combobox", { name: "New field" }).fill("source");
  await page.getByRole("combobox", { name: "New field" }).press("Enter");
  await expect(page.getByRole("listitem").filter({ hasText: "source" })).toBeVisible();

  // aggregates: cards; separator only for concat.
  const cards = page.getByTestId("list-aggregates");
  await cards.getByRole("button", { name: "Add aggregate" }).click();
  const card = cards.getByRole("listitem", { name: "New aggregate" });
  await expect(card.getByText("Separator", { exact: true })).toHaveCount(0);
  await card.getByRole("combobox", { name: "Function" }).selectOption("concat");
  await expect(card.getByText("Separator", { exact: true })).toBeVisible();
  await card.getByRole("combobox", { name: "Function" }).selectOption("count");
  await type(page, card.getByRole("textbox").first(), "orders");
  await expect(cards.getByRole("listitem", { name: "aggregate 1" })).toBeVisible();
  const text = await saved(page);
  expect(text).toContain("      groupBy: [source]\n");
  expect(text).toContain("        - { name: orders, function: count }\n");
  expect(text).not.toContain("separator");
});

test("offers the fields of the incoming records", async ({ page }) => {
  await open(page, "order");
  const name = page.getByTestId("list-fields").getByRole("row", { name: "field 1" }).getByRole("combobox");
  await name.click();
  const options = page.getByRole("listbox", { name: "Fields of the incoming records" }).getByRole("option");
  await expect(options.filter({ hasText: "source" })).toBeVisible();
  await expect(options.filter({ hasText: /^n/ })).toContainText("int");
  // Typing narrows the list; Enter takes the chosen one.
  await name.fill("sou");
  await expect(options).toHaveCount(1);
  await name.press("ArrowDown");
  await name.press("Enter");
  await expect(name).toHaveValue("source");
  expect(await saved(page)).toContain("        - { name: source }\n");
});

test("shows a problem at its entry", async ({ page, request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original.replace("        - { name: n }\n", "        - { name: n }\n        - { name: nn }\n") });
  await open(page, "order");
  const list = page.getByTestId("list-fields");
  await expect(list.getByTestId("entry-problems")).toHaveCount(1);
  await expect(list.getByTestId("entry-problems")).toContainText('"nn"');
});

test("offers the targets of the data links", async ({ page }) => {
  await open(page, "route");
  const cards = page.getByTestId("list-cases");
  await cards.getByRole("button", { name: "Add case" }).click();
  const card = cards.getByRole("listitem", { name: "New case" });
  const to = card.getByRole("combobox", { name: "To" });
  await expect(to.getByRole("option")).toHaveText(["to", "sink"]);
  await to.selectOption("sink");
  // fill, not typing: the editor closes "{{" by itself.
  await card.getByRole("textbox").first().fill("{{ record.n > 1 }}");
  await card.getByRole("textbox").first().blur();
  await expect(cards.getByRole("listitem", { name: "case 2" })).toBeVisible();
  expect(await saved(page)).toContain('        - { when: "{{ record.n == 1 }}", to: sink }\n        - { when: "{{ record.n > 1 }}", to: sink }\n');
});

test("help with an example of the configuration", async ({ page }) => {
  await open(page, "tag");
  await page.getByRole("button", { name: "Help" }).click();
  await expect(page.getByTestId("help-example")).toContainText("- id: tag\n  type: core.constant\n  with:\n    fields:");
  await expect(page.getByText("runnel catalog core.constant")).toBeVisible();
  await page.getByRole("button", { name: "Show in YAML" }).click();
  const line = original.split("\n").findIndex((l) => l.includes("id: tag")) + 1;
  await expect(page.getByTestId("file-summary")).toContainText(`Ln ${line},`);
});
