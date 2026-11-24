import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// The forms of environments and project.yaml (mockup E20, E20b): variables as a table with
// the other level, secret store, inline secrets by name only, defaults and limits. Nothing
// is saved. Against the server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });

const open = async (page: Page, name: string) => {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  return page.getByTestId("settings-form");
};
const yaml = async (page: Page) => {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Form", exact: true }).click();
  return text;
};

test("edits the variables and secrets of an environment", async ({ page }) => {
  const form = await open(page, "prod.yaml");
  await expect(form.getByRole("heading", { name: "prod" })).toBeVisible();
  await expect(form.locator('[data-field="name"]')).toContainText("rename it in the explorer");
  // TARGET is set in project.yaml too: its value there is shown as overridden.
  const target = form.locator('[data-variable="TARGET"]');
  await expect(target.getByRole("textbox")).toHaveValue("Production");
  await expect(target).toContainText("World");
  await expect(target).toContainText("overridden");

  // A value, a new variable and a removed one: each changes its line only.
  await target.getByRole("textbox").fill("Live");
  await target.getByRole("textbox").press("Tab");
  await form.getByRole("button", { name: "Add variable" }).click();
  await form.getByRole("textbox", { name: "Name of the new variable" }).fill("ARCHIVE");
  await form.getByRole("textbox", { name: "Name of the new variable" }).press("Enter");
  await expect(form.locator('[data-variable="ARCHIVE"]')).toBeVisible();
  await form.getByRole("button", { name: "Remove STAGE" }).click();
  await expect(form.locator('[data-variable="STAGE"]')).toHaveCount(0);
  await expect.poll(() => yaml(page)).toContain('variables:\n  TARGET: Live\n  OUTBOX: \\\\fileserver\\exchange\\runnel-hello\n  ARCHIVE: ""\n');

  // inline: a secret is added with its value, the form shows its name only.
  await form.locator('[data-field="secretStore"]').getByRole("combobox").selectOption("inline");
  await form.getByRole("button", { name: "Add secret" }).click();
  await form.getByRole("textbox", { name: "Name of the new secret" }).fill("api-token");
  await form.getByLabel("Value of the new secret").fill("Pa55word");
  await form.locator('[data-field="secrets"]').getByRole("button", { name: "Add", exact: true }).click();
  const secret = form.locator('[data-secret="api-token"]');
  await expect(secret).toContainText("hidden in the form");
  await expect(form.locator('[data-field="secrets"]')).not.toContainText("Pa55word");
  await expect(form.locator('[data-field="secrets"]').getByRole("note")).toContainText("Plain text in this environment.");
  expect(await yaml(page)).toContain("secretStore: inline\nsecrets:\n  api-token: Pa55word\n");
});

test("edits project.yaml", async ({ page }) => {
  const form = await open(page, "project.yaml");
  await expect(form.getByRole("heading", { name: "hello" })).toBeVisible();
  // The environments that override a variable of the project.
  await expect(form.locator('[data-variable="TARGET"]')).toContainText("prod");
  await form.locator('[data-field="defaultEnvironment"]').getByRole("combobox").selectOption("prod");
  await form.getByRole("button", { name: "Defaults and limits" }).click();
  const limit = form.locator('[data-field="settings.maxConcurrentRuns"]').getByRole("textbox");
  await limit.fill("3");
  await limit.press("Tab");
  await form.locator('[data-field="workflow.defaultJoin"]').getByRole("combobox").selectOption("first");
  await expect.poll(() => yaml(page)).toContain("defaultEnvironment: prod\n");
  const text = await yaml(page);
  expect(text).toContain("# Example project. Check it with:");
  expect(text).toContain("settings:\n  maxConcurrentRuns: 3\n");
  expect(text).toContain("workflow:\n  defaultJoin: first\n");
});

test("sets how long the run history keeps runs in project.yaml", async ({ page }) => {
  const form = await open(page, "project.yaml");
  await form.getByRole("button", { name: "Run history" }).click();
  await form.locator('[data-field="settings.history.cli"]').getByRole("combobox").selectOption("errors");
  const days = form.locator('[data-field="settings.history.keepDays"]').getByRole("textbox");
  await days.fill("7");
  await days.press("Tab");
  await expect.poll(() => yaml(page)).toContain("settings:\n  history:\n    cli: errors\n    keepDays: 7\n");
  // A value out of range is not written.
  const most = form.locator('[data-field="settings.history.maxRuns"]').getByRole("textbox");
  await most.fill("0");
  await most.press("Tab");
  await expect(page.getByRole("alert").filter({ hasText: "Most runs: enter a whole number from 1 to 1,000,000" })).toBeVisible();
  // Empty values remove the mapping.
  await form.locator('[data-field="settings.history.cli"]').getByRole("combobox").selectOption("");
  await days.fill("");
  await days.press("Tab");
  await expect.poll(() => yaml(page)).not.toContain("history:");
});
