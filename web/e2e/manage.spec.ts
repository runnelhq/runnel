import { expect, test, type Page } from "@playwright/test";

// Files in the explorer (mockup design/mockups/e11-files.html): creating files from a
// template and folders, renaming and deleting. Against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const tree = (page: Page) => page.getByRole("tree", { name: "Project files" });
const row = (page: Page, name: string) => tree(page).getByRole("treeitem", { name, exact: true }).locator(":scope > div").first();

async function menuOf(page: Page, name: string) {
  await row(page, name).click({ button: "right" });
  return page.getByRole("menu", { name: `Actions for ${name}` });
}

// A new trigger points to the first workflow of the project. Other specs create and delete
// workflows on the same server at the same time (e.g. call.workflow.yaml), so this one is
// first by name and stays while the tests run.
const firstWorkflow = "workflows/0-first.workflow.yaml";

test.beforeAll(async ({ request }) => {
  const text = "kind: Workflow\napiVersion: runnel/v1\nname: 0-first\nactions:\n  - id: start\n    type: core.start\n";
  expect((await request.put(`/api/v1/files/${firstWorkflow}`, { headers: { "If-Match": "*" }, data: text })).ok()).toBe(true);
});

test.afterAll(async ({ request }) => {
  for (const f of [firstWorkflow, "workflows/import.workflow.yaml", "workflows/loader.workflow.yaml", "triggers/new-trigger.yaml"]) {
    await request.delete(`/api/v1/files/${f}`, { headers: { "If-Match": "*" } });
  }
  await request.delete("/api/v1/folders/archive");
});

test("creates a workflow from the template and renames it", async ({ page, request }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Workflow/ }).click();
  const input = tree(page).getByRole("textbox", { name: "Name of the new workflow" });
  await expect(input).toHaveValue("new-workflow");
  await expect(tree(page)).toContainText("Enter creates workflows/new-workflow.workflow.yaml");
  await input.fill("import");
  await input.press("Enter");
  // The new file opens; it is a valid workflow with a start action.
  await expect(page.getByRole("tab", { name: /import\.workflow\.yaml/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByTestId("node-start")).toBeVisible();
  expect(await (await request.get("/api/v1/files/workflows/import.workflow.yaml")).text()).toContain("name: import\n");

  // Renaming: a name in use is reported at the field; the tab follows the new name.
  await (await menuOf(page, "import.workflow.yaml")).getByRole("menuitem", { name: /^Rename/ }).click();
  const rename = tree(page).getByRole("textbox", { name: "New name of import.workflow.yaml" });
  await rename.fill("hello");
  await rename.press("Enter");
  await expect(tree(page)).toContainText("workflows/hello.workflow.yaml already exists");
  await rename.fill("loader");
  await rename.press("Enter");
  await expect(row(page, "loader.workflow.yaml")).toBeVisible();
  await expect(page.getByRole("tab", { name: /loader\.workflow\.yaml/ })).toHaveAttribute("aria-selected", "true");
  expect((await request.get("/api/v1/files/workflows/import.workflow.yaml")).status()).toBe(404);
});

// New → Trigger asks for name, type and target (mockup E20, "new"); the trigger starts
// disabled with the required fields of its type and opens as a form.
test("creates a trigger that starts disabled", async ({ page, request }) => {
  await page.goto("/");
  await expect(row(page, "workflows")).toBeVisible();
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Trigger/ }).click();
  const dialog = page.getByRole("dialog", { name: "New trigger" });
  await expect(dialog.getByRole("combobox", { name: "Target" })).toHaveValue(firstWorkflow);
  await dialog.getByRole("radio", { name: /Interval/ }).click();
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("tab", { name: /new-trigger\.yaml/ })).toBeVisible();
  const text = await (await request.get("/api/v1/files/triggers/new-trigger.yaml")).text();
  expect(text).toBe(`kind: Trigger\napiVersion: runnel/v1\nname: new-trigger\ntype: interval\ntarget: ${firstWorkflow}\nenabled: false\nwith:\n  every: 1h\n`);
  await expect(page.getByTestId("trigger-form").getByRole("switch", { name: "Enabled" })).toHaveAttribute("aria-checked", "false");
  await expect(page.getByTestId("error-count")).toHaveCount(0);
});

test("creates and deletes folders and files", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: "Folder" }).click();
  const folder = tree(page).getByRole("textbox", { name: "Name of the new folder" });
  await folder.fill("archive");
  await folder.press("Enter");
  await expect(row(page, "archive")).toBeVisible();

  await (await menuOf(page, "archive")).getByRole("menuitem", { name: "New file…" }).click();
  const file = tree(page).getByRole("textbox", { name: "Name of the new file" });
  await file.fill("notes.csv");
  await file.press("Enter");
  await expect(row(page, "notes.csv")).toBeVisible();

  // A folder with files cannot be deleted.
  await expect((await menuOf(page, "archive")).getByRole("menuitem", { name: /Delete folder/ })).toBeDisabled();
  await page.keyboard.press("Escape");

  await (await menuOf(page, "notes.csv")).getByRole("menuitem", { name: "Delete" }).click();
  await page.getByRole("alertdialog", { name: "Delete notes.csv?" }).getByRole("button", { name: "Delete" }).click();
  await expect(row(page, "notes.csv")).toHaveCount(0);
  await expect(page.getByRole("tab", { name: /notes\.csv/ })).toHaveCount(0);

  await (await menuOf(page, "archive")).getByRole("menuitem", { name: /Delete folder/ }).click();
  await page.getByRole("alertdialog", { name: "Delete archive?" }).getByRole("button", { name: "Delete" }).click();
  await expect(row(page, "archive")).toHaveCount(0);
});

test("names the files that refer to a workflow before deleting it", async ({ page }) => {
  await page.goto("/");
  await (await menuOf(page, "hello.workflow.yaml")).getByRole("menuitem", { name: "Delete" }).click();
  const dialog = page.getByRole("alertdialog", { name: "Delete hello.workflow.yaml?" });
  await expect(dialog.getByTestId("references")).toContainText("triggers/");
  await expect(dialog.getByTestId("references")).toContainText("– target");
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(row(page, "hello.workflow.yaml")).toBeVisible();
});

test("keeps project.yaml and creates it again if it is missing", async ({ page }) => {
  await page.goto("/");
  // project.yaml cannot be renamed, moved or deleted: without it the project is not found.
  const menu = await menuOf(page, "project.yaml");
  for (const name of [/^Rename/, /^Move to/, /^Delete/]) await expect(menu.getByRole("menuitem", { name })).toBeDisabled();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await expect(page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Project file/ })).toHaveCount(0);
  await page.keyboard.press("Escape");

  // Deleted outside the editor: "New" offers to create it, named after the project folder.
  await page.route("**/api/v1/projects/current/tree", async (route) => {
    const tree = await (await route.fetch()).json();
    tree.children = tree.children.filter((e: { path: string }) => e.path !== "project.yaml");
    await route.fulfill({ json: tree });
  });
  let created = "";
  await page.route("**/api/v1/files/project.yaml", async (route) => {
    if (route.request().method() !== "PUT") return route.fallback();
    created = route.request().postData() ?? "";
    await route.fulfill({ status: 201, body: "" });
  });
  await page.reload();
  await expect(row(page, "project.yaml")).toHaveCount(0);
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Project file/ }).click();
  await expect.poll(() => created).toMatch(/^kind: Project\napiVersion: runnel\/v1\nname: [A-Za-z0-9_-]+\n$/);
  // After creating, the editor loads the tree again; a request still in the route when the
  // page closes must not fail the test.
  await page.unrouteAll({ behavior: "ignoreErrors" });
});
