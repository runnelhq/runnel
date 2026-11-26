import { expect, test } from "@playwright/test";

test("shows the project and the connection state", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveTitle("Runnel");
  await expect(page.getByTestId("project")).toContainText("hello");
  await expect(page.getByTestId("connection")).toHaveText("● Connected");
  await expect(page.getByText(/^Runnel (v|dev)/)).toBeVisible();
});

test("switches and remembers the theme", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");
  const html = page.locator("html");
  await expect(page.getByRole("button", { name: "System" })).toHaveAttribute("aria-pressed", "true");
  await expect(html).toHaveAttribute("data-theme", "light");

  // "System" follows the operating system setting.
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(html).toHaveAttribute("data-theme", "dark");

  await page.getByRole("button", { name: "Light" }).click();
  await expect(html).toHaveAttribute("data-theme", "light");
  await page.reload();
  await expect(html).toHaveAttribute("data-theme", "light");
  await expect(page.getByRole("button", { name: "Light" })).toHaveAttribute("aria-pressed", "true");
});

test("reports a lost connection", async ({ page }) => {
  await page.route("**/api/v1/events", (route) => route.abort());
  await page.goto("/");
  await expect(page.getByTestId("connection")).toHaveText("⚠ Disconnected");
  await expect(page.getByRole("alert")).toContainText("Connection to the Runnel server lost.");
});

test("uses the design tokens and the embedded fonts", async ({ page }) => {
  await page.goto("/");
  const font = await page.evaluate(() => getComputedStyle(document.body).fontFamily);
  expect(font).toContain("Inter");
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.fonts.check('13px "Inter"'))).toBe(true);
});

test("the tab bar has no scroll bar; the line between explorer and palette can be moved", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tree").getByText("hello.workflow.yaml").click();
  const tabs = page.getByRole("tablist", { name: "Open files" });
  await expect(page.getByRole("tab")).toHaveCount(1);
  expect(await tabs.evaluate((el) => el.scrollHeight <= el.clientHeight)).toBe(true);

  const pane = page.getByTestId("explorer-pane");
  const palette = page.getByRole("region", { name: "Palette" });
  const split = page.getByRole("separator", { name: "Resize explorer and palette" });
  // Start from the smallest explorer, so that the drag has room however many files the example has.
  const initial = (await pane.boundingBox())!;
  await split.focus();
  await page.keyboard.press("Home");
  await expect(pane).toHaveJSProperty("offsetHeight", 120);
  const before = (await pane.boundingBox())!;
  const paletteBefore = (await palette.boundingBox())!;
  const handle = (await split.boundingBox())!;
  await page.mouse.move(handle.x + 40, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.mouse.move(handle.x + 40, handle.y + 100, { steps: 5 });
  await page.mouse.up();
  const after = (await pane.boundingBox())!;
  expect(Math.round(after.height - before.height)).toBeGreaterThan(80);
  expect((await palette.boundingBox())!.height).toBeLessThan(paletteBefore.height - 80);

  // The height is remembered; the keyboard moves the line too; a double-click restores the default.
  await page.reload();
  await expect(pane).toHaveJSProperty("offsetHeight", Math.round(after.height));
  await split.focus();
  await page.keyboard.press("ArrowUp");
  await expect(pane).toHaveJSProperty("offsetHeight", Math.round(after.height) - 16);
  await split.dblclick();
  await expect.poll(async () => Math.round((await pane.boundingBox())!.height)).toBe(Math.round(initial.height));
});
