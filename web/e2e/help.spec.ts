import { expect, test, type Page } from "@playwright/test";

// Help per building block (mockup design/mockups/e15-help.html, E15d): opened from the
// palette ("?", F1), from the properties panel ("?", "Open help", F1), back with
// "‹ Properties" or Esc, the switch between action and transform, a plugin without
// details and "Copy example". Against "runnel serve ../examples/hello"; nothing is changed.
test.use({ permissions: ["clipboard-read", "clipboard-write"] });

const help = (page: Page) => page.getByTestId("help-panel");
const properties = (page: Page) => page.getByRole("region", { name: "Properties" });

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("hello.workflow.yaml", { exact: true }).click();
  await expect(page.getByTestId("node-greet")).toBeVisible();
}

async function select(page: Page, id: string) {
  await page.getByTestId(`node-${id}`).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue(id);
}

test("opens from the palette with the whole reference; × closes it", async ({ page }) => {
  await open(page);
  const tile = page.getByTestId("palette-file.list");
  await expect(tile).toHaveAttribute("title", /\? or F1 for help$/);
  await tile.hover();
  await tile.getByRole("button", { name: "Help for file.list" }).click();

  await expect(properties(page)).toHaveCount(0);
  await expect(help(page).getByRole("heading", { name: "List Files" })).toBeVisible();
  // From the palette there is no way back to the properties, only ×.
  await expect(help(page).getByText("Help", { exact: true })).toBeVisible();
  await expect(help(page).getByRole("button", { name: "Properties" })).toHaveCount(0);
  await expect(page.getByTestId("help-badges")).toContainText("Action");
  await expect(page.getByTestId("help-badges")).toContainText("Files");
  await expect(page.getByTestId("help-badges")).toContainText("Built-in");

  // Fields with required, defaults and allowed values; the sections of the descriptor.
  await expect(page.getByTestId("help-field-folder")).toContainText("required");
  await expect(page.getByTestId("help-field-pattern")).toContainText("*");
  await expect(page.getByTestId("help-field-include")).toContainText("filesfoldersall");
  const bar = help(page).getByRole("navigation", { name: "Sections" });
  await expect(bar.locator("> button")).toHaveText(["Fields4", "Output5", "Errors", "Limits1", "Example"]);
  await expect(page.getByTestId("help-section-output")).toContainText("output[].modified");
  await expect(page.getByTestId("help-section-records")).toHaveCount(0);
  // No "Show … in YAML": the help is not that of the selected step.
  await expect(help(page).getByRole("button", { name: /in YAML/ })).toHaveCount(0);

  // A jump link scrolls to its section and marks it.
  await bar.getByRole("button", { name: "Errors" }).click();
  await expect(bar.getByRole("button", { name: "Errors" })).toHaveAttribute("aria-current", "true");

  // The transform named in the limits opens its help.
  await page.getByTestId("help-section-limits").getByRole("button", { name: "file.list" }).click();
  await expect(page.getByTestId("help-badges")).toContainText("Transform");
  await expect(page.getByTestId("help-section-records")).toBeVisible();

  await help(page).getByRole("button", { name: "Close help" }).click();
  await expect(help(page)).toHaveCount(0);
  await expect(properties(page)).toBeVisible();
});

test("opens from the panel; ‹ Properties and Esc go back", async ({ page }) => {
  await open(page);
  await select(page, "greet");
  await properties(page).getByRole("button", { name: "Help for core.log" }).click();
  await expect(help(page).getByRole("heading", { name: "Log" })).toBeVisible();
  await expect(page.getByTestId("help-example")).toContainText("- id: greet\n  type: core.log\n");
  await expect(help(page).getByRole("button", { name: "Show greet in YAML" })).toBeVisible();
  await help(page).getByRole("button", { name: "Properties" }).click();
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("greet");

  await properties(page).getByRole("button", { name: /^Open help/ }).click();
  await expect(help(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(help(page)).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "ID", exact: true })).toHaveValue("greet");

  // "Show <id> in YAML" goes to the step in the YAML view.
  await properties(page).getByRole("button", { name: /^Open help/ }).click();
  await help(page).getByRole("button", { name: "Show greet in YAML" }).click();
  await expect(page.getByTestId("yaml-editor")).toBeVisible();
});

test("F1 opens the help of the selected step and of the palette tile in focus", async ({ page }) => {
  await open(page);
  await select(page, "pause");
  await page.keyboard.press("F1");
  await expect(help(page).getByRole("heading", { name: "Wait" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(help(page)).toHaveCount(0);

  await page.getByTestId("palette-file.read").getByRole("button").focus();
  await page.keyboard.press("F1");
  await expect(help(page).getByRole("heading", { name: "Read File" })).toBeVisible();
});

test("switches between action and transform of the same type", async ({ page }) => {
  await open(page);
  await select(page, "greet");
  await page.keyboard.press("F1");
  const pair = help(page).getByRole("group", { name: "Same type as" });
  await expect(pair.getByRole("button", { name: "Action" })).toHaveAttribute("aria-pressed", "true");
  await pair.getByRole("button", { name: "Transform" }).click();
  await expect(page.getByTestId("help-badges")).toContainText("Transform");
  await expect(page.getByTestId("help-section-records")).toBeVisible();
  // The transform is not the selected step.
  await expect(help(page).getByRole("button", { name: /in YAML/ })).toHaveCount(0);
  await pair.getByRole("button", { name: "Action" }).click();
  await expect(help(page).getByRole("button", { name: "Show greet in YAML" })).toBeVisible();
  // A type that exists only as an action has no switch.
  await page.getByTestId("palette-core.wait").getByRole("button").focus();
  await page.keyboard.press("F1");
  await expect(help(page).getByRole("heading", { name: "Wait" })).toBeVisible();
  await expect(help(page).getByRole("group", { name: "Same type as" })).toHaveCount(0);
});

test("shows sub-fields, conditions and allowed values", async ({ page }) => {
  await open(page);
  await page.getByTestId("palette-http.request").hover();
  await page.getByTestId("palette-http.request").getByRole("button").click();
  await expect(help(page).getByRole("heading", { name: "HTTP Request" })).toBeVisible();
  // Sub-fields of an object right after it, with the condition of x-runnel-only.
  await expect(page.getByTestId("help-field-maxRetries")).toContainText("└maxRetries");
  await expect(page.getByTestId("help-field-start")).toContainText("Only with style: page, offset");
  await expect(page.getByTestId("help-field-connection")).toContainText("connection");
  await expect(page.getByTestId("help-field-timeout")).toContainText("duration");
  await expect(page.getByTestId("help-field-method")).toContainText("GETPOSTPUTPATCHDELETEHEAD");
});

test("a plugin without the new keys shows a neutral note", async ({ page }) => {
  // The catalog with an action of a plugin that describes only its fields.
  await page.route("**/api/v1/catalog", async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { actions: unknown[] };
    body.actions.push({
      type: "iban.check",
      kind: "action",
      category: "",
      plugin: "iban",
      label: "Check IBAN",
      description: "Checks one IBAN and returns bank and BIC",
      schema: { type: "object", properties: { iban: { type: "string", description: "The IBAN to check" } }, required: ["iban"] },
    });
    await route.fulfill({ response: res, json: body });
  });
  await open(page);
  await page.getByTestId("palette-iban.check").hover();
  await page.getByTestId("palette-iban.check").getByRole("button").click();
  await expect(help(page).getByRole("heading", { name: "Check IBAN" })).toBeVisible();
  await expect(page.getByTestId("help-badges")).toContainText("General");
  await expect(page.getByTestId("help-badges")).toContainText("Plugin iban");
  await expect(help(page)).toContainText("No more details from the plugin.");
  await expect(help(page).getByRole("navigation", { name: "Sections" }).locator("> button")).toHaveText(["Fields1"]);
  await expect(page.getByTestId("help-example")).toHaveCount(0);
});

test("copies the example", async ({ page }) => {
  await open(page);
  await select(page, "pause");
  await page.keyboard.press("F1");
  await help(page).getByRole("button", { name: "Copy example" }).click();
  await expect(help(page).getByRole("button", { name: "Copied" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe("- id: pause\n  type: core.wait\n  with:\n    duration: 30s\n");
});
