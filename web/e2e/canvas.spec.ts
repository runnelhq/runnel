import { expect, test, type Page } from "@playwright/test";

// Read-only canvas (mockup design/mockups/e5-canvas.html) against "runnel serve ../examples/hello".
// Variants of a workflow are served by intercepting the file request; no file is changed.

const parallelPath = "workflows/parallel.workflow.yaml";

async function serveFile(page: Page, path: string, content: string) {
  await page.route(`**/api/v1/files/${path}`, (route) =>
    route.fulfill({ status: 200, contentType: "application/yaml", body: content }),
  );
}

async function openFile(page: Page, name: string) {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
}

test("shows a workflow as a graph", async ({ page }) => {
  await openFile(page, "parallel.workflow.yaml");

  const canvas = page.getByTestId("canvas");
  for (const id of ["start", "fetch-a", "fetch-b", "combine", "report-error"]) {
    await expect(canvas.getByTestId(`node-${id}`)).toBeVisible();
  }
  await expect(canvas.getByTestId("node-fetch-a")).toContainText("core.wait");
  await expect(canvas.locator('[data-kind="success"]')).toHaveCount(4);
  await expect(canvas.locator('[data-kind="failure"]')).toHaveCount(2);
  await expect(canvas.locator('[data-kind="failure"]').first()).toHaveText("✖");
  await expect(canvas.getByTestId("node-report-error")).toContainText("join: first");
  await expect(canvas.getByTestId("node-combine")).not.toContainText("join:");
  await expect(page.getByTestId("file-summary")).toHaveText("Workflow · 5 actions · 6 links");
  await expect(page.getByTestId("zoom")).toHaveText("100 %");
  await expect(page.getByTestId("auto-layout")).toHaveCount(0);

  // Selecting a node marks it; clicking the canvas clears the selection.
  await canvas.getByTestId("node-fetch-a").click();
  await expect(canvas.getByTestId("node-fetch-a")).toHaveClass(/outline-node-selected/);
  await canvas.locator(".react-flow__pane").click({ position: { x: 20, y: 20 } });
  await expect(canvas.getByTestId("node-fetch-a")).not.toHaveClass(/outline-node-selected/);

  // Switching to YAML and back.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toContainText("name: parallel");
  await expect(page.getByTestId("file-summary")).toHaveText(/^YAML · UTF-8 · \d+ lines$/);
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  await expect(canvas.getByTestId("node-start")).toBeVisible();
});

test("other files have no canvas", async ({ page }) => {
  await openFile(page, "dev.yaml");
  await expect(page.getByTestId("code")).toBeVisible();
  await expect(page.getByRole("group", { name: "View" })).toHaveCount(0);
});

test("shows problems at the actions and jumps to the YAML line", async ({ page }) => {
  const res = await page.request.get(`/api/v1/files/${parallelPath}`);
  const broken = (await res.text())
    .replace("type: core.wait\n    with:\n      duration: ${DELAY_B}", "type: core.wiat\n    with:\n      duration: ${DELAY_B}")
    .replace("message: Both branches are done", "message: Done in ${UNKNOWN}");
  await serveFile(page, parallelPath, broken);
  await openFile(page, "parallel.workflow.yaml");

  const canvas = page.getByTestId("canvas");
  await expect(canvas.getByTestId("node-fetch-b")).toHaveAttribute("data-severity", "error");
  await expect(canvas.getByTestId("node-combine")).toHaveAttribute("data-severity", "warning");
  await expect(canvas.getByTestId("node-fetch-a")).not.toHaveAttribute("data-severity");
  await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
  await expect(page.getByTestId("warning-count")).toHaveText("⚠ 1");

  // With the properties panel (E7a), the problems are shown there; the popover at the node
  // is shown when the panel is hidden.
  await canvas.getByTestId("node-fetch-b").click();
  await expect(page.getByRole("region", { name: "Properties" })).toContainText('unknown action type "core.wiat"');
  await expect(page.getByRole("dialog", { name: "Problems of fetch-b" })).toHaveCount(0);
  await page.getByRole("button", { name: "Hide properties" }).click();
  const dialog = page.getByRole("dialog", { name: "Problems of fetch-b" });
  await expect(dialog).toContainText('unknown action type "core.wiat"');
  await dialog.getByRole("button", { name: "Show in YAML ›" }).click();

  // The YAML view (E9) puts the cursor on the line.
  const focused = page.getByTestId("yaml-editor").locator(".cm-activeLine");
  await expect(focused).toBeVisible();
  await expect(focused).toContainText("type: core.wiat");
  await expect(page.getByRole("button", { name: "YAML", exact: true })).toHaveAttribute("aria-pressed", "true");
});

test("explains when a workflow cannot be shown as a graph", async ({ page }) => {
  await serveFile(page, parallelPath, "kind: Workflow\napiVersion: runnel/v1\nname: broken\nactions: [\n");
  await openFile(page, "parallel.workflow.yaml");

  const alert = page.getByRole("alert");
  await expect(alert).toContainText("This workflow can’t be shown as a graph");
  await expect(alert).toContainText("workflows/parallel.workflow.yaml:");
  await expect(page.getByTestId("error-count")).toHaveText("✖ 1");
  await expect(alert).toContainText("Fix them in the YAML view; your changes are kept.");
  await expect(page.getByTestId("not-a-graph")).toBeVisible();
  await alert.getByRole("button", { name: /^Show YAML at line \d+$/ }).click();
  await expect(page.getByTestId("yaml-editor")).toContainText("name: broken");
});

test("arranges a workflow without layout automatically", async ({ page }) => {
  const path = "workflows/collector.workflow.yaml";
  const res = await page.request.get(`/api/v1/files/${path}`);
  const text = await res.text();
  await serveFile(page, path, text.slice(0, text.indexOf("layout:")));
  await openFile(page, "collector.workflow.yaml");

  await expect(page.getByTestId("auto-layout")).toHaveText("Arranged automatically – the file has no layout yet");
  const canvas = page.getByTestId("canvas");
  for (const id of ["start", "check-inbox", "check-archive", "check-network", "checks-done", "report", "done"]) {
    await expect(canvas.getByTestId(`node-${id}`)).toBeVisible();
  }
  // Left to right: the start comes first, the end last.
  const x = async (id: string) => (await canvas.getByTestId(`node-${id}`).boundingBox())!.x;
  expect(await x("start")).toBeLessThan(await x("checks-done"));
  expect(await x("checks-done")).toBeLessThan(await x("done"));
});
