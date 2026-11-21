import { readFileSync } from "node:fs";
import { editorText } from "./editor";
import { expect, test, type Page } from "@playwright/test";

// Properties panel (mockup design/mockups/e7-properties.html), against the server on a
// temporary copy of examples/hello (e2e/serve-copy.mjs), like edit.spec.ts.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

// Its own copy of parallel.workflow.yaml: the spec files run in parallel workers on the same server.
const path = "workflows/properties.workflow.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = readFileSync(new URL("../../examples/hello/workflows/parallel.workflow.yaml", import.meta.url), "utf8");

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("properties.workflow.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-fetch-b")).toBeVisible();
}

async function select(page: Page, id: string) {
  await page.getByTestId(`node-${id}`).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue(id);
}

/** The text of the YAML view, line by line. */
async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const text = await editorText(page);
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return text;
}

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const field = (page: Page, key: string) => panel(page).locator(`[data-field="${key}"]`);

test("shows the properties of the selected action", async ({ page }) => {
  await open(page);
  // Nothing selected: the properties of the workflow (E7b).
  await expect(panel(page).getByTestId("workflow-properties")).toBeVisible();
  // With the panel, the file bar is compact (mockup E7).
  await expect(page.getByTestId("crumbs")).toHaveText("properties.workflow.yaml");
  await expect(page.getByRole("button", { name: "Copy path" })).toBeVisible();

  await select(page, "combine");
  await expect(panel(page)).toContainText("Log");
  await expect(panel(page)).toContainText("Writes a message to the run log");
  await expect(field(page, "with.message").getByRole("textbox")).toHaveText("Both branches are done");
  await expect(field(page, "with.message")).toContainText("*"); // required
  await expect(field(page, "with.level").getByRole("combobox")).toHaveValue("");
  await expect(field(page, "with.level").getByRole("combobox").locator("option").first()).toHaveText("info (default)");
  await expect(panel(page).getByRole("group", { name: "Join" })).toBeEnabled(); // two incoming links

  await select(page, "fetch-b");
  await expect(panel(page)).toContainText("Waits for a given time");
  await expect(field(page, "with.duration").getByRole("textbox")).toHaveText("${DELAY_B}");
  // Variables in the expression colour (decision for mockup E4, editor S35a).
  await expect(field(page, "with.duration").locator(".rx-var")).toHaveText("${DELAY_B}");
  await expect(panel(page).getByRole("group", { name: "Join" }).getByRole("button").first()).toBeDisabled();

  // Several actions selected; hiding and showing the panel.
  await page.getByTestId("node-fetch-a").click({ modifiers: ["Control"] });
  await expect(panel(page)).toContainText("2 elements selected");
  // The toggle in the file bar is always there (in the YAML view disabled), pressed while the panel is shown.
  const toggle = page.getByRole("button", { name: "Properties panel" });
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Hide properties" }).click();
  await expect(panel(page)).toHaveCount(0);
  await expect(toggle).toHaveAttribute("aria-pressed", "false");
  // The file bar stays as it is, with or without the panel and in the YAML view.
  await expect(page.getByTestId("crumbs")).toHaveText("properties.workflow.yaml");
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("crumbs")).toHaveText("properties.workflow.yaml");
  await expect(page.getByRole("button", { name: "Copy path" })).toBeVisible();
  await expect(toggle).toBeDisabled();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  // The toggle does not move when the panel is shown or hidden.
  const before = await toggle.boundingBox();
  await toggle.click();
  await expect(panel(page)).toBeVisible();
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  expect(await toggle.boundingBox()).toEqual(before);
  await toggle.click();
  await expect(panel(page)).toHaveCount(0);
  await toggle.click();
  await expect(panel(page)).toBeVisible();
});

test("changes the configuration; typing in one field is one undo step", async ({ page }) => {
  await open(page);
  await select(page, "combine");
  const message = field(page, "with.message").getByRole("textbox");
  await message.fill("Both done");
  await message.pressSequentially(" now", { delay: 30 });
  await expect(page.getByTestId("unsaved")).toBeVisible();
  await message.blur();
  await expect.poll(() => yaml(page)).toContain("      message: Both done now\n");

  // A multi-line text becomes a literal block.
  await select(page, "combine");
  await field(page, "with.message").getByRole("textbox").fill("line one\nline two");
  await field(page, "with.message").getByRole("textbox").blur();
  await expect.poll(() => yaml(page)).toContain("      message: |-\n        line one\n        line two\n");

  // Both changes of the field are one step: one undo brings back the original.
  await page.getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => yaml(page)).toContain("      message: Both branches are done\n");

  // Select from the enum; "info (default)" removes the field again.
  await select(page, "combine");
  await field(page, "with.level").getByRole("combobox").selectOption("error");
  await expect.poll(() => yaml(page)).toContain("      message: Both branches are done\n      level: error\n");
  await select(page, "combine");
  await field(page, "with.level").getByRole("combobox").selectOption("");
  await expect.poll(() => yaml(page)).toContain("      message: Both branches are done\n  - id: report-error\n");
});

test("reports problems at the field while typing", async ({ page }) => {
  await open(page);
  await select(page, "combine");
  await field(page, "with.message").getByRole("textbox").fill("");
  await field(page, "with.message").getByRole("textbox").blur();
  await expect(field(page, "with.message").getByRole("alert")).toContainText('missing required field "message"');
  await expect(page.getByTestId("node-combine")).toHaveAttribute("data-severity", "error");

  // An invalid duration keeps the canvas; the error is shown at the field.
  const timeout = field(page, "timeout").getByRole("textbox");
  await timeout.fill("5 mins");
  await expect(field(page, "timeout").getByRole("alert")).toContainText('invalid duration "5 mins"');
  await expect(page.getByTestId("node-combine")).toBeVisible();
  await timeout.fill("5m");
  await expect(field(page, "timeout").getByRole("alert")).toHaveCount(0);
  await timeout.blur();
  expect(await yaml(page)).toContain("    timeout: 5m\n");
});

test("changes the execution settings", async ({ page }) => {
  await open(page);
  await select(page, "fetch-a");
  const strategy = panel(page).getByRole("group", { name: "Backoff strategy" });
  await expect(strategy.getByRole("button", { name: "Exponential" })).toBeDisabled();

  const attempts = field(page, "retry").getByRole("textbox").first();
  await attempts.fill("x");
  await attempts.blur();
  await expect(field(page, "retry").getByRole("alert")).toContainText("whole number of 1 or more");
  await attempts.fill("3");
  await attempts.blur();
  await expect(field(page, "retry").getByRole("alert")).toHaveCount(0);
  await field(page, "retry").getByRole("textbox").nth(1).fill("10s");
  await field(page, "retry").getByRole("textbox").nth(1).blur();
  await strategy.getByRole("button", { name: "Exponential" }).click();
  // The checkbox follows the file: it is ticked once the server has changed the text.
  await panel(page).getByRole("checkbox", { name: /Continue on error/ }).click();
  await expect(panel(page).getByRole("checkbox", { name: /Continue on error/ })).toBeChecked();
  await expect.poll(() => yaml(page)).toContain(
    "    retry:\n      attempts: 3\n      backoff: 10s\n      strategy: exponential\n    continueOnError: true\n",
  );
  await select(page, "fetch-a");
  await expect(panel(page)).toContainText("retry 3× · continue on error");

  // Join: report-error has two incoming links and join: first.
  await select(page, "report-error");
  const join = panel(page).getByRole("group", { name: "Join" });
  await expect(join.getByRole("button", { name: "First" })).toHaveAttribute("aria-pressed", "true");
  await join.getByRole("button", { name: "Each" }).click();
  await expect.poll(() => yaml(page)).toContain("    join: each\n");
});

test("renames an action in the panel", async ({ page }) => {
  await open(page);
  await select(page, "combine");
  const id = page.getByRole("textbox", { name: "ID", exact: true });
  await id.fill("fetch-a");
  await expect(field(page, "id")).toContainText("“fetch-a” is already used");
  await id.fill("merge");
  await id.press("Enter");
  await expect(page.getByTestId("node-merge")).toBeVisible();
  await expect(id).toHaveValue("merge");
  expect(await yaml(page)).toContain("{ from: fetch-a, to: merge }");
});

test("shows and changes the properties of a link", async ({ page }) => {
  await open(page);
  // The link fetch-a → report-error (failure) is links[4].
  await page.getByTestId("link-4").click();
  await page.keyboard.press("Escape"); // closes the link menu, the link stays selected
  const link = panel(page).getByTestId("link-properties");
  await expect(link.getByTestId("link-ends")).toHaveText("fetch-a → report-error");
  const condition = link.getByRole("group", { name: "Condition" });
  await expect(condition.getByRole("button", { name: "✖ Failure" })).toHaveAttribute("aria-pressed", "true");
  await expect(field(page, "when")).toContainText("Followed when fetch-a has failed");

  await condition.getByRole("button", { name: "∞ Always" }).click();
  await expect(page.getByTestId("link-4")).toHaveAttribute("data-kind", "always");
  await link.getByRole("checkbox", { name: /Disabled/ }).click();
  await expect(link.getByRole("checkbox", { name: /Disabled/ })).toBeChecked();
  expect(await yaml(page)).toContain("  - { from: fetch-a, to: report-error, when: always, disabled: true }\n");

  await page.getByTestId("link-4").click();
  await page.keyboard.press("Escape");
  await panel(page).getByRole("button", { name: "Delete link" }).click();
  await expect(page.getByRole("status")).toContainText("Deleted 1 link");
  expect(await yaml(page)).not.toContain("to: report-error, when: always");
});

test("shows and changes the properties of the workflow", async ({ page }) => {
  await open(page);
  const wf = panel(page).getByTestId("workflow-properties");
  await expect(field(page, "name").getByRole("textbox")).toHaveValue("parallel");
  await expect(wf).toContainText("DELAY_A");
  await expect(wf).toContainText("string · optional");

  await field(page, "description").getByRole("textbox").fill("Two branches");
  await field(page, "description").getByRole("textbox").blur();
  await field(page, "settings.maxConcurrentRuns").getByRole("textbox").fill("0");
  await field(page, "settings.maxConcurrentRuns").getByRole("textbox").blur();
  await expect(field(page, "settings.maxConcurrentRuns").getByRole("alert")).toContainText("whole number of 1 or more");
  await field(page, "settings.maxConcurrentRuns").getByRole("textbox").fill("1");
  await field(page, "settings.maxConcurrentRuns").getByRole("textbox").blur();
  await expect.poll(() => yaml(page)).toContain("name: parallel\ndescription: Two branches\n");
  expect(await yaml(page)).toContain("settings:\n  maxConcurrentRuns: 1\nactions:\n");

  // An empty name is reported at the field.
  await field(page, "name").getByRole("textbox").fill("");
  await field(page, "name").getByRole("textbox").blur();
  await expect(field(page, "name").getByRole("alert")).toContainText('missing required field "name"');
});

test("lists the problems below the canvas and jumps to the field", async ({ page }) => {
  await open(page);
  // Two problems: a missing message (error) and an invalid timeout.
  await select(page, "combine");
  await field(page, "with.message").getByRole("textbox").fill("");
  await field(page, "with.message").getByRole("textbox").blur();
  await select(page, "fetch-a");
  await field(page, "timeout").getByRole("textbox").fill("5 mins");
  await field(page, "timeout").getByRole("textbox").blur();
  await page.locator(".react-flow__pane").click({ position: { x: 10, y: 10 } });

  const bar = page.getByRole("region", { name: "Problems and log" });
  await expect(bar.getByTestId("problem-count")).toHaveText("1");
  await expect(bar.getByRole("table")).toHaveCount(0);
  // The badge opens the list, also in the YAML view (E9); there a click shows the line.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await page.getByTestId("error-count").click();
  await expect(page.getByRole("button", { name: "YAML", exact: true })).toHaveAttribute("aria-pressed", "true");
  const rows = bar.getByRole("table").getByRole("row");
  await expect(rows.nth(1)).toContainText('invalid duration "5 mins"');
  await expect(rows.nth(1)).toContainText("fetch-a · Timeout");
  await rows.nth(1).click();
  await expect(page.getByTestId("yaml-editor").locator(".cm-activeLine")).toContainText("timeout: 5 mins");
  // On the canvas a click selects the action and shows the field.
  await page.getByRole("button", { name: "Canvas", exact: true }).click();

  await rows.nth(1).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("fetch-a");
  await expect(field(page, "timeout").getByRole("textbox")).toBeFocused();
  await field(page, "timeout").getByRole("textbox").fill("5m");
  await field(page, "timeout").getByRole("textbox").blur();

  // Once the timeout is valid, the configuration is checked and the missing message shows up.
  await expect(rows.nth(1)).toContainText('missing required field "message"');
  await expect(rows.nth(1)).toContainText("combine · Message");
  await rows.nth(1).click();
  await expect(field(page, "with.message").getByRole("textbox")).toBeFocused();

  await bar.getByRole("button", { name: /^Problems/ }).click();
  await expect(bar.getByRole("table")).toHaveCount(0);
});
