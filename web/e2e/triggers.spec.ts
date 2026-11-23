import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// The form of a trigger (mockup E20, E20a): type as cards, fields of the type, the
// schedule in words, target, environment and parameters, "Run target now". Against the
// server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const files: Record<string, string> = {
  "triggers/e2e-nightly.yaml": `# Greets at night.
kind: Trigger
apiVersion: runnel/v1
name: e2e-nightly
type: cron
target: workflows/hello.workflow.yaml
enabled: false
with:
  schedule: "0 2 * * 1-5"   # weekdays
  timezone: Europe/Berlin
  overlap: skip
`,
  "triggers/e2e-hook.yaml": `kind: Trigger
apiVersion: runnel/v1
name: e2e-hook
type: http
target: workflows/hello.workflow.yaml
enabled: false
with:
  method: POST
  path: /e2e/{GREETING}
  auth: none
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

const open = async (page: Page, name: string) => {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  return page.getByTestId("trigger-form");
};
const yaml = async (page: Page) => {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Form", exact: true }).click();
  return text;
};

test("shows a schedule in words and changes the type with undo", async ({ page }) => {
  const form = await open(page, "e2e-nightly.yaml");
  await expect(form.getByRole("heading", { name: "e2e-nightly" })).toBeVisible();
  await expect(form).toContainText("Schedule · starts workflows/hello.workflow.yaml");
  // The explorer shows the type of the triggers the server runs.
  await expect(page.getByRole("tree").getByRole("treeitem", { name: "morning.yaml" })).toContainText("cron");
  await expect(form.getByRole("radio", { name: /Schedule/ })).toHaveAttribute("aria-checked", "true");
  await expect(form.getByTestId("schedule-reading")).toContainText("At 02:00, Monday to Friday");
  await expect(form.getByTestId("schedule-reading").locator("li")).toHaveCount(3);
  await expect(form.getByTestId("trigger-state")).toContainText("Disabled: starts no runs");

  // Another type removes the fields of the old one; Undo brings them back.
  await form.getByRole("radio", { name: /Interval/ }).click();
  await expect(form.getByRole("status")).toContainText("Type changed to interval: schedule, timezone were removed.");
  await expect(form.locator('[data-field="with.every"]')).toBeVisible();
  await expect(form.locator('[data-field="with.schedule"]')).toHaveCount(0);
  await form.getByRole("button", { name: "Undo" }).click();
  await expect(form.getByTestId("schedule-reading")).toContainText("At 02:00, Monday to Friday");
  expect(await yaml(page)).toBe(files["triggers/e2e-nightly.yaml"]);
  // Only the lines concerned change; overlap stays.
  await form.getByRole("radio", { name: /Interval/ }).click();
  await expect(form.locator('[data-field="with.every"]')).toBeVisible();
  expect(await yaml(page)).toBe(files["triggers/e2e-nightly.yaml"].replace("type: cron", "type: interval").replace(/  schedule.*\n  timezone.*\n/, ""));
  await page.keyboard.press("Control+z");
  await expect(form.getByTestId("schedule-reading")).toContainText("At 02:00, Monday to Friday");

  // A change of the schedule writes its line only; the reading follows.
  const schedule = form.locator('[data-field="with.schedule"]').getByRole("textbox");
  await schedule.fill("30 6 * * *");
  await schedule.press("Tab");
  await expect(form.getByTestId("schedule-reading")).toContainText("At 06:30, every day");
  expect(await yaml(page)).toContain('  schedule: 30 6 * * *   # weekdays\n');
});

test("sets enabled, environment and the parameters of the target", async ({ page }) => {
  const form = await open(page, "e2e-nightly.yaml");
  await form.getByRole("switch", { name: "Enabled" }).click();
  await expect(form.getByRole("switch", { name: "Enabled" })).toHaveAttribute("aria-checked", "true");
  await form.locator('[data-field="environment"]').getByRole("combobox").selectOption("prod");
  // The parameters of hello.workflow.yaml with their type and default.
  const greeting = form.locator('[data-param="GREETING"]');
  await expect(greeting).toContainText("default Hello");
  await greeting.getByRole("textbox").fill("Moin");
  await greeting.getByRole("textbox").press("Tab");
  await expect.poll(() => yaml(page)).toContain("target: workflows/hello.workflow.yaml\nenvironment: prod\nparams:\n  GREETING: Moin\nwith:\n");
  expect(await yaml(page)).not.toContain("enabled:");
  await expect(form.getByTestId("trigger-state")).toContainText("Next run after saving");
});

test("shows the address of an HTTP trigger and its sign-in", async ({ page }) => {
  const form = await open(page, "e2e-hook.yaml");
  await expect(form).toContainText("HTTP call · starts workflows/hello.workflow.yaml");
  await expect(form.getByTestId("trigger-address")).toContainText("POST");
  await expect(form.getByTestId("trigger-address")).toContainText("/e2e/{GREETING}");
  // GREETING comes from the path.
  await expect(form.locator('[data-param="GREETING"]')).toContainText("from the path");
  const signIn = form.getByRole("region", { name: "Sign-in" });
  await signIn.locator('[data-field="with.auth"]').getByRole("combobox").selectOption("token");
  await expect(signIn.locator('[data-field="with.auth.token"]').getByTestId("secret")).toContainText("${secret:e2e-hook-token}");
  await expect.poll(() => yaml(page)).toContain("  auth:\n    token: ${secret:e2e-hook-token}\n");
});

test("runs the target of the saved trigger now", async ({ page }) => {
  await open(page, "e2e-nightly.yaml");
  await page.getByRole("button", { name: "Run target now" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Started a run of hello.workflow.yaml" })).toBeVisible();
});
