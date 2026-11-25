import { expect, test, type APIRequestContext, type Page } from "@playwright/test";

// Plugins in the editor (mockup design/mockups/s52-plugins.html, S52b1): their state in the
// explorer, the overview, trusting after the dialog, untrusting, a changed plugin and one
// that cannot start. The plugin is a small Node script that speaks the protocol (ADR 0019).
// Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs), whose
// approvals are in a folder of the test.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

// The plugin: answers initialize and describe with one action; "broken" ends at once.
const script = `import { createInterface } from "node:readline";
if (process.argv[2] === "broken") { console.error("cannot open the directory"); process.exit(3); }
const send = (m) => process.stdout.write(JSON.stringify({ jsonrpc: "2.0", ...m }) + "\\n");
createInterface({ input: process.stdin }).on("line", (line) => {
  const { id, method } = JSON.parse(line);
  if (method === "initialize") send({ id, result: { protocolVersion: 1, encoding: "json", name: "demo", version: "1.0.0" } });
  else if (method === "describe") send({ id, result: { actions: [{ type: "demo.echo", label: "Echo", description: "Returns its text", schema: { type: "object", properties: { text: { type: "string" } } } }] } });
  else if (method === "shutdown") { send({ id, result: null }); process.exit(0); }
  else if (id !== undefined) send({ id, error: { code: -32601, message: "method not found" } });
});
`;
const pluginYAML = (args: string) =>
  `kind: Plugin\napiVersion: runnel/v1\nname: demo\nversion: 1.0.0\ndescription: A demo plugin\nnamespace: demo\nexecutable: node\nargs: [${args}]\n`;
const files: Record<string, string> = {
  "plugins/demo/plugin.mjs": script,
  "plugins/demo/bin/demo.exe": "MZ\u0000\u0000program",
  "plugins/demo/plugin.yaml": pluginYAML("plugin.mjs"),
  "workflows/e2e-plugin.workflow.yaml":
    "kind: Workflow\napiVersion: runnel/v1\nname: e2e-plugin\nactions:\n  - id: start\n    type: core.start\n  - id: say\n    type: demo.echo\n    with:\n      text: hi\nlinks:\n  - { from: start, to: say }\n",
};

const put = async (request: APIRequestContext, path: string, data: string) =>
  expect((await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data })).ok()).toBe(true);

test.beforeEach(async ({ request }) => {
  for (const [path, data] of Object.entries(files)) await put(request, path, data);
  await request.post("/api/v1/plugins/demo/untrust", { data: {} });
});

test.afterAll(async ({ request }) => {
  await request.post("/api/v1/plugins/demo/untrust", { data: {} });
  for (const path of Object.keys(files)) await request.delete(`/api/v1/files/${path}`, { headers: { "If-Match": "*" } });
  await request.delete("/api/v1/folders/plugins/demo/bin");
  await request.delete("/api/v1/folders/plugins/demo");
  await request.delete("/api/v1/folders/plugins");
});

const row = (page: Page) => page.getByRole("treeitem", { name: "demo", exact: true });
const view = (page: Page) => page.getByTestId("plugin-view");

async function openPlugin(page: Page) {
  await page.goto("/");
  await row(page).locator("> div").click();
  await expect(view(page)).toBeVisible();
}

test("a plugin that is not trusted is shown, trusted after the dialog and untrusted again", async ({ page, request }) => {
  await openPlugin(page);
  await expect(page.getByTestId("plugin-state-demo")).toHaveAttribute("title", "Not trusted");
  await expect(page.getByTestId("plugin-state")).toHaveText("! Not trusted");
  await expect(view(page)).toContainText("Not trusted on this computer");
  // Without starting the plugin: what the project uses of it.
  await expect(view(page).getByRole("region", { name: "Types" })).toContainText("demo.echo – 1 step(s)");
  await expect(view(page).getByRole("region", { name: "Used by" })).toContainText("say (demo.echo)");

  await page.getByRole("button", { name: "Trust…", exact: true }).click();
  const dialog = page.getByRole("alertdialog", { name: "Trust plugin demo?" });
  const { plugins } = (await (await request.get("/api/v1/plugins")).json()) as { plugins: { hash: string }[] };
  await expect(dialog.getByTestId("trust-hash")).toHaveText(plugins[0].hash);
  await expect(dialog).toContainText("node");
  await dialog.getByRole("button", { name: "Trust plugin" }).click();
  await expect(dialog).toBeHidden();

  await expect(page.getByTestId("plugin-state")).toHaveText("✓ Trusted");
  await expect(view(page)).toContainText("Trusted on this computer");
  await expect(view(page).getByRole("region", { name: "Types" }).getByRole("row", { name: /demo\.echo/ })).toContainText("Returns its text");
  await expect(page.getByTestId("plugin-state-demo")).toHaveAttribute("title", "Trusted");

  await page.getByRole("button", { name: "Untrust" }).click();
  await expect(page.getByTestId("plugin-state")).toHaveText("! Not trusted");
  await expect(page.getByTestId("plugin-state-demo")).toHaveAttribute("title", "Not trusted");
});

test("a changed plugin is trusted again; one that cannot start offers Try again", async ({ page, request }) => {
  const { plugins } = (await (await request.get("/api/v1/plugins")).json()) as { plugins: { hash: string }[] };
  expect((await request.post("/api/v1/plugins/demo/trust", { data: { hash: plugins[0].hash } })).ok()).toBe(true);
  await put(request, "plugins/demo/plugin.mjs", script + "// changed\n");

  await openPlugin(page);
  await expect(page.getByTestId("plugin-state")).toHaveText("! Changed since trusted");
  await expect(view(page)).toContainText("Changed since it was trusted");
  await page.getByRole("button", { name: "Trust again…", exact: true }).click();
  await page.getByRole("alertdialog", { name: "Trust plugin demo?" }).getByRole("button", { name: "Trust plugin" }).click();
  await expect(page.getByTestId("plugin-state")).toHaveText("✓ Trusted");

  // The plugin ends at once: after trusting it again (plugin.yaml changed), it cannot start.
  await put(request, "plugins/demo/plugin.yaml", pluginYAML("plugin.mjs, broken"));
  await page.reload();
  await row(page).locator("> div").click();
  await page.getByRole("button", { name: "Trust again…", exact: true }).click();
  await page.getByRole("alertdialog", { name: "Trust plugin demo?" }).getByRole("button", { name: "Trust plugin" }).click();
  await expect(page.getByTestId("plugin-state")).toHaveText("✕ Cannot start");
  await expect(view(page)).toContainText("cannot open the directory");
  await put(request, "plugins/demo/plugin.yaml", pluginYAML("plugin.mjs"));
  await page.getByRole("button", { name: "Try again" }).first().click();
  await expect(page.getByTestId("plugin-state")).toHaveText("! Changed since trusted");
});

test("a step of a plugin that is not trusted leads to the plugin from the panel and from Problems", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("treeitem", { name: "e2e-plugin.workflow.yaml" }).click();
  const node = page.getByTestId("node-say");
  await expect(node).toHaveAttribute("data-severity", "error");
  await node.click();
  const panel = page.getByRole("region", { name: "Properties" });
  await expect(panel.getByRole("alert")).toContainText("Plugin demo is not trusted on this computer");
  await panel.getByRole("button", { name: "Open plugin demo" }).click();
  await expect(view(page)).toBeVisible();
  await expect(page.getByTestId("plugin-state")).toHaveText("! Not trusted");

  // Back in the workflow: the same in Problems.
  await page.getByRole("tab", { name: /e2e-plugin\.workflow\.yaml/ }).click();
  const problems = page.getByRole("region", { name: "Problems and log" });
  await problems.getByRole("button", { name: /^Problems/ }).click();
  const row = problems.getByRole("table").getByRole("row", { name: /plugin "demo" is not trusted/ });
  await row.getByRole("button", { name: "Open plugin demo" }).click();
  await expect(page.getByTestId("plugin-view")).toBeVisible();
});

test("the program of a plugin stays in the explorer but does not open", async ({ page }) => {
  await page.goto("/");
  // The folder of a plugin starts closed; its chevron opens it.
  const exe = page.getByRole("treeitem", { name: "demo.exe" });
  await expect(exe).toHaveCount(0);
  const demo = page.getByRole("treeitem", { name: "demo", exact: true });
  await demo.locator("> div svg").first().click();
  await page.getByRole("treeitem", { name: "bin", exact: true }).locator("> div").click();
  await expect(exe.locator("> div")).toHaveAttribute("title", /a binary file, not shown in the editor/);
  await exe.locator("> div").click();
  await expect(page.getByRole("tab", { name: /demo\.exe/ })).toHaveCount(0);
});

test("trusting a plugin brings its types into the palette and the panel without reloading", async ({ page, request }) => {
  await page.goto("/");
  await page.getByRole("treeitem", { name: "e2e-plugin.workflow.yaml" }).click();
  await page.getByTestId("node-say").click();
  const panel = page.getByRole("region", { name: "Properties" });
  await expect(panel).toContainText("Action of plugin demo");

  // Trusted in a terminal (here: the API) while the editor is open: on focus it follows.
  const { plugins } = (await (await request.get("/api/v1/plugins")).json()) as { plugins: { hash: string }[] };
  expect((await request.post("/api/v1/plugins/demo/trust", { data: { hash: plugins[0].hash } })).ok()).toBe(true);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(panel.getByRole("heading", { name: "Echo" })).toBeVisible();
  await expect(page.getByTestId("node-say")).not.toHaveAttribute("data-severity", "error");
  await page.getByLabel("Search actions").fill("echo");
  // Without a category the action is grouped by its plugin.
  await expect(page.getByRole("region", { name: "Palette" }).getByRole("group", { name: "Plugin: demo" })).toContainText("demo.echo");
});
