import { expect, test } from "@playwright/test";

// Explorer, tabs and the read-only file view (mockup design/mockups/e4-editor-shell.html)
// against "runnel serve ../examples/hello". The tests do not change any file.

test("the explorer shows the project tree", async ({ page }) => {
  await page.goto("/");
  const tree = page.getByRole("tree", { name: "Project files" });
  for (const name of ["environments", "dev.yaml", "prod.yaml", "workflows", "hello.workflow.yaml", "project.yaml"]) {
    await expect(tree.getByText(name, { exact: true })).toBeVisible();
  }
  // Folders come first and can be collapsed.
  await expect(tree.getByRole("treeitem").first()).toContainText("environments");
  await tree.getByText("environments", { exact: true }).click();
  await expect(tree.getByText("dev.yaml", { exact: true })).toBeHidden();
  await expect(page.getByText("No file open")).toBeVisible();
});

test("opens files in tabs and shows them as highlighted YAML", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("hello.workflow.yaml").click();
  // Workflows open as canvas (E5); this test is about the YAML view.
  await page.getByRole("button", { name: "YAML", exact: true }).click();

  await expect(page.getByRole("tab", { name: /hello\.workflow\.yaml/ })).toHaveAttribute("aria-selected", "true");
  // Workflows have the compact file bar in every view: the path in the tooltip, no kind badge.
  await expect(page.getByTestId("crumbs")).toHaveText("hello.workflow.yaml");
  await expect(page.getByTestId("crumbs")).toHaveAttribute("title", "workflows/hello.workflow.yaml");
  await expect(page.getByTestId("kind")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Copy path" })).toBeVisible();
  // Workflows are edited in the YAML view too (E9): no "Read-only", highlighted as before.
  await expect(page.getByText("Read-only")).toHaveCount(0);
  const code = page.getByTestId("yaml-editor");
  await expect(code).toContainText("apiVersion: runnel/v1");
  await expect(code.locator(".ry-key", { hasText: "apiVersion" })).toBeVisible();
  await expect(code.locator(".ry-comment").first()).toContainText("# A small, valid workflow");
  await expect(code.locator(".ry-var", { hasText: "${GREETING}" }).first()).toBeVisible();
  await expect(page.getByTestId("file-summary")).toHaveText(/^YAML · UTF-8 · \d+ lines$/);

  // A second file opens a second tab; the explorer marks the active file.
  await page.getByRole("tree").getByText("dev.yaml").click();
  await expect(page.getByRole("tab")).toHaveCount(2);
  await expect(page.getByTestId("crumbs")).toHaveText("environments / dev.yaml");
  await expect(page.getByTestId("kind")).toHaveText("Environment");
  // Environments open as a form (E20b) and are edited in the YAML view as well (E10).
  await expect(page.getByText("Read-only")).toHaveCount(0);
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toContainText("kind: Environment");
  await expect(page.getByRole("treeitem", { name: "dev.yaml" })).toHaveAttribute("aria-selected", "true");

  // Opening an open file again only switches tabs.
  await page.getByRole("tab", { name: /hello\.workflow\.yaml/ }).click();
  await page.getByRole("tree").getByText("hello.workflow.yaml").click();
  await expect(page.getByRole("tab")).toHaveCount(2);

  // Closing the active tab activates its neighbour; closing the last shows the empty state.
  await page.getByRole("button", { name: "Close hello.workflow.yaml" }).click();
  await expect(page.getByRole("tab")).toHaveCount(1);
  await expect(page.getByTestId("crumbs")).toHaveText("environments / dev.yaml");
  await page.getByRole("button", { name: "Close dev.yaml" }).click();
  await expect(page.getByText("No file open")).toBeVisible();
  await expect(page.getByTestId("file-summary")).toHaveCount(0);
});

test("copies the full file path", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/");
  await page.getByRole("tree").getByText("project.yaml").click();
  await page.getByRole("button", { name: "Copy path" }).click();
  await expect(page.getByRole("button", { name: "✔ Copied" })).toBeVisible();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied).toMatch(/examples[\\/]hello[\\/]project\.yaml$/);
});

test("reports a file that no longer exists", async ({ page }) => {
  await page.goto("/");
  await page.route("**/api/v1/files/workflows/collector.workflow.yaml", (route) =>
    route.fulfill({ status: 404, contentType: "application/json", body: '{"error":"file not found"}' }),
  );
  await page.getByRole("tree").getByText("collector.workflow.yaml").click();
  const alert = page.getByRole("alert");
  await expect(alert).toContainText("File not found");
  await expect(alert).toContainText("workflows/collector.workflow.yaml no longer exists");
  await expect(alert.getByRole("button", { name: "Reload explorer" })).toBeVisible();
});

test("shows files saved as UTF-16 (Notepad \"Unicode\")", async ({ page }) => {
  const text = "kind: Environment\nname: dev\n# Grüße\n";
  const body = Buffer.concat([Buffer.from([0xff, 0xfe]), Buffer.from(text, "utf16le")]);
  await page.route("**/api/v1/files/environments/dev.yaml", (route) =>
    route.fulfill({ status: 200, contentType: "application/yaml", body }),
  );
  await page.goto("/");
  await page.getByRole("tree").getByText("dev.yaml").click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toContainText("# Grüße");
  await expect(page.getByTestId("file-summary")).toHaveText("YAML · UTF-16 LE · 3 lines");
});

test("works with files while the connection is lost", async ({ page }) => {
  // Without the event stream the editor shows the banner, but files can still be read.
  await page.route("**/api/v1/events", (route) => route.abort());
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText("Connection to the Runnel server lost.");
  await page.getByRole("tree").getByText("hello.workflow.yaml").click();
  await expect(page.getByTestId("node-greet")).toBeVisible();
  await expect(page.getByTestId("connection")).toHaveText("⚠ Disconnected");
});
