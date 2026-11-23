import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// Reference instead of the palette (mockup design/mockups/e18-reference.html): the lower
// part of the sidebar follows the open file – the fields of project.yaml, environments and
// triggers with Insert, the variables of the project, nothing for documents. Against the
// server on a temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const workflow = "workflows/e2e-vars.workflow.yaml";
const doc = "docs/e2e-reference.md";

test.beforeEach(async ({ request }) => {
  await request.put(`/api/v1/files/${workflow}`, {
    headers: { "If-Match": "*" },
    data: `kind: Workflow
apiVersion: runnel/v1
name: e2e-vars
actions:
  - { id: start, type: core.start }
  - id: log
    type: core.log
    with:
      message: "\${E2E_MISSING}"
links:
  - { from: start, to: log }
`,
  });
  await request.put(`/api/v1/files/${doc}`, { headers: { "If-Match": "*" }, data: "# Reference\n" });
});

test.afterAll(async ({ request }) => {
  await request.delete(`/api/v1/files/${workflow}`);
  await request.delete(`/api/v1/files/${doc}`);
});

async function openFile(page: Page, name: string) {
  await page.goto("/");
  await page.getByRole("tree").getByText(name, { exact: true }).click();
  // project.yaml and environments open as a form (E20b); Insert works in the YAML view.
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
}

test("project.yaml shows its fields and inserts one", async ({ page }) => {
  await openFile(page, "project.yaml");
  await expect(page.getByRole("region", { name: "Palette" })).toHaveCount(0);
  const ref = page.getByRole("region", { name: "Reference" });
  await expect(ref.getByRole("button", { name: "Fields" })).toHaveAttribute("aria-pressed", "true");
  // Fields in the file take one line with their line; the others show their short text.
  await expect(ref.getByTestId("field-defaultEnvironment")).toContainText("✓ line 6");
  const settings = ref.getByTestId("field-settings");
  await expect(settings).toContainText("Limits for the runs of the project");
  await settings.click();
  await settings.getByRole("button", { name: "+ Insert" }).click();
  // Inserting goes through the file model: wait for it instead of reading once.
  await expect.poll(() => editorText(page)).toContain("\nsettings:\n  \n");
  await expect(page.getByRole("tab", { name: /project\.yaml/ })).toContainText("●");
  // The field is in the file now: the open entry offers to go to its line.
  await expect(ref.getByTestId("field-settings").getByRole("button", { name: "Go to line 10" })).toBeVisible();

  // Go to line: the cursor is put on the line of a field that is there.
  await ref.getByTestId("field-name").click();
  await ref.getByTestId("field-name").getByRole("button", { name: "Go to line 4" }).click();
  await expect(page.getByTestId("file-summary")).toContainText("Ln 4,");
});

test("a trigger shows the fields of its type and inserts under with:", async ({ page }) => {
  // Triggers open as a form (E20a); Insert works in the YAML view.
  await page.goto("/");
  await page.getByRole("tree").getByText("morning.yaml", { exact: true }).click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
  const ref = page.getByRole("region", { name: "Reference" });
  await expect(ref.getByRole("button", { name: "Fields" })).toHaveCount(0); // triggers have no variables view
  const cron = ref.getByRole("group", { name: "with: for cron" });
  await expect(cron.getByTestId("field-with.schedule")).toContainText("✓ line");
  await expect(cron.getByTestId("field-with.timezone")).toContainText("✓ line");
  // The fields of the other types are folded away.
  await expect(ref.getByRole("group", { name: /^Other types: interval, file, http/ }).getByTestId("field-with.pattern")).toHaveCount(0);
  const overlap = cron.getByTestId("field-with.overlap");
  await overlap.click();
  await expect(overlap).toContainText("Allowed");
  await overlap.getByRole("button", { name: "+ Insert" }).click();
  await expect.poll(() => editorText(page)).toMatch(/timezone: Europe\/Berlin\n {2}overlap: skip\n/);
  // The value is selected: typing replaces it.
  await page.keyboard.type("allow");
  await expect.poll(() => editorText(page)).toContain("  overlap: allow\n");
});

test("an environment shows the variables and adds a missing one", async ({ page }) => {
  await openFile(page, "prod.yaml");
  const vars = page.getByRole("region", { name: "Variables" });
  await expect(vars.getByRole("button", { name: "Variables" })).toHaveAttribute("aria-pressed", "true");
  const row = vars.getByTestId("variable-E2E_MISSING");
  await expect(row).toContainText("1 use");
  await expect(row.getByTitle("used, but not set for prod")).toBeVisible();
  await expect(vars.getByTestId("variable-TARGET").getByTitle("set in environments/prod.yaml")).toHaveCount(1);
  // The file bar warns, with the places in its tooltip.
  await expect(page.getByTestId("variable-warning")).toHaveText("⚠ 1");
  await expect(page.getByTestId("variable-warning")).toHaveAttribute("title", /E2E_MISSING is used, but not set for prod \(workflows\/e2e-vars\.workflow\.yaml:9\)/);

  await row.click();
  await vars.getByRole("button", { name: "+ Add to prod.yaml" }).click();
  // Inserting goes through the file model: wait for it instead of reading once.
  await expect.poll(() => editorText(page)).toContain('\n  E2E_MISSING: ""\n');
  await page.keyboard.type("https://api.example.com");
  await expect.poll(() => editorText(page)).toContain('  E2E_MISSING: "https://api.example.com"\n');

  // A use opens its file at the line.
  await row.click();
  await row.click();
  await vars.getByRole("button", { name: `${workflow}:9` }).click();
  await expect(page.getByRole("tab", { name: /e2e-vars\.workflow\.yaml/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByTestId("file-summary")).toContainText("Ln 9,");
  // A workflow shows the palette again.
  await expect(page.getByRole("region", { name: "Palette" })).toBeVisible();
});

test("documents show the explorer only", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("e2e-reference.md", { exact: true }).click();
  await expect(page.getByTestId("markdown")).toBeVisible();
  await expect(page.getByRole("region", { name: "Palette" })).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Reference" })).toHaveCount(0);
  await expect(page.getByRole("separator", { name: "Resize explorer and palette" })).toHaveCount(0);
});
