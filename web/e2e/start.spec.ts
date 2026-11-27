import { expect, test } from "@playwright/test";

// The start page in a folder without project (R10): "runnel serve" in an empty folder
// "My Flows" (e2e/serve-empty.mjs). The second test creates the project, so they run in order.
test.use({ baseURL: "http://127.0.0.1:7396" });
test.describe.configure({ mode: "serial" });

test("shows the start page in light and dark", async ({ page }) => {
  await page.goto("/");
  const start = page.getByTestId("start-page");
  await expect(start.getByRole("heading", { name: "No project here" })).toBeVisible();
  await expect(page.getByTestId("start-folder")).toContainText("My Flows");
  await expect(page.getByLabel("Project name")).toHaveValue("my-flows");
  await expect(start.getByRole("img", { name: "Runnel" }).first()).toBeVisible();
  await expect(page.getByRole("checkbox")).toHaveCount(0);

  await page.emulateMedia({ colorScheme: "dark" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  // In dark mode the inverse logo is shown instead.
  await expect(start.locator("img:visible")).toHaveCount(1);
  await expect(start.locator('img[src*="inverse"]')).toBeVisible();
});

test("creates the project and opens the example workflow", async ({ page }) => {
  await page.goto("/");
  const name = page.getByLabel("Project name");
  await name.fill("my flows");
  await page.getByRole("button", { name: "Create project" }).click();
  await expect(page.getByRole("alert")).toContainText("invalid project name");
  await expect(name).toHaveAttribute("aria-invalid", "true");

  await name.fill("flows");
  await page.getByRole("button", { name: "Create project" }).click();
  await expect(page.getByTestId("project")).toContainText("flows");
  const tree = page.getByRole("tree");
  await expect(tree.getByText("hello.workflow.yaml", { exact: true })).toBeVisible();
  await expect(tree.getByText("project.yaml", { exact: true })).toBeVisible();
  await expect(page.getByTestId("node-greet")).toBeVisible();

  // Reloading shows the project, not the start page.
  await page.reload();
  await expect(tree.getByText("workflows", { exact: true })).toBeVisible();
  await expect(page.getByTestId("start-page")).toHaveCount(0);
});
