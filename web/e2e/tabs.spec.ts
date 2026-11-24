import { expect, test, type Page } from "@playwright/test";

// Many open files (mockup E21): readable tabs, the list of all open files with search,
// the menu of a tab with closing several files and "Reveal in explorer". Against
// "runnel serve ../examples/hello"; nothing is saved.

const files = ["dev.yaml", "prod.yaml", "answer.yaml", "greet.yaml", "hourly.yaml", "morning.yaml", "hello.workflow.yaml", "parallel.workflow.yaml", "project.yaml"];

async function openAll(page: Page) {
  await page.setViewportSize({ width: 1000, height: 800 });
  await page.goto("/");
  for (const name of files) await page.getByRole("tree").getByText(name, { exact: true }).click();
  await expect(tabs(page)).toHaveCount(files.length);
}
const bar = (page: Page) => page.getByRole("tablist", { name: "Open files" });
const tabs = (page: Page) => bar(page).getByRole("tab");
const tab = (page: Page, name: string) => bar(page).getByRole("tab", { name: new RegExp(`^${name.replace(".", "\\.")}`) });

test("tabs stay readable and the list goes to any open file", async ({ page }) => {
  await openAll(page);
  // At least 160 px: the names are readable instead of three letters.
  for (const w of await tabs(page).evaluateAll((els) => els.map((e) => e.getBoundingClientRect().width))) expect(w).toBeGreaterThanOrEqual(160);
  const all = page.getByRole("button", { name: "All open files (9)" });
  await expect(all).toContainText("9");

  // Ctrl+Shift+L opens the list; typing filters, Enter opens.
  await page.keyboard.press("Control+Shift+L");
  const list = page.getByRole("dialog", { name: "Open files" });
  await expect(list.getByRole("option")).toHaveCount(files.length);
  await expect(list.getByRole("option", { selected: true })).toContainText("project.yaml"); // the active file
  await list.getByRole("textbox", { name: "Go to an open file" }).fill("par");
  await expect(list).toContainText("1 of 9");
  await page.keyboard.press("Enter");
  await expect(list).toHaveCount(0);
  await expect(tab(page, "parallel.workflow.yaml")).toHaveAttribute("aria-selected", "true");
  await expect(tab(page, "parallel.workflow.yaml")).toBeInViewport({ ratio: 1 });

  // The button opens it too; × closes a file, Esc the list.
  await all.click();
  await list.getByRole("button", { name: "Close dev.yaml" }).click();
  await expect(list.getByRole("option")).toHaveCount(files.length - 1);
  await page.keyboard.press("Escape");
  await expect(list).toHaveCount(0);
  await expect(tabs(page)).toHaveCount(files.length - 1);
});

// Enter right after typing opens the first match: the selection follows the typing at
// once, not only after the list has been drawn again.
test("Enter right after typing opens the first match", async ({ page }) => {
  await openAll(page);
  const list = page.getByRole("dialog", { name: "Open files" });
  for (const [typed, name] of [["hel", "hello.workflow.yaml"], ["dev", "dev.yaml"], ["morn", "morning.yaml"]]) {
    await page.keyboard.press("Control+Shift+L");
    await page.keyboard.type(typed, { delay: 0 });
    await page.keyboard.press("Enter");
    await expect(list).toHaveCount(0);
    await expect(tab(page, name)).toHaveAttribute("aria-selected", "true");
  }
  // No match: Enter keeps the list open; Escape closes it.
  await page.keyboard.press("Control+Shift+L");
  await page.keyboard.type("nothing");
  await page.keyboard.press("Enter");
  await expect(list).toContainText("No open file matches");
  await page.keyboard.press("Escape");
  await expect(list).toHaveCount(0);
});

test("the menu of a tab closes others, those to the right and all", async ({ page }) => {
  await openAll(page);
  await tab(page, "greet.yaml").click({ button: "right" });
  const menu = page.getByRole("menu", { name: "Actions for greet.yaml" });
  await expect(menu.getByRole("menuitem", { name: /Close saved/ })).toContainText("9 files");
  await menu.getByRole("menuitem", { name: "Close to the right" }).click();
  await expect(tabs(page)).toHaveText(["dev.yaml", "prod.yaml", "answer.yaml", "greet.yaml"]);
  await tab(page, "prod.yaml").click({ button: "right" });
  await page.getByRole("menuitem", { name: "Close others" }).click();
  await expect(tabs(page)).toHaveText(["prod.yaml"]);
  await expect(tab(page, "prod.yaml")).toHaveAttribute("aria-selected", "true");
  await tab(page, "prod.yaml").click({ button: "right" });
  await page.getByRole("menuitem", { name: "Close all" }).click();
  await expect(page.getByText("No file open")).toBeVisible();
});

test("closing several files asks only for those with unsaved changes", async ({ page }) => {
  await openAll(page);
  // An unsaved change in hello.workflow.yaml (YAML view).
  await tab(page, "hello.workflow.yaml").click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await page.getByTestId("yaml-editor").locator(".cm-line").first().click();
  await page.keyboard.press("End");
  await page.keyboard.type(" ");
  await expect(tab(page, "hello.workflow.yaml")).toHaveAttribute("data-dirty", "true");

  // Close saved leaves it open.
  await tab(page, "dev.yaml").click({ button: "right" });
  await page.getByRole("menuitem", { name: /Close saved/ }).click();
  await expect(tabs(page)).toHaveText(["hello.workflow.yaml●"]);

  // Close all asks for it; Cancel keeps it.
  await page.getByRole("tree").getByText("dev.yaml", { exact: true }).click();
  await tab(page, "dev.yaml").click({ button: "right" });
  await page.getByRole("menuitem", { name: "Close all" }).click();
  const ask = page.getByRole("alertdialog", { name: "Save changes?" });
  await expect(ask).toContainText("workflows/hello.workflow.yaml");
  await expect(tabs(page)).toHaveCount(1); // dev.yaml closed at once
  await ask.getByRole("button", { name: "Cancel" }).click();
  await expect(tabs(page)).toHaveCount(1);
  await page.getByRole("button", { name: "All open files (1)" }).click();
  await page.getByRole("button", { name: "Close all…" }).click();
  await ask.getByRole("button", { name: "Don’t save" }).click();
  await expect(page.getByText("No file open")).toBeVisible();
});

test("reveals a file in the explorer and copies its path", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await openAll(page);
  const tree = page.getByRole("tree");
  await tree.getByText("workflows", { exact: true }).click(); // collapse
  await expect(tree.getByText("hello.workflow.yaml", { exact: true })).toBeHidden();
  await tab(page, "hello.workflow.yaml").click({ button: "right" });
  await page.getByRole("menuitem", { name: "Reveal in explorer" }).click();
  await expect(tree.getByText("hello.workflow.yaml", { exact: true })).toBeVisible();
  await expect(tree.getByRole("treeitem", { name: "workflows" })).toHaveAttribute("aria-expanded", "true");

  await tab(page, "dev.yaml").click({ button: "right" });
  await page.getByRole("menuitem", { name: "Copy path" }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toMatch(/examples[\\/]hello[\\/]environments[\\/]dev\.yaml$/);
});
