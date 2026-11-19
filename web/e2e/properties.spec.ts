import { readFileSync } from "node:fs";
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
  const lines = await page.getByTestId("code").locator(":scope > div > span:nth-child(2)").allTextContents();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return lines.join("\n") + "\n";
}

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const field = (page: Page, key: string) => panel(page).locator(`[data-field="${key}"]`);

test("shows the properties of the selected action", async ({ page }) => {
  await open(page);
  await expect(panel(page)).toContainText("Select an action to see its properties.");
  // With the panel, the file bar is compact (mockup E7).
  await expect(page.getByTestId("crumbs")).toHaveText("properties.workflow.yaml");
  await expect(page.getByRole("button", { name: "Copy path" })).toBeVisible();

  await select(page, "combine");
  await expect(panel(page)).toContainText("Log");
  await expect(panel(page)).toContainText("Writes a message to the run log");
  await expect(field(page, "with.message").getByRole("textbox")).toHaveValue("Both branches are done");
  await expect(field(page, "with.message")).toContainText("*"); // required
  await expect(field(page, "with.level").getByRole("combobox")).toHaveValue("");
  await expect(field(page, "with.level").getByRole("combobox").locator("option").first()).toHaveText("info (default)");
  await expect(panel(page).getByRole("group", { name: "Join" })).toBeEnabled(); // two incoming links

  await select(page, "fetch-b");
  await expect(panel(page)).toContainText("Waits for a given time");
  await expect(field(page, "with.duration").getByRole("textbox")).toHaveValue("${DELAY_B}");
  await expect(field(page, "with.duration").getByRole("textbox")).toHaveClass(/text-expression/);
  await expect(panel(page).getByRole("group", { name: "Join" }).getByRole("button").first()).toBeDisabled();

  // Several actions selected; hiding and showing the panel.
  await page.getByTestId("node-fetch-a").click({ modifiers: ["Control"] });
  await expect(panel(page)).toContainText("2 actions selected");
  await page.getByRole("button", { name: "Hide properties" }).click();
  await expect(panel(page)).toHaveCount(0);
  await expect(page.getByTestId("crumbs")).toHaveText("workflows / properties.workflow.yaml");
  await page.getByRole("button", { name: "Show properties" }).click();
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
