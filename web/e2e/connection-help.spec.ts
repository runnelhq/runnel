import { expect, test, type Page } from "@playwright/test";

// Help for connection types (mockup design/mockups/e15f-connection-help.html, E15g): from
// the form of a connection ("?" next to the type, F1), from the search of the help
// (Ctrl+K), from "New → Connection" ("?" on a card, F1) and from the field connection of a
// step; a plugin without errors and example shows a neutral note. Against the server on a
// temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398", permissions: ["clipboard-read", "clipboard-write"] });
test.describe.configure({ mode: "serial" });

const files: Record<string, string> = {
  "connections/e2e-help-sftp.yaml":
    "kind: Connection\napiVersion: runnel/v1\nname: e2e-help-sftp\ndescription: SFTP server of the partner\ntype: sftp\nwith:\n  host: sftp.example.com\n  user: runnel\n",
  "workflows/e2e-help-upload.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: e2e-help-upload
actions:
  - id: start
    type: core.start
  - id: upload
    type: transfer.upload
    with:
      connection: e2e-help-sftp
      source: outbox/*.csv
      target: /incoming/
links:
  - { from: start, to: upload }
`,
};

test.beforeEach(async ({ request }) => {
  for (const [path, data] of Object.entries(files)) {
    expect((await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data })).ok()).toBe(true);
  }
});

test.afterAll(async ({ request }) => {
  for (const path of Object.keys(files)) await request.delete(`/api/v1/files/${path}`, { headers: { "If-Match": "*" } });
});

const help = (page: Page) => page.getByTestId("help-panel");

async function openConnection(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-help-sftp.yaml", { exact: true }).click();
  await expect(page.getByTestId("connection-form").getByRole("heading", { name: "e2e-help-sftp" })).toBeVisible();
}

// A connection type of a plugin as describe gives it: fields and a test, no errors or example.
const plugin = {
  type: "iban",
  label: "IBAN service",
  description: "Account of the IBAN service",
  plugin: "iban",
  test: "Asks the plugin to check the connection (connection.test); what it checks depends on the plugin.",
  schema: {
    type: "object",
    properties: { url: { type: "string", description: "Base address of the service" }, apiKey: { type: "string", format: "runnel-secret", description: "API key of the account" } },
    required: ["url"],
  },
};
const archive = { type: "dms", label: "Archive", description: "Document archive of the DMS", plugin: "dms", schema: { type: "object", properties: {} } };

async function withPluginTypes(page: Page) {
  await page.route("**/api/v1/catalog", async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { connectionTypes: unknown[] };
    body.connectionTypes.push(plugin, archive);
    await route.fulfill({ response: res, json: body });
  });
  await page.route("**/api/v1/schemas/connection", async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { properties: { with: { "x-runnel-variants": Record<string, unknown> } } };
    body.properties.with["x-runnel-variants"].iban = { ...plugin.schema, description: `${plugin.label}: ${plugin.description}` };
    body.properties.with["x-runnel-variants"].dms = { ...archive.schema, description: `${archive.label}: ${archive.description}` };
    await route.fulfill({ response: res, json: body });
  });
}

test("F1 in the form shows the help of the type; the example is copied", async ({ page, request }) => {
  await openConnection(page);
  const testButton = page.getByRole("button", { name: "Test connection" });
  await expect(testButton).toHaveAttribute("title", /^Test the saved connection with the environment dev: connects, checks the host key against hostKey/);
  await expect(testButton).toHaveAttribute("title", /\? next to the type or F1 for the help of SFTP$/);

  await page.getByTestId("connection-form").getByRole("textbox", { name: "Description" }).focus();
  await page.keyboard.press("F1");
  await expect(help(page).getByRole("heading", { name: "SFTP" })).toBeVisible();
  // With the help open, the file bar keeps only the name.
  await expect(page.getByTestId("crumbs")).toHaveText("e2e-help-sftp.yaml");
  await expect(page.getByTestId("kind")).toHaveCount(0);
  await expect(page.getByTestId("help-badges")).toContainText("Connection type");
  await expect(page.getByTestId("help-badges")).toContainText("Built-in");
  await expect(page.getByTestId("help-badges")).toContainText("Test connection");
  const bar = help(page).getByRole("navigation", { name: "Sections" });
  await expect(bar.locator("> button")).toHaveText(["Fields8", "Test connection", "Errors", "Example"]);
  // Fields in the groups of the form, secrets with their type.
  await expect(page.getByTestId("help-section-fields")).toContainText("Security");
  await expect(page.getByTestId("help-field-host")).toContainText("required");
  await expect(page.getByTestId("help-field-password")).toContainText("secret");
  await expect(page.getByTestId("help-section-test-connection")).toContainText("lists the start folder");
  await expect(page.getByTestId("help-section-errors")).toContainText("never trusts a host key on first use");
  await expect(help(page).getByRole("button", { name: "New connection from example" })).toHaveCount(0);

  await bar.getByRole("button", { name: "Example" }).click();
  await help(page).getByRole("button", { name: "Copy example" }).click();
  await expect(help(page).getByRole("button", { name: "Copied" })).toBeVisible();
  const catalog = (await (await request.get("/api/v1/catalog")).json()) as { connectionTypes: { type: string; example: string }[] };
  const example = catalog.connectionTypes.find((c) => c.type === "sftp")!.example;
  expect(example).toContain("type: sftp\n");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(example);

  await page.keyboard.press("Escape");
  await expect(help(page)).toHaveCount(0);
  await expect(page.getByTestId("kind")).toHaveText("Connection");

  // "?" next to the type opens it too; × closes it.
  await page.getByTestId("connection-form").getByRole("button", { name: "Help for SFTP" }).click();
  await expect(help(page).getByRole("heading", { name: "SFTP" })).toBeVisible();
  await help(page).getByRole("button", { name: "Close help" }).click();
  await expect(help(page)).toHaveCount(0);
});

test("the search finds connection types before actions and transforms", async ({ page }) => {
  await openConnection(page);
  await page.getByTestId("connection-form").getByRole("button", { name: "Help for SFTP" }).click();
  await page.keyboard.press("Control+k");
  await expect(help(page).getByRole("textbox", { name: "Search help" })).toBeFocused();
  await page.keyboard.type("ftp");
  const list = page.getByRole("listbox", { name: "Help" });
  await expect(list.getByRole("presentation").first()).toHaveText("Connection types3");
  await expect(list.getByRole("option")).toHaveCount(6);
  await expect(list.getByRole("option").nth(0)).toContainText("FTP");
  await expect(list.getByRole("option").nth(0)).toContainText("Connection");
  await expect(list.getByRole("option").nth(3)).toContainText("Action");
  await expect(list).toContainText("Actions and transforms");
  await expect(list).toContainText("more");
  await page.keyboard.press("Enter");
  await expect(help(page).getByRole("heading", { name: "FTP", exact: true })).toBeVisible();
  await expect(page.getByTestId("help-field-insecure")).toContainText("FTP sends the password");

  // An action from the search opens its help in the same panel.
  await page.keyboard.press("Control+k");
  await page.keyboard.type("upload files");
  await page.keyboard.press("Enter");
  await expect(help(page).getByRole("heading", { name: "Upload Files" })).toBeVisible();
});

test("New → Connection shows the test of each type and opens the help", async ({ page }) => {
  await withPluginTypes(page);
  await page.goto("/");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Connection/ }).click();
  const dialog = page.getByRole("dialog", { name: "New connection" });
  await expect(dialog.getByRole("radio", { name: /^SFTP/ })).toContainText("A server for file transfer over SSH");
  await expect(dialog.getByRole("radio", { name: /^SFTP/ })).toContainText("Test");
  await expect(dialog.getByRole("radio", { name: /^IBAN service/ })).toContainText("Plugin iban");
  await expect(dialog.getByRole("radio", { name: /^Archive/ })).toContainText("No test");
  await expect(dialog.getByTestId("chosen-test")).toHaveCount(0);

  await dialog.getByRole("radio", { name: /^SFTP/ }).click();
  await expect(dialog.getByTestId("chosen-test")).toContainText("Test connection for SFTP: connects, checks the host key");
  await dialog.getByRole("button", { name: "Help for SFTP" }).click();
  await expect(help(page).getByRole("heading", { name: "SFTP" })).toBeVisible();
  // Esc closes the help, the dialog stays.
  await page.keyboard.press("Escape");
  await expect(help(page)).toHaveCount(0);
  await expect(dialog).toBeVisible();

  await dialog.getByRole("radio", { name: /^Archive/ }).click();
  await expect(dialog.getByTestId("chosen-test")).toContainText("No test for Archive.");
  await dialog.getByRole("textbox", { name: "Name" }).focus();
  await page.keyboard.press("F1");
  await expect(help(page).getByRole("heading", { name: "Archive" })).toBeVisible();
  await expect(help(page)).toContainText("No more details from the plugin.");
  await help(page).getByRole("button", { name: "Close help" }).click();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toHaveCount(0);
});

test("a connection type of a plugin shows its test and a neutral note", async ({ page }) => {
  await withPluginTypes(page);
  await openConnection(page);
  await page.keyboard.press("F1");
  await page.keyboard.press("Control+k");
  await page.keyboard.type("iban");
  await expect(page.getByRole("listbox", { name: "Help" }).getByRole("option").first()).toContainText("IBAN service");
  await page.keyboard.press("Enter");
  await expect(help(page).getByRole("heading", { name: "IBAN service" })).toBeVisible();
  await expect(page.getByTestId("help-badges")).toContainText("Plugin iban");
  await expect(page.getByTestId("help-badges")).toContainText("Test connection");
  await expect(help(page).getByRole("navigation", { name: "Sections" }).locator("> button")).toHaveText(["Fields2", "Test connection"]);
  await expect(page.getByTestId("help-section-test-connection")).toContainText("Asks the plugin to check the connection (connection.test)");
  await expect(help(page)).toContainText("No more details from the plugin.");
  await expect(page.getByTestId("help-example")).toHaveCount(0);
});

test("? at the connection of a step opens the help of its type", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-help-upload.workflow.yaml", { exact: true }).click();
  await page.getByTestId("node-upload").click();
  const properties = page.getByRole("region", { name: "Properties" });
  await properties.locator('[data-field="with.connection"]').getByRole("button", { name: "Help for SFTP" }).click();
  await expect(help(page).getByRole("heading", { name: "SFTP" })).toBeVisible();
  await expect(page.getByTestId("help-badges")).toContainText("Connection type");
  // Back to the properties of the step.
  await help(page).getByRole("button", { name: "Properties" }).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("upload");
});
