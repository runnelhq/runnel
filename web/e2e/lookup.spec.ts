import { expect, test, type Page } from "@playwright/test";

// Info links and Stream Lookup in the editor (mockup design/mockups/s23b-info-link.html,
// states canvas, link, run and problems). Against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs), like pipeline.spec.ts.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "pipelines/lookup.pipeline.yaml";
const fileURL = `/api/v1/files/${path}`;
// Orders get the name of their customer; customers 1 to 3 exist, order 4 has none.
const withInfo = `kind: Pipeline
apiVersion: runnel/v1
name: lookup
transforms:
  - id: customers
    type: core.generate-rows
    with: { rows: 3, rowNumberField: id, fields: [ { name: name, value: Ada } ] }
  - id: orders
    type: core.generate-rows
    with: { rows: 4, rowNumberField: customerId }
  - id: lookup
    type: core.stream-lookup
    with:
      keys:
        - { field: customerId, lookupField: id }
      return:
        - { lookupField: name, as: customer, default: unknown }
  - id: write
    type: core.log
links:
  - { from: customers, to: lookup, type: info }
  - { from: orders, to: lookup }
  - { from: lookup, to: write }
layout:
  customers: { x: 40, y: 40 }
  orders: { x: 40, y: 250 }
  lookup: { x: 330, y: 250 }
  write: { x: 620, y: 250 }
`;
const withoutInfo = withInfo.replace("  - { from: customers, to: lookup, type: info }\n", "");

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });

async function open(page: Page, request: import("@playwright/test").APIRequestContext, text: string) {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: text });
  await page.goto("/");
  await page.getByRole("tree").getByText("lookup.pipeline.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-lookup")).toBeVisible();
}

test.afterAll(async ({ request }) => {
  await request.delete(fileURL, { headers: { "If-Match": "*" } });
});

test("shows the info link and the lookup data in the panel", async ({ page, request }) => {
  await open(page, request, withInfo);
  // Dotted, with the symbol i, into the port at the top of the lookup.
  await expect(page.getByTestId("link-0")).toHaveAttribute("data-kind", "info");
  await expect(page.getByTestId("link-0")).toHaveText("i");
  await expect(page.getByTestId("info-port-lookup")).toBeVisible();
  await expect(page.getByTestId("info-port-lookup")).not.toHaveClass(/missing/);
  // Only transforms that take lookup data have the port.
  await expect(page.getByTestId("info-port-write")).toHaveCount(0);

  await page.getByTestId("node-lookup").click();
  await expect(panel(page).getByTestId("lookup-source")).toContainText("from customers · info link");
  await expect(panel(page).locator('[data-field="with.maxRecords"]')).toBeVisible();
  // The fields of the lookup data apart from those of the data link.
  await expect(panel(page).getByTestId("info-fields")).toContainText("id");
  await expect(panel(page).getByTestId("info-fields")).toContainText("name");

  // The link menu offers Info for the lookup; the panel of the link explains it.
  await page.getByTestId("link-0").click();
  const menu = page.getByRole("menu", { name: "Link" });
  await expect(menu.getByRole("menuitemradio", { name: /Info/ })).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Escape");
  await page.getByTestId("link-1").click();
  await expect(page.getByRole("menu", { name: "Link" }).getByRole("menuitemradio", { name: /Info/ })).toBeVisible();
  await page.keyboard.press("Escape");
  // A link to a transform that takes no lookup data has no Info.
  await page.getByTestId("link-2").click();
  await expect(page.getByRole("menu", { name: "Link" }).getByRole("menuitemradio")).toHaveCount(2);
});

test("a lookup without info link is marked; a link dropped on its top becomes one", async ({ page, request }) => {
  await open(page, request, withoutInfo);
  await expect(page.getByTestId("info-port-lookup")).toHaveClass(/missing/);
  await page.getByTestId("node-lookup").click();
  await expect(panel(page).getByTestId("lookup-source")).toContainText("No info link");

  // Drag from the output of customers to the port at the top of lookup.
  const out = (await page.getByTestId("node-customers").locator(".react-flow__handle.source").boundingBox())!;
  const port = (await page.getByTestId("info-port-lookup").boundingBox())!;
  await page.mouse.move(out.x + out.width / 2, out.y + out.height / 2);
  await page.mouse.down();
  await page.mouse.move((out.x + port.x) / 2, (out.y + port.y) / 2, { steps: 5 });
  await page.mouse.move(port.x + port.width / 2, port.y + port.height / 2, { steps: 5 });
  await page.mouse.up();

  await expect(page.locator('[data-kind="info"]')).toHaveCount(1);
  await expect(page.getByTestId("info-port-lookup")).not.toHaveClass(/missing/);
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.locator(".cm-content")).toContainText("{ from: customers, to: lookup, type: info }");
});

test("runs with the lookup data and shows its count", async ({ page, request }) => {
  await open(page, request, withInfo);
  await page.getByRole("button", { name: "Run", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Run lookup.pipeline.yaml" });
  await dialog.getByRole("button", { name: "Run" }).click();
  await expect(page.getByTestId("run-status")).toContainText("✔ Last run succeeded", { timeout: 10_000 });
  await expect(page.getByTestId("rows-0")).toHaveText("3");
  await expect(page.getByTestId("node-lookup").getByTestId("counts")).toHaveText("4 → 4");
  await expect(page.getByRole("log", { name: "Run log" })).toContainText("read 4, written 4, lookup 3 in");
  await page.getByTestId("node-lookup").click();
  await expect(panel(page).getByTestId("run-counts")).toHaveText("3lookup records4read4written");
});
