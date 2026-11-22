import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// Suggestions and hover in the YAML editor (E19): field names and allowed values from the
// schemas of the file kinds (S58), in workflows the types of the catalog and the fields
// under with: of an action. Against the server on a temporary copy of examples/hello.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const trigger = "triggers/e2e-assist.yaml";
const workflow = "workflows/e2e-assist.workflow.yaml";

test.beforeEach(async ({ request }) => {
  await request.put(`/api/v1/files/${trigger}`, {
    headers: { "If-Match": "*" },
    data: `kind: Trigger
apiVersion: runnel/v1
name: e2e-assist
type: cron
target: workflows/hello.workflow.yaml
with:
  schedule: "0 7 * * *"
`,
  });
  await request.put(`/api/v1/files/${workflow}`, {
    headers: { "If-Match": "*" },
    data: `kind: Workflow
apiVersion: runnel/v1
name: e2e-assist
actions:
  - id: start
    type: core.start
  - id: log
    type: core.log
    with:
      message: hello
links:
  - { from: start, to: log }
`,
  });
});

test.afterAll(async ({ request }) => {
  await request.delete(`/api/v1/files/${trigger}`);
  await request.delete(`/api/v1/files/${workflow}`);
});

async function open(page: Page, name: string) {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  const yamlTab = page.getByRole("button", { name: "YAML", exact: true });
  if (await yamlTab.count()) await yamlTab.click();
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
}

/** Puts the cursor at the end of a line (1-based). */
async function endOf(page: Page, line: number) {
  await page.getByTestId("yaml-editor").locator(".cm-line").nth(line - 1).click();
  await page.keyboard.press("End");
}

/** CodeMirror takes keys for a suggestion list only 75 ms after it opened (interactionDelay, see expressions.spec.ts). */
const settle = (page: Page) => page.waitForTimeout(100);

const options = (page: Page) => page.locator(".cm-tooltip.rx-complete li .cm-completionLabel");

test("a trigger suggests the fields not set yet and their values", async ({ page }) => {
  await open(page, "e2e-assist.yaml");
  // Under with:, typing suggests the fields of with:; schedule is set already.
  await endOf(page, 7);
  await page.keyboard.press("Enter");
  await page.keyboard.type("tim"); // the new line keeps the indentation
  await expect(options(page)).toHaveText(["timezone"]);
  await expect(page.locator(".cm-tooltip.rx-complete")).toContainText("text");
  await settle(page);
  await page.keyboard.press("Enter");
  await expect.poll(() => editorText(page)).toContain('  schedule: "0 7 * * *"\n  timezone: ');

  // The allowed values of a field, with its default.
  await page.keyboard.press("Enter");
  await page.keyboard.type("overlap: ");
  await page.keyboard.press("Control+Space");
  await expect(options(page)).toHaveText(["skip", "allow"]);
  await expect(page.locator(".cm-tooltip.rx-complete li").first()).toContainText("default");
  await settle(page);
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await expect.poll(() => editorText(page)).toContain("  overlap: allow");

  // At the top level: the fields of a trigger without those in the file.
  await endOf(page, 5);
  await page.keyboard.press("Enter");
  await page.keyboard.press("Control+Space");
  await expect(options(page)).toContainText(["environment", "params"]);
  await expect(options(page)).not.toContainText(["name"]);
  await page.keyboard.type("envi");
  await expect(options(page)).toHaveText(["environment"]);
  await settle(page);
  await page.keyboard.press("Enter");
  // The environments of the project.
  await page.keyboard.press("Control+Space");
  await expect(options(page)).toHaveText(["dev", "prod"]);
  await page.keyboard.press("Escape");
  await expect.poll(() => editorText(page)).toContain("\nenvironment: \nwith:");
});

test("hover describes a field", async ({ page }) => {
  await open(page, "e2e-assist.yaml");
  await page.getByTestId("yaml-editor").getByText("schedule", { exact: true }).hover();
  const tip = page.getByTestId("yaml-hover");
  await expect(tip).toContainText("schedule · text");
  await expect(tip).toContainText("Example:");
});

test("a workflow suggests types and the fields of an action", async ({ page }) => {
  await open(page, "e2e-assist.workflow.yaml");
  // The value of type: is one of the catalog.
  await endOf(page, 8);
  for (let i = 0; i < 3; i++) await page.keyboard.press("Backspace");
  await page.keyboard.press("Control+Space");
  // "core." is left: the actions of the namespace, with their names.
  await expect(options(page)).toContainText(["core.log", "core.start"]);
  await expect(page.locator(".cm-tooltip.rx-complete li", { hasText: "core.log" })).toContainText("Log");
  await page.keyboard.press("Escape");
  await page.keyboard.type("log");

  // Under with: the fields of core.log, without message which is set.
  await endOf(page, 10);
  await page.keyboard.press("Enter");
  await page.keyboard.type("l");
  await expect(options(page)).toHaveText(["level"]);
  await settle(page);
  await page.keyboard.press("Enter");
  await page.keyboard.press("Control+Space");
  await expect(options(page)).toHaveText(["debug", "info", "warn", "error"]);
  await page.keyboard.press("Escape");

  // Hover on the type names the action.
  await page.getByTestId("yaml-editor").getByText("core.start").hover();
  await expect(page.getByTestId("yaml-hover")).toContainText("core.start – Start");
});
