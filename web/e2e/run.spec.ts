import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";

// Running in the editor (mockup design/mockups/e8-run.html), against the server on a temporary
// copy of examples/hello (e2e/serve-copy.mjs), on its own copy of parallel.workflow.yaml.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/run.workflow.yaml";
const original = readFileSync(new URL("../../examples/hello/workflows/parallel.workflow.yaml", import.meta.url), "utf8");

test.beforeEach(async ({ request }) => {
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("run.workflow.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-fetch-b")).toBeVisible();
}

const node = (page: Page, id: string) => page.getByTestId(`node-${id}`);
const runButton = (page: Page) => page.getByRole("button", { name: "Run", exact: true });

/** Starts a run via the dialog with the given parameters. */
async function runWith(page: Page, params: Record<string, string>) {
  const dialog = page.getByRole("dialog", { name: "Run run.workflow.yaml" });
  await expect(dialog).toBeVisible();
  for (const [name, value] of Object.entries(params)) await dialog.getByLabel(name).fill(value);
  await dialog.getByRole("button", { name: "Run" }).click();
  await expect(dialog).toHaveCount(0);
}

test("runs a workflow and shows the status of each action", async ({ page }) => {
  await open(page);
  // The first run of a workflow with parameters asks for the values.
  await runButton(page).click();
  const dialog = page.getByRole("dialog", { name: "Run run.workflow.yaml" });
  await expect(dialog.getByLabel("Environment")).toHaveValue("");
  await expect(dialog.getByLabel("Environment").locator("option:checked")).toHaveText("dev (default)");
  await expect(dialog.getByLabel("DELAY_A")).toHaveAttribute("placeholder", "2s (default)");
  await runWith(page, { DELAY_B: "oops" });

  // While running: Cancel instead of Run, fetch-a runs, fetch-b failed at once.
  await expect(page.getByRole("button", { name: "Cancel" })).toBeVisible();
  await expect(page.getByTestId("run-status")).toContainText("Running (dev)");
  await expect(node(page, "fetch-a")).toHaveAttribute("data-run", "running");
  await expect(node(page, "fetch-b")).toHaveAttribute("data-run", "failed");
  await expect(node(page, "report-error")).toHaveAttribute("data-run", "succeeded");

  // Finished: the failure was handled by the failure link, so the run succeeded.
  await expect(page.getByTestId("run-status")).toContainText("✔ Last run succeeded", { timeout: 10_000 });
  await expect(node(page, "combine")).toHaveAttribute("data-run", "succeeded");
  await expect(node(page, "fetch-a").getByTestId("duration")).toHaveText(/^2\.\d s$/);
  await expect(runButton(page)).toBeVisible();
  // fetch-b → combine (success) was not followed.
  await expect(page.getByTestId("link-3")).toHaveAttribute("data-run", "dead");

  // Moving keeps the status; changing the actions hides it (decision for mockup E8).
  const box = (await node(page, "start").boundingBox())!;
  await page.mouse.move(box.x + 60, box.y + 20);
  await page.mouse.down();
  await page.mouse.move(box.x + 60, box.y + 60, { steps: 5 });
  await page.mouse.up();
  await expect(page.getByTestId("unsaved")).toBeVisible();
  await expect(node(page, "start")).toHaveAttribute("data-run", "succeeded");
  await node(page, "report-error").click();
  await page.keyboard.press("Delete");
  await expect(node(page, "report-error")).toHaveCount(0);
  await expect(node(page, "start")).not.toHaveAttribute("data-run");
});

test("Run uses the remembered values; the arrow changes them", async ({ page }) => {
  await open(page);
  await runButton(page).click();
  await runWith(page, { DELAY_A: "0s", DELAY_B: "oops" });
  await expect(page.getByTestId("run-status")).toContainText("Last run succeeded", { timeout: 10_000 });

  // F5 runs again without the dialog, with the same values.
  await page.keyboard.press("F5");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(node(page, "fetch-b")).toHaveAttribute("data-run", "failed");
  await expect(page.getByTestId("run-status")).toContainText("Last run succeeded", { timeout: 10_000 });

  await page.getByRole("button", { name: "Run with other values" }).click();
  const dialog = page.getByRole("dialog", { name: "Run run.workflow.yaml" });
  await expect(dialog.getByLabel("DELAY_B")).toHaveValue("oops");
  await runWith(page, { DELAY_B: "" });
  await expect(page.getByTestId("run-status")).toContainText("Last run succeeded", { timeout: 10_000 });
  await expect(node(page, "fetch-b")).toHaveAttribute("data-run", "succeeded");
  await expect(node(page, "report-error")).toHaveAttribute("data-run", "skipped");
});

test("cancels a run", async ({ page }) => {
  await open(page);
  await runButton(page).click();
  await runWith(page, { DELAY_A: "30s" });
  await expect(node(page, "fetch-a")).toHaveAttribute("data-run", "running");
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByTestId("run-status")).toContainText("■ Last run cancelled");
  await expect(node(page, "fetch-a")).toHaveAttribute("data-run", "cancelled");
  await expect(node(page, "combine")).toHaveAttribute("data-run", "skipped");
});

test("asks before running with unsaved changes and reports runs that cannot start", async ({ page, request }) => {
  await open(page);
  const box = (await node(page, "combine").boundingBox())!;
  await page.mouse.move(box.x + 60, box.y + 20);
  await page.mouse.down();
  await page.mouse.move(box.x + 60, box.y + 60, { steps: 5 });
  await page.mouse.up();
  await expect(page.getByTestId("unsaved")).toBeVisible();

  await runButton(page).click();
  const ask = page.getByRole("alertdialog", { name: "Save before running?" });
  await ask.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByTestId("unsaved")).toBeVisible();
  await runButton(page).click();
  await ask.getByRole("button", { name: "Save and run" }).click();
  await expect(page.getByTestId("unsaved")).toHaveCount(0);
  const saved = await (await request.get(`/api/v1/files/${path}`)).text();
  expect(saved).not.toEqual(original);
  await runWith(page, {});
  await expect(page.getByTestId("run-status")).toContainText("Last run succeeded", { timeout: 10_000 });

  // A workflow with errors does not start.
  await request.put(`/api/v1/files/${path}`, {
    headers: { "If-Match": "*" },
    data: original.replace("message: Both branches are done", "level: loud"),
  });
  await open(page);
  await page.keyboard.press("F5");
  await expect(page.getByRole("alert").filter({ hasText: "Could not start the run" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Problems" }).getByRole("table")).toBeVisible();
});
