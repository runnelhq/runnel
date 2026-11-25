import { expect, test, type Page } from "@playwright/test";

// Folders of the explorer: the top level starts open, deeper folders closed; what the
// user opens or closes is kept per project in the browser; "Collapse all" closes every
// folder. Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });

const dir = "zz-explorer";
const files = [`${dir}/deep/inner.workflow.yaml`, `${dir}/top.workflow.yaml`];
const content = "kind: Workflow\napiVersion: runnel/v1\nname: x\nactions:\n  - { id: start, type: core.start }\n";

const tree = (page: Page) => page.getByRole("tree", { name: "Project files" });
const item = (page: Page, name: string) => tree(page).getByRole("treeitem", { name, exact: true });

test.beforeAll(async ({ request }) => {
  for (const path of files) {
    expect((await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: content })).ok()).toBe(true);
  }
});

test.afterAll(async ({ request }) => {
  for (const path of files) await request.delete(`/api/v1/files/${path}`, { headers: { "If-Match": "*" } });
  for (const f of [`${dir}/deep`, dir]) await request.delete(`/api/v1/folders/${f}`);
});

test("folders start closed below the top level, are kept and close all at once", async ({ page }) => {
  await page.goto("/");
  await expect(item(page, dir)).toHaveAttribute("aria-expanded", "true");
  await expect(item(page, "top.workflow.yaml")).toBeVisible();
  await expect(item(page, "deep")).toHaveAttribute("aria-expanded", "false");
  await expect(item(page, "inner.workflow.yaml")).toHaveCount(0);

  // Opened by hand: still open after reloading the page.
  await item(page, "deep").locator("> div").click();
  await expect(item(page, "inner.workflow.yaml")).toBeVisible();
  await page.reload();
  await expect(item(page, "inner.workflow.yaml")).toBeVisible();

  // Collapse all: only the top level is visible, also after reloading.
  await page.getByRole("button", { name: "Collapse all" }).click();
  await expect(item(page, dir)).toHaveAttribute("aria-expanded", "false");
  await expect(item(page, "workflows")).toHaveAttribute("aria-expanded", "false");
  await expect(item(page, "top.workflow.yaml")).toHaveCount(0);
  await page.reload();
  await expect(item(page, dir)).toHaveAttribute("aria-expanded", "false");
  await item(page, dir).locator("> div").click();
  await expect(item(page, "deep")).toHaveAttribute("aria-expanded", "false");
});
