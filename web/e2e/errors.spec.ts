import { expect, test } from "@playwright/test";

// Errors while drawing the editor stay in their part (fix after the empty page with
// project.yaml in examples/expressions): a file shows a message in its tab, the sidebar
// in its panel, the rest stays usable. The errors come from broken answers of the API.

test("an error in a file stays in its tab", async ({ page }) => {
  // Only the check of hello.workflow.yaml answers broken; other files are checked normally.
  await page.route("**/api/v1/validate", (route) =>
    route.request().postData()?.includes("hello.workflow.yaml") ? route.fulfill({ json: { problems: 5 } }) : route.continue(),
  );
  await page.goto("/");
  await page.getByRole("tree").getByText("hello.workflow.yaml", { exact: true }).click();
  const error = page.getByTestId("file-error");
  await expect(error).toContainText("This file can’t be shown");
  await expect(error).toContainText("workflows/hello.workflow.yaml");
  // The rest of the editor works: another file opens and shows its text.
  await page.getByRole("tree").getByText("dev.yaml", { exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
  await expect(page.getByTestId("file-error")).toHaveCount(0);
  // Back on the broken tab, "Try again" draws it anew once the cause is gone.
  await page.unroute("**/api/v1/validate");
  await page.getByRole("tab", { name: /hello\.workflow\.yaml/ }).click();
  await error.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByTestId("file-error")).toHaveCount(0);
  await expect(page.getByTestId("crumbs")).toContainText("hello.workflow.yaml");
});

test("an error in the sidebar stays in its panel", async ({ page }) => {
  await page.route("**/api/v1/schemas/project", (route) =>
    route.fulfill({ json: { type: "object", properties: { settings: { type: "object", description: 5 } } } }),
  );
  await page.goto("/");
  await page.getByRole("tree").getByText("project.yaml", { exact: true }).click();
  const panel = page.getByRole("alert", { name: "Error" });
  await expect(panel).toContainText("This panel can’t be shown");
  await expect(panel.getByRole("button", { name: "Try again" })).toBeVisible();
  // The file itself is shown and the explorer still works.
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
  await page.getByRole("tree").getByText("hello.workflow.yaml", { exact: true }).click();
  await expect(page.getByRole("region", { name: "Palette" })).toBeVisible();
});

test("an error outside a file or panel offers to reload the page", async ({ page }) => {
  await page.route("**/api/v1/variables", (route) => route.fulfill({ json: { environments: [], variables: 5 } }));
  await page.goto("/");
  await page.getByRole("tree").getByText("project.yaml", { exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("The editor stopped because of an error");
  await page.unroute("**/api/v1/variables");
  await page.getByRole("button", { name: "Reload page" }).click();
  await expect(page.getByRole("tree")).toBeVisible();
});
