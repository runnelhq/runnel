import { expect, test, type Page } from "@playwright/test";

// The data of each step after a run (E23b, mockup design/mockups/e23-step-data.html): the tab
// "Data" with input, output as tree, table and JSON, the path of a value, entries of forEach,
// failed and skipped steps, the empty state, and (mocked) runs without data and cut output.
// Against a copy of examples/hello with a key for the local secret store (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7394", permissions: ["clipboard-read", "clipboard-write"] });
test.describe.configure({ mode: "serial" });

const path = "workflows/data.workflow.yaml";
const workflow = `kind: Workflow
apiVersion: runnel/v1
name: data
actions:
  - id: start
    type: core.start
  - id: orders
    type: core.script
    with:
      params:
        key: \${secret:e2e-data-key}
        stage: \${STAGE}
      script: |
        return {
          count: 3,
          echo: "key " + params.key,
          items: [
            { id: 1, name: "Anna", "unit price": 9.5, customer: { city: "Bonn" } },
            { id: 2, name: "Ben", "unit price": 2, customer: { city: "Köln" } },
            { id: 3, name: "Cleo", "unit price": 4.25, customer: { city: "Ulm" } },
          ],
        };
  - id: query
    type: db.query
    with:
      connection: e2e-data-db
      sql: SELECT 1 AS one
  - id: each
    type: core.script
    forEach: "{{ results.orders.output.items }}"
    with:
      params:
        id: "{{ row.id }}"
      script: |
        if (Number(params.id) === 3) throw new Error("entry 3 is broken");
        return { doubled: Number(params.id) * 2 };
  - id: after
    type: core.log
    with:
      message: done
links:
  - { from: start, to: orders }
  - { from: orders, to: query }
  - { from: query, to: each }
  - { from: each, to: after }
layout:
  start: { x: 40, y: 80 }
  orders: { x: 260, y: 80 }
  query: { x: 480, y: 80 }
  each: { x: 700, y: 80 }
  after: { x: 920, y: 80 }
`;

test.beforeAll(async ({ request }) => {
  const files: Record<string, string> = {
    [path]: workflow,
    "connections/e2e-data-db.yaml": "kind: Connection\napiVersion: runnel/v1\nname: e2e-data-db\ntype: sqlite\nwith:\n  path: data/e2e.db\n",
  };
  for (const [p, data] of Object.entries(files)) {
    expect((await request.put(`/api/v1/files/${p}`, { headers: { "If-Match": "*" }, data })).ok()).toBe(true);
  }
  expect((await request.put("/api/v1/secrets/e2e-data-key", { data: { value: "s3cr3t-value" } })).ok()).toBe(true);
});

const node = (page: Page, id: string) => page.getByTestId(`node-${id}`);
const dataTab = (page: Page) => page.getByRole("button", { name: /^Data( !)?$/ });
const data = (page: Page) => page.getByTestId("data");
const clipboard = (page: Page) => page.evaluate(() => navigator.clipboard.readText());

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("data.workflow.yaml", { exact: true }).click();
  await expect(node(page, "orders")).toBeVisible();
}

/** Runs the workflow with dev and waits for its end (it fails at entry 3 of each); the first run of a page asks for the environment. */
async function run(page: Page, first = true) {
  await page.getByRole("button", { name: "Run", exact: true }).click();
  if (first) await page.getByRole("dialog", { name: "Run data.workflow.yaml" }).getByRole("button", { name: "Run" }).click();
  await expect(page.getByTestId("log-status")).toContainText("✖ Failed", { timeout: 10_000 });
}

test("shows the empty state before the first run", async ({ page }) => {
  await open(page);
  await node(page, "orders").click();
  await dataTab(page).click();
  await expect(data(page)).toContainText("No run yet");
  await expect(data(page)).toContainText("Run the workflow (F5) to see the data of each step");
  await expect(data(page).getByRole("button", { name: /Run/ })).toBeVisible();
  await expect(page.getByTestId("data-sign-orders")).toHaveCount(0);
});

test("shows input and output of a step as tree, table and JSON with the path of a value", async ({ page }) => {
  await open(page);
  await run(page);
  // Nodes with data carry the sign; the skipped step has none.
  for (const id of ["orders", "query", "each"]) await expect(page.getByTestId(`data-sign-${id}`)).toBeVisible();
  await expect(page.getByTestId("data-sign-after")).toHaveCount(0);
  await expect(page.getByTestId("for-each")).toContainText("for each · 3");

  await page.getByTestId("data-sign-orders").click();
  await expect(data(page)).toBeVisible();
  const head = page.getByTestId("data-head");
  await expect(head).toContainText("orders");
  await expect(head).toContainText("core.script");
  await expect(page.getByTestId("data-status")).toHaveText("✔ Succeeded");
  await expect(head).toContainText(/Run\s?[0-9A-Z]{12} · dev/);

  // Input: the secret as reference, never its value; the source of a variable.
  const input = page.getByTestId("data-input");
  await expect(input.getByTestId("input-params.key")).toContainText("***");
  await expect(input.getByTestId("input-params.key")).toContainText("${secret:e2e-data-key}");
  await expect(input.getByTestId("input-params.stage")).toContainText('"development"');
  await expect(input.getByTestId("input-params.stage")).toContainText("from ${STAGE}");
  await expect(data(page)).not.toContainText("s3cr3t-value");

  // Output as tree: the secret masked, lists with their length; a click shows the path.
  const output = page.getByTestId("data-output");
  await data(page).getByRole("button", { name: "Tree" }).click();
  await expect(output.getByRole("treeitem", { name: /echo/ })).toContainText('"key ***"');
  const items = output.getByRole("treeitem", { name: /^items/ });
  await expect(items).toContainText("3 entries");
  await items.click();
  await expect(page.getByTestId("data-path")).toHaveText("results.orders.output.items");
  await output.getByRole("treeitem").filter({ hasText: "[0]" }).click();
  await output.getByRole("treeitem").filter({ hasText: '"Anna"' }).click();
  await expect(page.getByTestId("data-path")).toHaveText("results.orders.output.items[0].name");
  await data(page).getByRole("button", { name: "Copy path" }).click();
  await expect.poll(() => clipboard(page)).toBe("results.orders.output.items[0].name");
  await data(page).getByRole("button", { name: "{{ }}" }).click();
  await expect.poll(() => clipboard(page)).toBe("{{ results.orders.output.items[0].name }}");

  // Table: the list of objects with its columns (keys sorted as the server keeps them); a key that is no name in brackets.
  await data(page).getByRole("button", { name: "Table" }).click();
  const table = page.getByRole("table", { name: "Output as table" });
  await expect(table.getByRole("columnheader")).toHaveText(["#", "customerobject", "idinteger", "namestring", "unit pricenumber"]);
  await table.getByRole("cell", { name: "4.25" }).click();
  await expect(page.getByTestId("data-path")).toHaveText('results.orders.output.items[2]["unit price"]');
  await expect(table.getByRole("cell", { name: "{ … }" }).first()).toHaveAttribute("title", '{"city":"Bonn"}');

  // JSON and Copy.
  await data(page).getByRole("button", { name: "JSON" }).click();
  await expect(page.getByTestId("data-json")).toContainText('"name": "Ben"');
  await data(page).getByRole("button", { name: "Copy", exact: true }).click();
  await expect.poll(() => clipboard(page)).toContain('"unit price": 4.25');
  await data(page).getByRole("button", { name: "Tree" }).click();

  // The log of the step, collapsed; the connection of a database step, resolved.
  await expect(page.getByTestId("data-log")).toContainText("Log of this step");
  await node(page, "query").click();
  await expect(head).toContainText("db.query");
  await expect(input.getByTestId("input-connection")).toContainText('"e2e-data-db"');
  const conn = input.getByRole("button", { name: /Connection e2e-data-db/ });
  await expect(conn).toContainText("sqlite · as resolved for dev");
  await conn.click();
  await expect(input.getByTestId("input-path")).toContainText('"data/e2e.db"');
  await data(page).getByRole("button", { name: /^\+ \d+ defaults?$/ }).click();
  await expect(input.getByTestId("input-maxRows")).toContainText("default");
});

test("shows the entries of forEach, a failed entry and a skipped step", async ({ page }) => {
  await open(page);
  await run(page);
  // The properties panel opens the data of the selected step.
  await node(page, "each").click();
  await page.getByTestId("last-run").getByRole("button", { name: "Show data" }).click();
  await expect(dataTab(page)).toContainText("!");
  await expect(page.getByTestId("data-status")).toHaveText("✖ Failed");
  await expect(page.getByTestId("data-head")).toContainText("entry 3 of 3 failed");
  await expect(page.getByTestId("entries-summary")).toHaveText("2 succeeded · 1 failed");

  // The failed entry is shown at once: the error instead of the output, the log open.
  await expect(page.getByRole("combobox", { name: "Entry" })).toHaveValue("3");
  const error = page.getByTestId("data-error");
  await expect(error).toContainText("entry 3 is broken");
  await expect(error).toContainText("results.each.error");
  await expect(error).toContainText("results.each.output.items[2].error");
  await expect(page.getByTestId("data-input").getByRole("button", { name: /^row/ })).toBeVisible();
  await expect(page.getByTestId("input-params.id")).toContainText("3");
  await expect(page.getByTestId("data-log")).toContainText("entry 3 is broken");

  // Another entry: its output as in results.each.output.items[0].
  await data(page).getByRole("button", { name: "Entry 1", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Entry" })).toHaveValue("1");
  const output = page.getByTestId("data-output");
  await output.getByRole("treeitem").filter({ hasText: "doubled" }).click();
  await expect(page.getByTestId("data-path")).toHaveText("results.each.output.items[0].output.doubled");
  await data(page).getByRole("button", { name: "Next entry" }).click();
  await expect(page.getByRole("combobox", { name: "Entry" })).toHaveValue("2");
  await expect(page.getByTestId("input-params.id")).toContainText("2");

  // All entries: the list of the entries; no input of its own.
  await data(page).getByRole("button", { name: "All entries" }).click();
  await expect(page.getByRole("combobox", { name: "Entry" })).toHaveCount(0);
  await expect(page.getByTestId("data-input")).toContainText("Each entry has an input of its own");

  // A skipped step: why, with its incoming links and the status of their source.
  await node(page, "after").click();
  await expect(page.getByTestId("data-status")).toHaveText("– Skipped");
  await expect(data(page)).toContainText("Not run: no incoming link applies");
  await expect(page.getByTestId("skipped-link")).toContainText("each → after");
  await expect(page.getByTestId("skipped-link")).toContainText("On success · each failed");
  await data(page).getByRole("button", { name: "orders" }).click();
  await expect(page.getByTestId("data-head")).toContainText("core.script");
  await expect(page.getByTestId("data-status")).toHaveText("✔ Succeeded");
});

test("shows runs without data and output cut to the limit", async ({ page }) => {
  await open(page);
  // The output of orders as the server sends it when it was larger than 1 MB.
  await page.route("**/api/v1/runs/*/steps/orders", async (route) => {
    const response = await route.fetch();
    const json = await response.json();
    json.truncated = [{ part: "output", path: "items", kind: "list", length: 8200, kept: 3 }];
    await route.fulfill({ response, json });
  });
  // The data of query as the server sends it when the run had reached its limit of 20 MB.
  await page.route("**/api/v1/runs/*/steps/query", async (route) => {
    const response = await route.fetch();
    const json = await response.json();
    delete json.input;
    delete json.output;
    delete json.connection;
    json.dropped = true;
    await route.fulfill({ response, json });
  });
  await run(page);
  await page.getByTestId("data-sign-query").click();
  await expect(page.getByTestId("data-head")).toContainText("db.query");
  await expect(data(page).getByRole("note")).toContainText("the run reached its limit of 20 MB for the data of its steps");
  await expect(page.getByTestId("data-input")).toContainText("Not kept: the run reached its limit of 20 MB.");
  await expect(page.getByTestId("data-output")).toContainText("No output.");
  await page.getByTestId("data-sign-orders").click();
  await expect(data(page).getByRole("note")).toContainText("Output larger than 1 MB – shown up to 1 MB.");
  await expect(data(page).getByRole("note")).toContainText("the first 3 of 8,200 entries of items");
  await data(page).getByRole("button", { name: "Tree" }).click();
  await expect(page.getByTestId("data-output").getByRole("treeitem", { name: /^items/ })).toContainText("8,200 entries · first 3 kept");

  // A run whose data the server did not keep (e.g. after a restart or another run).
  await page.route("**/api/v1/runs/*/steps", (route) => route.fulfill({ status: 404, json: { error: "no data kept for this run" } }));
  await run(page, false);
  await node(page, "orders").click();
  await dataTab(page).click();
  await expect(data(page)).toContainText("No data kept for this run");
  await expect(page.getByTestId("data-sign-orders")).toHaveCount(0);
  await data(page).getByRole("button", { name: "Show in Log" }).click();
  await expect(page.getByRole("log", { name: "Run log" })).toBeVisible();
});
