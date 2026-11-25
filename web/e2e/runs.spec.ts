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
  // The runs of the earlier tests, from the API: counting the rows at once could be too
  // early, before the list has loaded them. Then wait until the list shows them.
  const list = await request.get(`/api/v1/runs?${new URLSearchParams({ path, limit: "200" })}`);
  const before = ((await list.json()) as { runs?: unknown[] }).runs?.length ?? 0;
  await expect(rows).toHaveCount(before);

  const res = await request.post("/api/v1/runs", { data: { path, params: { WAIT: "3s" } } });
  expect(res.status()).toBe(201);
  // The run appears while running, the header counts it, and it ends without reloading.
  await expect(rows).toHaveCount(before + 1);
  await expect(rows.nth(0)).toContainText("Running");
  await expect(page.getByRole("button", { name: /^Runs/ })).toContainText("1");
  await expect(rows.nth(0)).toContainText("Succeeded", { timeout: 10_000 });
});

test("opens a run of the history on the canvas, with older and newer runs", async ({ page, request }) => {
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: workflow });
  const start = async (params: Record<string, string>) => {
    const res = await request.post("/api/v1/runs", { data: { path, environment: "prod", params } });
    const id = ((await res.json()) as { id: string }).id;
    await expect.poll(async () => ((await (await request.get(`/api/v1/runs/${id}`)).json()) as { status: string }).status).not.toBe("running");
    return id;
  };
  const ok = await start({});
  const failed = await start({ WAIT: "oops" });
  // The file changes after the runs: "wait" becomes "pause".
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: workflow.replaceAll("wait", "pause").replace("type: core.pause", "type: core.wait") });

  await page.goto("/");
  await page.getByRole("button", { name: /^Runs/ }).click();
  const view = page.getByTestId("runs-view");
  await view.getByRole("combobox", { name: "File" }).selectOption(path);
  await view.getByRole("row").filter({ hasText: "Failed" }).first().click();
  await page.getByTestId("run-details").getByRole("button", { name: "Open on canvas" }).click();

  const info = page.getByTestId("run-info");
  await expect(page.getByRole("tab", { name: /runs-list\.workflow\.yaml/ })).toHaveAttribute("aria-selected", "true");
  await expect(info).toContainText("Failed");
  await expect(info).toContainText("Editor");
  await expect(info).toContainText("prod · 1 parameter");
  await expect(page.getByTestId("run-status")).toContainText(`Showing run ${failed.slice(0, 12)} · failed`);
  await expect(page.getByTestId("node-start")).toHaveAttribute("data-run", "succeeded");
  // The file changed: the line names what differs; the new action has no status.
  const changed = page.getByTestId("run-changed");
  await expect(changed).toContainText("The file was changed after this run");
  await expect(changed).toContainText("not in the run: pause");
  await expect(changed).toContainText("no longer in the file: wait (failed 0 s)");
  await expect(page.getByTestId("node-pause")).not.toHaveAttribute("data-run", /./);
  // The log of the stored run.
  await expect(page.getByRole("log", { name: "Run log" })).toBeVisible();

  // Older run of the file; there is none newer than the failed one.
  await expect(info.getByRole("button", { name: "Newer run" })).toBeDisabled();
  await info.getByRole("button", { name: "Older run" }).click();
  await expect(page.getByTestId("run-status")).toContainText(`Showing run ${ok.slice(0, 12)} · succeeded`);
  await expect(info.getByRole("button", { name: "Newer run" })).toBeEnabled();

  // All runs of this file: the list with the file filter.
  await info.getByRole("button", { name: "All runs of this file" }).click();
  await expect(view.getByRole("combobox", { name: "File" })).toHaveValue(path);

  // × hides the run again.
  await page.getByRole("tab", { name: /runs-list\.workflow\.yaml/ }).click();
  await info.getByRole("button", { name: "Hide the run" }).click();
  await expect(info).toHaveCount(0);
  await expect(page.getByTestId("node-start")).not.toHaveAttribute("data-run", /./);
});

// The open list of a filter takes the colours of a popover, also in the dark scheme
// (the select itself is transparent).
test("the lists of the filters are readable in the dark scheme", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Dark", exact: true }).click();
  await page.getByRole("button", { name: /^Runs/ }).click();
  const option = page.getByTestId("runs-view").getByRole("combobox", { name: "File" }).locator("option").first();
  const colours = await option.evaluate((o) => {
    const s = getComputedStyle(o);
    return [s.backgroundColor, s.color];
  });
  const expected = await page.evaluate(() => {
    const probe = document.createElement("span");
    probe.style.backgroundColor = "var(--popover)";
    probe.style.color = "var(--popover-foreground)";
    document.body.append(probe);
    const s = getComputedStyle(probe);
    const out = [s.backgroundColor, s.color];
    probe.remove();
    return out;
  });
  expect(colours).toEqual(expected);
  expect(colours[0]).not.toEqual(colours[1]);
});
