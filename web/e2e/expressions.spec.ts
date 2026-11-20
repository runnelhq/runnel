import { expect, test, type Page } from "@playwright/test";

// Expression editor (roadmap S35a, mockup design/mockups/s35-expressions.html): fields of
// the properties panel and link conditions with highlighting, suggestions, signature help
// and problems at their position. Runs against the server on a temporary copy of
// examples/hello (e2e/serve-copy.mjs), like edit.spec.ts.
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/expressions.workflow.yaml";
const fileURL = `/api/v1/files/${path}`;
const original = `kind: Workflow
apiVersion: runnel/v1
name: orders
parameters:
  - name: ORDERS
    type: integer
    default: "7"
  - name: DAY
    default: "2026-11-19"
actions:
  - id: start
    type: core.start
  - id: check
    type: core.log
    with:
      message: "Checking \${TARGET}"
  - id: many
    type: core.log
    with:
      message: Many
links:
  - { from: start, to: check }
  - { from: check, to: many }
layout:
  start: { x: 40, y: 120 }
  check: { x: 280, y: 120 }
  many: { x: 520, y: 120 }
`;

test.beforeEach(async ({ request }) => {
  await request.put(fileURL, { headers: { "If-Match": "*" }, data: original });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("expressions.workflow.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-check")).toBeVisible();
}

async function yaml(page: Page) {
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  const lines = await page.getByTestId("code").locator(":scope > div > span:nth-child(2)").allTextContents();
  await page.getByRole("button", { name: "Canvas", exact: true }).click();
  return lines.join("\n") + "\n";
}

const panel = (page: Page) => page.getByRole("region", { name: "Properties" });
const message = (page: Page) => panel(page).locator('[data-field="with.message"]').getByRole("textbox");
const suggestions = (page: Page) => page.getByRole("listbox");

async function selectCheck(page: Page) {
  await page.getByTestId("node-check").click();
  await expect(message(page)).toHaveText("Checking ${TARGET}");
}

test("suggests names after {{ and inserts them", async ({ page }) => {
  await open(page);
  await selectCheck(page);
  await message(page).click();
  await page.keyboard.press("End");

  // "{{" adds the closing braces and opens the suggestions: context and functions.
  await page.keyboard.type(" {{");
  await expect(message(page)).toHaveText("Checking ${TARGET} {{  }}");
  await expect(suggestions(page).getByRole("option", { name: /params/ })).toBeVisible();
  await expect(suggestions(page).getByRole("option", { name: /formatDate/ })).toBeVisible();

  // After "params." the parameters of the workflow, with their type.
  await page.keyboard.type("params.");
  await expect(suggestions(page).getByRole("option")).toHaveCount(2);
  await expect(suggestions(page).getByRole("option").first()).toContainText("ORDERS");
  await expect(suggestions(page).getByRole("option").first()).toContainText("integer");
  await page.waitForTimeout(100); // see interactionDelay below
  await page.keyboard.press("Enter");
  await expect(message(page)).toHaveText("Checking ${TARGET} {{ params.ORDERS }}");
  await expect(message(page).locator(".rx-root")).toHaveText("params");

  // Leaving the field applies it (Enter adds a line: the message may span several lines).
  await message(page).blur();
  await expect.poll(() => yaml(page)).toContain("      message: Checking ${TARGET} {{ params.ORDERS }}\n");
});

test("shows the signature of a function and common date patterns", async ({ page }) => {
  await open(page);
  await selectCheck(page);
  await message(page).click();
  await page.keyboard.press("End");
  await page.keyboard.type(" {{");
  await page.keyboard.type("formatDate(today(), '");
  await expect(page.locator(".rx-signature")).toContainText("formatDate(date, pattern) → string");
  await expect(page.locator(".rx-signature-current")).toHaveText("pattern");
  await expect(suggestions(page).getByRole("option").first()).toContainText("'dd.MM.yyyy'");
  // CodeMirror accepts a suggestion only 75 ms after the list opened (interactionDelay).
  await page.waitForTimeout(100);
  await page.keyboard.press("Enter");
  await expect(message(page)).toHaveText("Checking ${TARGET} {{ formatDate(today(), 'dd.MM.yyyy' }}");

  // Pointing at a variable shows its value from the project.
  await message(page).locator(".rx-var").hover();
  await expect(page.locator(".rx-hover")).toContainText("${TARGET} = World");
  await expect(page.locator(".rx-hover")).toContainText("project.yaml");
});

test("marks an unknown name and offers the correction", async ({ page }) => {
  await open(page);
  await selectCheck(page);
  await message(page).fill("Last: {{ reslt.output }}");
  await message(page).blur();
  const field = panel(page).locator('[data-field="with.message"]');
  await expect(field.getByRole("alert")).toContainText("unknown name reslt – did you mean result?");
  await expect(field.locator(".rx-error")).toHaveText("reslt");
  await field.getByRole("button", { name: "Replace with result" }).click();
  await expect(field.getByRole("alert")).toHaveCount(0);
  await expect.poll(() => yaml(page)).toContain('      message: "Last: {{ result.output }}"\n');
});

test("writes a link condition as an expression", async ({ page }) => {
  await open(page);
  await page.getByTestId("link-1").click();
  await page.keyboard.press("Escape"); // closes the link menu, the link stays selected
  const link = panel(page).getByTestId("link-properties");
  await link.getByRole("group", { name: "Condition" }).getByRole("button", { name: "ƒ Expr." }).click();
  const editor = link.getByTestId("link-expression").getByRole("textbox");
  await expect(editor).toHaveText("{{  }}");
  // Nothing written yet: the file is unchanged.
  await expect(page.getByTestId("unsaved")).toHaveCount(0);

  // Without braces, the text is taken as expression.
  await editor.fill("params.ORDERS > 3 && results.check.success");
  await editor.blur();
  await expect(page.getByTestId("link-1")).toHaveAttribute("data-kind", "expression");
  await expect.poll(() => yaml(page)).toContain('  - { from: check, to: many, when: "{{ params.ORDERS > 3 && results.check.success }}" }\n');

  // The fields of a result are suggested after "results.<id>.".
  await page.getByTestId("link-1").click();
  await page.keyboard.press("Escape");
  await expect(editor).toHaveText("{{ params.ORDERS > 3 && results.check.success }}");
  // Ctrl+Space asks for suggestions anywhere.
  await editor.fill("{{ results.check.");
  await page.keyboard.press("Control+Space");
  await expect(suggestions(page).getByRole("option", { name: /durationMs/ })).toBeVisible();

  // Back to a simple condition.
  await page.keyboard.press("Escape");
  await link.getByRole("group", { name: "Condition" }).getByRole("button", { name: "✔ Success" }).click();
  await expect.poll(() => yaml(page)).toContain("  - { from: check, to: many }\n");
});

test("edits a field in the larger editor with the reference", async ({ page }) => {
  await open(page);
  await selectCheck(page);
  const field = panel(page).locator('[data-field="with.message"]');
  await field.getByRole("button", { name: "Open the larger editor" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Message" });
  await expect(dialog).toContainText("check · with.message");
  const editor = dialog.getByTestId("expression-dialog-editor").getByRole("textbox");
  await expect(editor).toHaveText("Checking ${TARGET}");
  await expect(editor).toBeFocused();
  await expect(dialog.locator(".cm-lineNumbers")).toContainText("1");
  await expect(dialog.getByTestId("expression-dialog-status")).toHaveText("✔ No problems · result is text");

  // The reference lists the parameters and the functions; the search narrows it down.
  const ref = dialog.getByRole("list", { name: "Reference" });
  await expect(ref).toContainText("params.ORDERS");
  await expect(ref).toContainText("formatDate(date, pattern)");
  await dialog.getByRole("textbox", { name: "Search names and functions" }).fill("pad");
  await expect(ref.getByRole("listitem")).toHaveCount(2);

  // A click inserts at the cursor; outside of an expression with {{ … }} around it.
  await editor.click();
  await page.keyboard.press("Control+End");
  await page.keyboard.type(" ");
  await ref.getByRole("listitem", { name: /padLeft/ }).click();
  await expect(editor).toHaveText("Checking ${TARGET} {{ padLeft( }}");
  await page.keyboard.type("params.ORDERS, 3, '0')");
  await expect(dialog.getByTestId("expression-dialog-status")).toHaveText("Checked when applied · result is text");

  // Ctrl+S is kept from the page behind the dialog; the dialog and its text stay.
  await page.keyboard.press("Control+s");
  await expect(dialog).toBeVisible();
  await expect(editor).toHaveText("Checking ${TARGET} {{ padLeft(params.ORDERS, 3, '0') }}");

  // Ctrl+Enter applies, the field and the file follow.
  await page.keyboard.press("Control+Enter");
  await expect(dialog).toHaveCount(0);
  await expect(field.getByRole("textbox")).toHaveText("Checking ${TARGET} {{ padLeft(params.ORDERS, 3, '0') }}");
  await expect.poll(() => yaml(page)).toContain("      message: Checking ${TARGET} {{ padLeft(params.ORDERS, 3, '0') }}\n");

  // Ctrl+Shift+E opens it from the field; Escape leaves the field unchanged.
  await selectCheck2(page);
  await field.getByRole("textbox").click();
  await page.keyboard.press("Control+Shift+E");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Control+End");
  await page.keyboard.type(" changed {{");
  // The first Escape closes the suggestions, the second the dialog.
  await expect(suggestions(page)).toBeVisible();
  await page.waitForTimeout(100); // CodeMirror's interactionDelay, see above
  await page.keyboard.press("Escape");
  await expect(suggestions(page)).toHaveCount(0);
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(field.getByRole("textbox")).toHaveText("Checking ${TARGET} {{ padLeft(params.ORDERS, 3, '0') }}");
});

async function selectCheck2(page: Page) {
  await page.getByTestId("node-check").click();
  await expect(message(page)).toContainText("padLeft");
}
