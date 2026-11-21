import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// Organizing the project (mockup design/mockups/e12-organize.html): moving files with
// "Move to…" and by dragging, updating the references of other files, triggers in
// subfolders. Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
// The folder sorts after workflows/, so that new triggers of other specs keep their target.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const dir = "zz-organize";
const files: Record<string, string> = {
  [`${dir}/count.pipeline.yaml`]: "kind: Pipeline\napiVersion: runnel/v1\nname: count\ntransforms:\n  - { id: rows, type: core.generate-rows, with: { rows: 3 } }\n",
  [`${dir}/main.workflow.yaml`]: `kind: Workflow\napiVersion: runnel/v1\nname: main\nactions:\n  - { id: start, type: core.start }\n  - { id: count, type: core.run-pipeline, with: { pipeline: ${dir}/count.pipeline.yaml } }\nlinks:\n  - { from: start, to: count }\n`,
  [`${dir}/organize-notes.csv`]: "a;b\n",
  [`triggers/${dir}/main.yaml`]: `kind: Trigger\napiVersion: runnel/v1\nname: organize-main\ntype: interval\ntarget: ${dir}/main.workflow.yaml\nenabled: false\nwith:\n  every: 1h\n`,
};

const tree = (page: Page) => page.getByRole("tree", { name: "Project files" });
const row = (page: Page, name: string) => tree(page).getByRole("treeitem", { name, exact: true }).locator(":scope > div").first();
const text = async (page: Page, path: string) => (await page.request.get(`/api/v1/files/${path}`)).text();

test.beforeAll(async ({ request }) => {
  for (const [path, content] of Object.entries(files)) {
    expect((await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: content })).ok()).toBe(true);
  }
});

test.afterAll(async ({ request }) => {
  for (const f of ["triggers/zz-organize/main.yaml", "zz-organize/start.workflow.yaml", "zz-organize/main.workflow.yaml", "zz-organize/shared/count.pipeline.yaml", "zz-organize/shared/organize-notes.csv"]) {
    await request.delete(`/api/v1/files/${f}`, { headers: { "If-Match": "*" } });
  }
  for (const f of ["triggers/zz-organize", "zz-organize/shared", "zz-organize"]) await request.delete(`/api/v1/folders/${f}`);
});

test("moves a file to a new folder and updates the references", async ({ page }) => {
  await page.goto("/");
  await row(page, "count.pipeline.yaml").click({ button: "right" });
  await page.getByRole("menu", { name: "Actions for count.pipeline.yaml" }).getByRole("menuitem", { name: "Move to…" }).click();
  const move = page.getByRole("dialog", { name: "Move count.pipeline.yaml" });
  // The current folder can only be chosen to create a folder in it.
  await move.getByRole("option", { name: dir, exact: true }).click();
  await expect(move.getByRole("button", { name: "Move", exact: true })).toBeDisabled();
  await move.getByRole("option", { name: "(project)", exact: true }).click();
  await expect(move).toContainText("New path: count.pipeline.yaml");
  // The new folder is created in the selected one.
  await move.getByRole("button", { name: "New folder" }).click();
  const name = move.getByRole("textbox", { name: "Name of the new folder" });
  await name.fill(`${dir}/x`);
  await name.press("Enter");
  await expect(move).toContainText("must not be empty or contain /");
  await move.getByRole("option", { name: dir, exact: true }).click();
  await name.fill("shared");
  await name.press("Enter");
  await expect(move.getByRole("option", { name: `${dir}/shared` })).toHaveAttribute("aria-selected", "true");
  await expect(move).toContainText(`New path: ${dir}/shared/count.pipeline.yaml`);
  await move.getByRole("button", { name: "Move", exact: true }).click();

  const refs = page.getByRole("dialog", { name: "Update the references?" });
  await expect(refs).toContainText(`count.pipeline.yaml moves to ${dir}/shared/count.pipeline.yaml. 1 file refers to it by its old path:`);
  await expect(refs.getByTestId("references")).toContainText(`${dir}/main.workflow.yaml – action count (core.run-pipeline)`);
  await expect(refs.locator("del")).toHaveText(`${dir}/count.pipeline.yaml`);
  await expect(refs.locator("ins")).toHaveText(`${dir}/shared/count.pipeline.yaml`);
  await refs.getByRole("button", { name: "Move and update 1 reference" }).click();

  await expect(row(page, "shared")).toBeVisible();
  await expect(tree(page).getByRole("treeitem", { name: "shared" }).getByRole("treeitem", { name: "count.pipeline.yaml" })).toBeVisible();
  expect(await text(page, `${dir}/main.workflow.yaml`)).toContain(`{ pipeline: ${dir}/shared/count.pipeline.yaml }`);
});

test("reads triggers in subfolders and renames their target", async ({ page }) => {
  await page.goto("/");
  // The trigger in triggers/zz-organize/ is a trigger file without problems.
  await tree(page).getByRole("treeitem", { name: "triggers" }).getByRole("treeitem", { name: "main.yaml" }).click();
  await expect(page.getByRole("tab", { name: /main\.yaml/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByTestId("kind")).toHaveText("Trigger");
  await expect(page.getByTestId("error-count")).toHaveCount(0);

  // Renaming the workflow offers to change the trigger's target.
  await row(page, "main.workflow.yaml").press("F2");
  const input = tree(page).getByRole("textbox", { name: "New name of main.workflow.yaml" });
  await input.fill("start");
  await input.press("Enter");
  const refs = page.getByRole("dialog", { name: "Update the references?" });
  await expect(refs).toContainText(`main.workflow.yaml is renamed to ${dir}/start.workflow.yaml`);
  await expect(refs.getByTestId("references")).toContainText(`triggers/${dir}/main.yaml – target`);
  await refs.getByRole("button", { name: "Rename and update 1 reference" }).click();
  await expect(row(page, "start.workflow.yaml")).toBeVisible();
  expect(await text(page, `triggers/${dir}/main.yaml`)).toContain(`target: ${dir}/start.workflow.yaml\n`);
  // The open tab of the trigger shows the new text.
  await expect.poll(() => editorText(page)).toContain(`target: ${dir}/start.workflow.yaml`);
});

test("moves a file by dragging it onto a folder", async ({ page }) => {
  await page.goto("/");
  await row(page, "organize-notes.csv").dragTo(row(page, "shared"));
  await expect(tree(page).getByRole("treeitem", { name: "shared" }).getByRole("treeitem", { name: "organize-notes.csv" })).toBeVisible();
  expect(await text(page, `${dir}/shared/organize-notes.csv`)).toBe("a;b\n");
  // Dropping into its own folder changes nothing.
  await row(page, "organize-notes.csv").dragTo(row(page, "count.pipeline.yaml"));
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(tree(page).getByRole("treeitem", { name: "shared" }).getByRole("treeitem", { name: "organize-notes.csv" })).toBeVisible();
});
