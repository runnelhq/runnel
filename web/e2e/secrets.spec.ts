import { expect, test, type Page } from "@playwright/test";

// Secrets in the editor (S39g2, mockup S39f): at a secret field, per environment what to do –
// set, replace and delete values of the local store, the explanation for env, the warning
// for inline; the dialog "Set value" without reveal; the overview in the form of
// project.yaml. Against a copy of examples/hello whose local store has a key of its own
// (e2e/serve-copy.mjs --secret-key): dev reads secrets from secrets: (inline), test from
// environment variables (env), prod from the local store.
test.use({ baseURL: "http://127.0.0.1:7395" });
test.describe.configure({ mode: "serial" });

const files: Record<string, string> = {
  "environments/dev.yaml":
    "kind: Environment\napiVersion: runnel/v1\nname: dev\nsecretStore: inline\nvariables:\n  STAGE: development\nsecrets:\n  e2e-db-password: dev-only\n",
  "environments/test.yaml": "kind: Environment\napiVersion: runnel/v1\nname: test\nsecretStore: env\n",
  "connections/e2e-db.yaml":
    "kind: Connection\napiVersion: runnel/v1\nname: e2e-db\ntype: sqlserver\nwith:\n  host: db01.example.local\n  database: ERP\n  password: ${secret:e2e-db-password}\n",
};

test.beforeAll(async ({ request }) => {
  for (const [path, data] of Object.entries(files)) {
    expect((await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data })).ok()).toBe(true);
  }
});

test.afterEach(async ({ request }) => {
  for (const name of ["e2e-db-password", "e2e-old"]) await request.delete(`/api/v1/secrets/${name}`);
});

/** Changes the answers of GET /secrets; a request the page gave up (it loads again) is ignored. */
async function mockSecrets(page: Page, change: (json: { secrets: { environments: Record<string, unknown> }[]; local: Record<string, unknown> }) => void, onCall?: () => void) {
  await page.route("**/api/v1/secrets", async (route) => {
    onCall?.();
    try {
      const response = await route.fetch();
      const json = await response.json();
      change(json);
      await route.fulfill({ response, json });
    } catch {
      // the request was cancelled
    }
  });
}

async function openField(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-db.yaml", { exact: true }).click();
  const field = page.getByTestId("connection-form").locator('[data-field="with.password"]');
  await expect(field.getByTestId("secret-environments")).toBeVisible();
  return field;
}

test("sets, replaces and deletes a value of the local store at the field", async ({ page, request }) => {
  const field = await openField(page);
  await expect(field.getByTestId("secret-state-dev")).toContainText("⚠ dev inline");
  await expect(field.getByTestId("secret-state-prod")).toContainText("⚠ prod missing");
  await expect(field.getByTestId("secret-state-test")).toContainText("⚠ test missing");
  // The list is open: something is missing. Per source what to do.
  const dev = field.locator('[data-env="dev"]');
  await expect(dev).toContainText("the value is in the file and in Git");
  const test_ = field.locator('[data-env="test"]');
  await expect(test_).toContainText("Runnel cannot set it");
  await expect(test_.getByRole("button")).toHaveCount(0);
  await expect(field).toContainText("Runs of the Windows service read their own store");

  // Set value: masked, pasted with line breaks kept, never shown.
  await field.locator('[data-env="prod"]').getByRole("button", { name: "Set value…" }).click();
  const dialog = page.getByRole("dialog", { name: "Set value" });
  await expect(dialog).toContainText("Set value of e2e-db-password");
  await expect(dialog).toContainText("prod");
  const value = dialog.getByRole("textbox", { name: "Value" });
  await expect(value).toBeFocused();
  await value.evaluate((el) => {
    const data = new DataTransfer();
    data.setData("text/plain", "line one\nline two");
    el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: data, bubbles: true, cancelable: true }));
  });
  await expect(dialog).toContainText("Pasted from the clipboard · 17 characters");
  await expect(dialog).not.toContainText("line one");
  await page.keyboard.type("!");
  await expect(dialog).toContainText("18 characters");
  await expect(dialog.getByRole("button", { name: /show|reveal/i })).toHaveCount(0);
  await dialog.getByRole("button", { name: "Set value", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(field.getByTestId("secret-state-prod")).toContainText("● prod local");
  const prod = field.locator('[data-env="prod"]');
  await expect(prod).toContainText("Set in the local store on this computer · changed");
  const info = await (await request.get("/api/v1/secrets")).json();
  expect(JSON.stringify(info)).not.toContain("line one");
  expect(info.secrets.find((s: { name: string }) => s.name === "e2e-db-password").environments.prod.state).toBe("set");

  // Replace: the same dialog with the hint that the old value is overwritten.
  await prod.getByRole("button", { name: "Replace value…" }).click();
  await expect(dialog).toContainText("Replace value of e2e-db-password");
  await expect(dialog).toContainText("The current value is overwritten.");
  await page.keyboard.type("new");
  await dialog.getByRole("button", { name: "Replace value" }).click();
  await expect(dialog).toBeHidden();

  // Delete: asks first and names the environment.
  await prod.getByRole("button", { name: "Delete value" }).click();
  const confirm = page.getByRole("alertdialog", { name: "Delete the value of e2e-db-password?" });
  await expect(confirm).toContainText("runs with prod do not start");
  await confirm.getByRole("button", { name: "Delete value" }).click();
  await expect(field.getByTestId("secret-state-prod")).toContainText("⚠ prod missing");

  // "Open dev.yaml" opens the file of the plain text.
  await dev.getByRole("button", { name: "Open dev.yaml" }).click();
  await expect(page.getByRole("tab", { name: /dev\.yaml/ })).toBeVisible();
});

test("keeps the value and shows the error of the server", async ({ page }) => {
  const field = await openField(page);
  await page.route("**/api/v1/secrets/e2e-db-password", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({
        code: "secret_key",
        error: "the value was not stored. The local secret store needs a key on this system: set RUNNEL_MASTER_KEY to 32 random bytes in base64, then restart runnel serve",
      }),
    }),
  );
  await field.locator('[data-env="prod"]').getByRole("button", { name: "Set value…" }).click();
  const dialog = page.getByRole("dialog", { name: "Set value" });
  await page.keyboard.type("secret");
  await dialog.getByRole("button", { name: "Set value", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("The value was not stored.");
  await expect(dialog.getByRole("alert")).toContainText("RUNNEL_MASTER_KEY");
  await expect(dialog).toContainText("6 characters");
  await expect(dialog.getByRole("button", { name: "Try again" })).toBeEnabled();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(field.getByTestId("secret-state-prod")).toContainText("⚠ prod missing");
});

test("shows all secrets in the form of project.yaml with filters", async ({ page, request }) => {
  // A value no file uses any more.
  expect((await request.put("/api/v1/secrets/e2e-old", { data: { value: "x", environment: "prod" } })).ok()).toBe(true);
  const field = await openField(page);
  await expect(page.getByRole("tree").getByRole("treeitem", { name: /project\.yaml/ }).getByTestId("explorer-mark")).toHaveText("⚠ 2");
  await field.getByRole("button", { name: "All secrets of the project…" }).click();
  const overview = page.getByTestId("secrets-overview");
  await expect(overview).toBeVisible();
  const section = page.getByRole("region", { name: "Secrets" });
  await expect(section).toContainText("⚠ 2 missing");
  await expect(section).toContainText("⚠ dev: plain text");
  const row = overview.locator('tr[data-secret="e2e-db-password"]');
  await expect(row).toContainText("plain text");
  await expect(row).toContainText("missing");
  await expect(overview.locator('tr[data-secret="e2e-old"]')).toContainText("not used");

  // A click shows the uses and per environment what to do.
  await row.click();
  await expect(overview).toContainText("Used in");
  await expect(overview.getByRole("button", { name: "connections/e2e-db.yaml:8" })).toBeVisible();
  await expect(overview.locator('[data-env="test"]')).toContainText("Runnel cannot set it");

  const filter = (name: string) => overview.getByRole("group", { name: "Filter secrets" }).getByRole("button", { name: new RegExp(name) });
  await filter("Missing").click();
  await expect(overview.locator('tr[data-secret="e2e-old"]')).toHaveCount(0);
  await expect(row).toBeVisible();
  await filter("Plain text").click();
  await expect(row).toBeVisible();
  await filter("Not used").click();
  await expect(row).toHaveCount(0);
  const old = overview.locator('tr[data-secret="e2e-old"]');
  await old.getByRole("button", { name: "Delete value" }).click();
  const confirm = page.getByRole("alertdialog", { name: "Delete the value of e2e-old?" });
  await expect(confirm).toContainText("No file of the project uses it any more.");
  await confirm.getByRole("button", { name: "Delete value" }).click();
  await expect(old).toHaveCount(0);
  await expect(overview).toContainText("No secret matches the filter.");

  // The service store: commands to copy.
  await expect(overview).toContainText("runnel secret missing --service");
});

test("turns setting off when the server says so", async ({ page }) => {
  await mockSecrets(page, (json) => {
    json.local.readOnly = "service";
    json.local.store = "service";
  });
  const field = await openField(page);
  await expect(field.locator('[data-env="prod"]').getByRole("button", { name: "Set value…" })).toBeDisabled();
  await expect(field).toContainText("This server runs as the Windows service");
  await page.getByRole("tree").getByText("project.yaml", { exact: true }).click();
  await expect(page.getByTestId("secrets-read-only")).toContainText("Setting secrets is off for this server");
  await page.getByTestId("secrets-overview").locator('tr[data-secret="e2e-db-password"]').click();
  await expect(page.getByTestId("secrets-overview").locator('[data-env="prod"]').getByRole("button", { name: "Set value…" })).toBeDisabled();
});

test("explains a local store that cannot be read", async ({ page }) => {
  let calls = 0;
  await mockSecrets(
    page,
    (json) => {
      for (const s of json.secrets) s.environments.prod = { store: "local", state: "unavailable" };
      json.local.error = "access to the file of the local secret store was denied; details are in the server log";
      json.local.unused = [];
    },
    () => calls++,
  );
  const field = await openField(page);
  await expect(field.getByTestId("secret-state-prod")).toContainText("– prod cannot be read");
  const prod = field.locator('[data-env="prod"]');
  await expect(prod).toContainText("The local store cannot be read: access to the file of the local secret store was denied.");
  await expect(prod).toContainText("runnel/secrets");
  const before = calls;
  await prod.getByRole("button", { name: "Check again" }).click();
  await expect.poll(() => calls).toBeGreaterThan(before);
});
