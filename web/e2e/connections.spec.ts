import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// Connections in the editor without a form of their own (quality round before S39b):
// the choice of connection: in the panel, SQL over several lines, and the fields of the
// connection type in the YAML editor and the Reference. Against the server on a temporary
// copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const files: Record<string, string> = {
  "connections/e2e-shop.yaml": "kind: Connection\napiVersion: runnel/v1\nname: e2e-shop\ntype: sqlite\nwith:\n  path: out/e2e.db\n",
  "connections/e2e-api.yaml": "kind: Connection\napiVersion: runnel/v1\nname: e2e-api\ntype: http\nwith:\n  baseUrl: https://api.example.com/\n",
  "workflows/e2e-db.workflow.yaml": `kind: Workflow
apiVersion: runnel/v1
name: e2e-db
actions:
  - id: start
    type: core.start
  - id: count
    type: db.query
    with:
      connection: e2e-shop
      sql: |-
        SELECT COUNT(*)
        FROM orders
links:
  - { from: start, to: count }
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

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const field = (page: Page, key: string) => panel(page).locator(`[data-field="${key}"]`);

test("the panel offers the connections of a fitting type and shows SQL on several lines", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-db.workflow.yaml", { exact: true }).click();
  await page.getByTestId("node-count").click();
  const choice = field(page, "with.connection").getByRole("combobox");
  await expect(choice).toHaveValue("e2e-shop");
  // Only sqlite, sqlserver and postgres fit db.query: the http connection is not offered.
  await expect(choice.locator("option")).toContainText(["Not set", "e2e-shop (sqlite)"]);
  await expect(choice.locator("option", { hasText: "e2e-api" })).toHaveCount(0);
  await expect(field(page, "with.sql").getByRole("textbox")).toHaveText(/SELECT COUNT\(\*\)\s*FROM orders/);

  // "Not set" removes the field.
  await choice.selectOption("");
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect.poll(() => editorText(page)).not.toContain("connection:");
});

test("a connection that is missing or of another type stays selectable and is marked", async ({ page, request }) => {
  for (const [name, mark] of [["e2e-gone", "e2e-gone (not found)"], ["e2e-api", "e2e-api (type http does not fit)"]]) {
    const wf = files["workflows/e2e-db.workflow.yaml"].replace("connection: e2e-shop", `connection: ${name}`);
    await request.put("/api/v1/files/workflows/e2e-db.workflow.yaml", { headers: { "If-Match": "*" }, data: wf });
    await page.goto("/");
    await page.getByRole("tree").getByText("e2e-db.workflow.yaml", { exact: true }).click();
    await page.getByTestId("node-count").click();
    const choice = field(page, "with.connection").getByRole("combobox");
    await expect(choice).toHaveValue(name);
    await expect(choice.locator("option:checked")).toHaveText(mark);
  }
});

test("a connection file suggests the fields of its type", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-api.yaml", { exact: true }).click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
  // The Reference lists the fields of http under with:.
  const ref = page.getByRole("region", { name: "Reference" });
  const http = ref.getByRole("group", { name: "with: for http" });
  await expect(http.getByTestId("field-with.baseUrl")).toContainText("✓ line");
  await expect(http.getByTestId("field-with.auth")).toBeVisible();

  // Typing under with: suggests them, with their values.
  await page.getByTestId("yaml-editor").locator(".cm-line").nth(5).click();
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await page.keyboard.type("au");
  const options = page.locator(".cm-tooltip.rx-complete li .cm-completionLabel");
  await expect(options).toHaveText(["auth"]);
  await page.waitForTimeout(100);
  await page.keyboard.press("Enter");
  await page.keyboard.press("Control+Space");
  await expect(options).toContainText(["none", "bearer", "basic", "apiKey", "oauth2"]);
  await page.keyboard.press("Escape");
  // Hover describes a field of the type.
  await page.getByTestId("yaml-editor").getByText("baseUrl", { exact: true }).hover();
  await expect(page.getByTestId("yaml-hover")).toContainText("Base address of the API");
});

// The form of a connection (mockup S39b, S39c2a): fields of the type in sections, only
// those of the chosen sign-in; a change writes only its line; Test connection with the
// environment shows the result above the form.
test("edits a connection in the form and tests it", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-api.yaml", { exact: true }).click();
  const form = page.getByTestId("connection-form");
  await expect(form.getByRole("heading", { name: "e2e-api" })).toBeVisible();
  await expect(form).toContainText("HTTP API");
  await expect(form.getByRole("region", { name: "API" }).locator('[data-field="with.baseUrl"]').getByRole("textbox")).toHaveText("https://api.example.com/");
  // Only the fields of the chosen sign-in: none shows no token.
  const signIn = form.getByRole("region", { name: "Sign-in" });
  await expect(signIn.locator('[data-field="with.token"]')).toHaveCount(0);
  await signIn.locator('[data-field="with.auth"]').getByRole("combobox").selectOption("bearer");
  await expect(signIn.locator('[data-field="with.token"]')).toBeVisible();
  // "More" is folded.
  const more = form.getByRole("region", { name: "More" });
  await expect(more).toContainText("headers");
  await expect(more.locator('[data-field="with.headers"]')).toHaveCount(0);
  // The change went into the text: one new line, the rest as it was.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect.poll(() => editorText(page)).toBe(files["connections/e2e-api.yaml"] + "  auth: bearer\n");
  await page.getByRole("button", { name: "Form", exact: true }).click();

  // Test connection: the saved file, with the environment chosen beside the button.
  await page.route("**/api/v1/connections/e2e-api/test", async (route) => {
    expect(route.request().postDataJSON()).toEqual({ environment: "prod" });
    await route.fulfill({ json: { ok: false, message: "GET https://api.example.com/: the API refused the sign-in (401 Unauthorized)", environment: "prod", durationMs: 120 } });
  });
  await page.getByRole("combobox", { name: "Environment of the test" }).selectOption("prod");
  await page.getByRole("button", { name: "Test connection" }).click();
  const result = page.getByTestId("test-result");
  await expect(result).toContainText("✕");
  await expect(result).toContainText("The test failed");
  await expect(result).toContainText("Environment prod · GET https://api.example.com/: the API refused the sign-in (401 Unauthorized)");
});

test("tests a database connection for real", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-shop.yaml", { exact: true }).click();
  await page.getByRole("button", { name: "Test connection" }).click();
  // The file does not exist: the test says a run creates it, and creates nothing.
  await expect(page.getByTestId("test-result")).toContainText("✓");
  await expect(page.getByTestId("test-result")).toContainText("does not exist yet; a run creates it");
});

// Secret fields (mockup S39f, S39c2b): the reference with its state per environment, a
// choice of names, a warning for plain text; never a value.
test("shows secrets by reference and warns about plain text", async ({ page, request }) => {
  const path = "connections/e2e-secret.yaml";
  const text = "kind: Connection\napiVersion: runnel/v1\nname: e2e-secret\ntype: http\nwith:\n  baseUrl: https://api.example.com/\n  auth: bearer\n  token: ${secret:env/RUNNEL_E2E_NOT_SET}\n";
  await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: text });
  try {
    await page.goto("/");
    await page.getByRole("tree").getByText("e2e-secret.yaml", { exact: true }).click();
    const token = page.getByTestId("connection-form").locator('[data-field="with.token"]');
    await expect(token.getByTestId("secret")).toContainText("${secret:env/RUNNEL_E2E_NOT_SET}");
    await expect(token.getByTestId("secret-state-dev")).toContainText("⚠ dev missing");

    // Choose another name: a new one is offered with what it will be.
    await token.getByRole("button", { name: "Change…" }).click();
    const picker = page.getByRole("dialog", { name: "Choose a secret" });
    await picker.getByRole("textbox", { name: "Name of the secret" }).fill("e2e-token");
    await picker.getByRole("button", { name: /Use new secret/ }).click();
    await expect(token.getByTestId("secret")).toContainText("${secret:e2e-token}");
    // The state comes from the saved files: after saving, the default store of hello is
    // local (S39d), where the secret is not set.
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    await page.goto("/");
    await page.getByRole("tree").getByText("e2e-secret.yaml", { exact: true }).click();
    await expect(token.getByTestId("secret-state-dev")).toContainText("⚠ dev missing");

    // Plain text is shown with a warning.
    await token.getByRole("button", { name: "Change…" }).click();
    await page.getByRole("button", { name: /Edit as text/ }).click();
    const input = token.getByRole("textbox");
    await input.fill("Pa55word");
    await input.press("Enter");
    await expect(token.getByTestId("plaintext")).toContainText("Plain text in a secret field.");
    await page.getByRole("button", { name: "YAML", exact: true }).click();
    await expect.poll(() => editorText(page)).toContain("  token: Pa55word\n");
  } finally {
    await request.delete(`/api/v1/files/${path}`, { headers: { "If-Match": "*" } });
  }
});

// The explorer shows connections with the sign of their type; New → Connection asks for
// name and type and creates the file with the required fields (mockup S39b, "new").
test("creates a connection and shows its type in the explorer", async ({ page, request }) => {
  await page.goto("/");
  const row = page.getByRole("tree").getByRole("treeitem", { name: "e2e-api.yaml" });
  await expect(row).toContainText("http");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Connection/ }).click();
  const dialog = page.getByRole("dialog", { name: "New connection" });
  await dialog.getByRole("textbox", { name: "Name" }).fill("e2e-new");
  await expect(dialog.getByRole("button", { name: "Create" })).toBeDisabled(); // no type yet
  await dialog.getByRole("radio", { name: /SQLite/ }).click();
  await dialog.getByRole("button", { name: "Create" }).click();
  try {
    await expect(page.getByTestId("connection-form").getByRole("heading", { name: "e2e-new" })).toBeVisible();
    expect(await (await request.get("/api/v1/files/connections/e2e-new.yaml")).text()).toBe(
      'kind: Connection\napiVersion: runnel/v1\nname: e2e-new\ntype: sqlite\nwith:\n  path: "out/e2e-new.db"\n',
    );
    await expect(page.getByRole("tree").getByRole("treeitem", { name: "e2e-new.yaml" })).toContainText("sqlite");
  } finally {
    await request.delete("/api/v1/files/connections/e2e-new.yaml", { headers: { "If-Match": "*" } });
  }
});
