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
