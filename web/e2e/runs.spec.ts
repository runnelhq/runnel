import { expect, test } from "@playwright/test";

// The list of runs (mockup design/mockups/s26d-runs.html, S26e1): runs from the run history
// with filters and the details of a run. Against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs) with its own workflow, so that the runs of other
// tests do not get in the way (the list is filtered by its file).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/runs-list.workflow.yaml";
const workflow = `kind: Workflow
apiVersion: runnel/v1
name: runs-list
parameters:
  - name: WAIT
    default: 0s
actions:
  - id: start
    type: core.start
  - id: wait
    type: core.wait
    with:
      duration: \${WAIT}
links:
  - { from: start, to: wait }
`;

test("lists the runs of the project with filters and details", async ({ page, request }) => {
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: workflow });
  const start = async (params: Record<string, string>) => {
    const res = await request.post("/api/v1/runs", { data: { path, environment: "prod", params } });
    expect(res.status()).toBe(201);
    return ((await res.json()) as { id: string }).id;
  };
  const ok = await start({});
  const failed = await start({ WAIT: "oops" });

  await page.goto("/");
  await page.getByRole("button", { name: /^Runs/ }).click();
  await expect(page.getByRole("tab", { name: /Runs/ })).toHaveAttribute("aria-selected", "true");
  const view = page.getByTestId("runs-view");
  await view.getByRole("combobox", { name: "File" }).selectOption(path);
  const rows = view.getByRole("table", { name: "Runs" }).getByRole("row").filter({ hasText: "runs-list.workflow.yaml" });
  await expect(rows).toHaveCount(2);
  // Newest first, grouped by day; started with POST /runs: trigger "Editor".
  await expect(rows.nth(0)).toContainText("Failed");
  await expect(rows.nth(1)).toContainText("Succeeded");
  await expect(rows.nth(0)).toContainText("Editor");
  await expect(rows.nth(0)).toContainText("prod");
  await expect(view.getByRole("row").filter({ hasText: /^Today · / })).toHaveCount(1);
  await expect(page.getByTestId("file-summary")).toHaveText("2 runs of runs-list.workflow.yaml");

  // Filters: status, trigger; "Clear filters" shows all again.
  await view.getByRole("combobox", { name: "Status" }).selectOption("failed");
  await expect(rows).toHaveCount(1);
  await view.getByRole("combobox", { name: "Status" }).selectOption("succeeded");
  await view.getByRole("combobox", { name: "Trigger" }).selectOption("cli");
  await expect(view.getByText("No runs match the filters")).toBeVisible();
  await view.getByRole("button", { name: "Clear filters" }).first().click();
  await expect(view.getByRole("combobox", { name: "File" })).toHaveValue("");
  await view.getByRole("combobox", { name: "File" }).selectOption(path);
  await view.getByRole("combobox", { name: "Trigger" }).selectOption("editor");
  await expect(rows).toHaveCount(2);

  // Details of the failed run: error, parameters, actions, log; the file opens from there.
  await rows.nth(0).click();
  const details = page.getByTestId("run-details");
  await expect(details).toContainText(failed);
  await expect(details.getByRole("alert")).toContainText('invalid duration "oops"');
  await expect(details).toContainText("WAIT = oops");
  const actions = details.getByRole("region", { name: "Actions" });
  await expect(actions).toContainText("2 · 1 failed");
  await expect(actions).toContainText("wait core.wait");
  await rows.nth(1).click();
  await expect(details).toContainText(ok);
  await expect(details.getByRole("alert")).toHaveCount(0);
  await details.getByRole("button", { name: path }).click();
  await expect(page.getByRole("tab", { name: /runs-list\.workflow\.yaml/ })).toHaveAttribute("aria-selected", "true");
});

test("shows new runs without reloading and counts running ones in the header", async ({ page, request }) => {
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: workflow });
  await page.goto("/");
  await page.getByRole("button", { name: /^Runs/ }).click();
  const view = page.getByTestId("runs-view");
  await view.getByRole("combobox", { name: "File" }).selectOption(path);
  const rows = view.getByRole("table", { name: "Runs" }).getByRole("row").filter({ hasText: "runs-list.workflow.yaml" });
  const before = await rows.count();

  const res = await request.post("/api/v1/runs", { data: { path, params: { WAIT: "3s" } } });
  expect(res.status()).toBe(201);
  // The run appears while running, the header counts it, and it ends without reloading.
  await expect(rows).toHaveCount(before + 1);
  await expect(rows.nth(0)).toContainText("Running");
  await expect(page.getByRole("button", { name: /^Runs/ })).toContainText("1");
  await expect(rows.nth(0)).toContainText("Succeeded", { timeout: 10_000 });
});
