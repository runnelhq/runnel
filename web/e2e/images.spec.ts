import { expect, test, type Page } from "@playwright/test";

// Images and binary files in the editor (R8): images are shown with their dimensions and
// size instead of as text, binary files (by extension or content) show a notice.

const tree = (page: Page) => page.getByRole("tree", { name: "Project files" });

test.describe("images", () => {
  // examples/pipelines with docs/customers.png (1040 × 140), only read.
  test.use({ baseURL: "http://127.0.0.1:7397" });

  test("a PNG opens as image with its dimensions and size, scaled down to the view", async ({ page }) => {
    await page.goto("/");
    await tree(page).getByText("customers.png", { exact: true }).click();
    await expect(page.getByRole("tab", { name: /customers\.png/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("kind")).toHaveText("Image");
    const img = page.getByTestId("image");
    await expect(img).toHaveAttribute("src", /^blob:/);
    await expect(page.getByTestId("image-info")).toHaveText(/^1040 × 140 px · 34\.1 KB$/);
    await expect(page.getByTestId("file-summary")).toHaveText("Image · 1040 × 140 px · 34.1 KB");
    await expect(page.getByTestId("yaml-editor")).toHaveCount(0);
    // At most its natural size, never scaled up.
    expect((await img.boundingBox())!.width).toBeLessThanOrEqual(1040);
    // In a narrow window it is scaled down to fit.
    await page.setViewportSize({ width: 900, height: 700 });
    await expect.poll(async () => (await img.boundingBox())!.width).toBeLessThan(900);
  });
});

test.describe("files that are not text", () => {
  test.use({ baseURL: "http://127.0.0.1:7398" });
  // One worker for both tests: the files are created once and removed after the last one.
  test.describe.configure({ mode: "serial" });
  const blob = "docs/e2e-blob.dat";
  const svg = "docs/e2e-icon.svg";

  test.beforeAll(async ({ request }) => {
    await request.put(`/api/v1/files/${blob}`, { headers: { "If-Match": "*" }, data: Buffer.from([0x52, 0x4e, 0x00, 0x01, 0xff, 0x00, 0x7f]) });
    await request.put(`/api/v1/files/${svg}`, {
      headers: { "If-Match": "*" },
      data: '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="16" onload="alert(1)"><script>alert(2)</script><rect width="24" height="16"/></svg>',
    });
  });
  test.afterAll(async ({ request }) => {
    for (const p of [blob, svg]) await request.delete(`/api/v1/files/${p}`, { headers: { "If-Match": "*" } });
  });

  test("a binary file of unknown type shows a notice, not the text editor", async ({ page }) => {
    await page.goto("/");
    await tree(page).getByText("e2e-blob.dat", { exact: true }).click();
    const notice = page.getByTestId("binary-notice");
    await expect(notice).toContainText("Binary file – can't be shown");
    await expect(notice).toContainText(blob);
    await expect(page.getByTestId("yaml-editor")).toHaveCount(0);
    await expect(page.getByTestId("file-summary")).toHaveCount(0);
  });

  test("an SVG is shown as image and runs no scripts", async ({ page }) => {
    const dialogs: string[] = [];
    page.on("dialog", (d) => {
      dialogs.push(d.message());
      void d.dismiss();
    });
    await page.goto("/");
    await tree(page).getByText("e2e-icon.svg", { exact: true }).click();
    await expect(page.getByTestId("image-info")).toHaveText(/^24 × 16 px · \d+ B$/);
    await expect(page.getByTestId("yaml-editor")).toHaveCount(0);
    // Opened directly, the sandbox of the file API blocks its scripts as well.
    await page.goto(`/api/v1/files/${svg}`);
    await page.waitForLoadState("load");
    expect(dialogs).toEqual([]);
  });
});
