import { expect, test, type Page } from "@playwright/test";
import { editorText } from "./editor";

// Markdown documents (mockup design/mockups/e17-markdown.html): the formatted view, links to
// files of the project and to the web, images from the project only, no HTML, the text view
// and "New → Document".

const tree = (page: Page) => page.getByRole("tree", { name: "Project files" });

test.describe("the README of a project", () => {
  // examples/pipelines with its README.md, on a server of its own (playwright.config.ts).
  test.use({ baseURL: "http://127.0.0.1:7397" });

  test("opens at the start and links to the files of the project", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("tab", { name: /README\.md/ })).toHaveAttribute("aria-selected", "true");
    const doc = page.getByTestId("markdown");
    await expect(doc.getByRole("heading", { level: 1 })).toHaveText("pipelines");
    await expect(page.getByTestId("file-summary")).toContainText("Markdown · UTF-8");
    // The image comes from the project.
    const img = doc.getByRole("img", { name: "The pipeline customers" });
    await expect(img).toHaveAttribute("src", "/api/v1/files/docs/customers.png");
    await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.naturalWidth)).toBeGreaterThan(0);
    // Links to the web open in a new tab.
    const web = doc.getByRole("link", { name: "Runnel on GitHub" });
    await expect(web).toHaveAttribute("target", "_blank");
    await expect(web).toHaveAttribute("rel", "noopener noreferrer");
    // A link to a file opens it in the editor.
    const link = doc.getByRole("link", { name: "customers", exact: true });
    await expect(link).toHaveAttribute("title", "Open pipelines/customers.pipeline.yaml");
    await expect(link.locator("svg")).toHaveCount(1);
    await link.click();
    await expect(page.getByRole("tab", { name: /customers\.pipeline\.yaml/ })).toHaveAttribute("aria-selected", "true");
  });
});

test.describe("documents", () => {
  test.use({ baseURL: "http://127.0.0.1:7398" });
  test.describe.configure({ mode: "serial" });
  const path = "docs/e2e-notes.md";
  const text = `# Notes

Runs [hello](../workflows/hello.workflow.yaml); errors: [errors](errors.md).
<b onclick="alert(1)">Important</b> and [no script](javascript:alert(1)).

![status](https://example.com/status.png)
`;

  test.beforeEach(async ({ request }) => {
    await request.put(`/api/v1/files/${path}`, { headers: { "If-Match": "*" }, data: text });
  });

  test("marks missing files and shows HTML as text", async ({ page }) => {
    await page.goto("/");
    await tree(page).getByText("e2e-notes.md", { exact: true }).click();
    const doc = page.getByTestId("markdown");
    await expect(doc.getByRole("link", { name: "hello" })).toHaveAttribute("title", "Open workflows/hello.workflow.yaml");
    await expect(doc.getByRole("link", { name: /errors/ })).toHaveAttribute("title", "✖ docs/errors.md does not exist");
    await expect(doc).toContainText('<b onclick="alert(1)">Important</b>');
    await expect(doc.locator("b")).toHaveCount(0);
    await expect(doc.getByRole("link", { name: "no script" })).toHaveCount(0);
    await expect(doc).toContainText("Image not shown: only images of the project are loaded (https://example.com/status.png)");
    await expect(doc.locator("img")).toHaveCount(0);
  });

  test("edits the text and saves it", async ({ page }) => {
    await page.goto("/");
    await tree(page).getByText("e2e-notes.md", { exact: true }).click();
    await page.getByRole("button", { name: "Text", exact: true }).click();
    const editor = page.getByRole("textbox", { name: "Markdown" });
    await editor.click();
    await page.keyboard.press("ControlOrMeta+End");
    await page.keyboard.type("\n## Added\n");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    expect(await (await page.request.get(`/api/v1/files/${path}`)).text()).toContain("\n## Added\n");
    expect(await editorText(page)).toContain("## Added");
    await page.getByRole("button", { name: "Preview", exact: true }).click();
    await expect(page.getByTestId("markdown").getByRole("heading", { name: "Added" })).toBeVisible();
  });

  test("New → Document creates one under docs/", async ({ page, request }) => {
    await request.delete("/api/v1/files/docs/e2e-guide.md");
    await page.goto("/");
    await page.getByRole("button", { name: "New", exact: true }).click();
    await page.getByRole("menu", { name: "New" }).getByRole("menuitem", { name: /^Document/ }).click();
    const input = tree(page).getByRole("textbox", { name: "Name of the new document" });
    await input.fill("e2e-guide");
    await input.press("Enter");
    await expect(page.getByRole("tab", { name: /e2e-guide\.md/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("markdown").getByRole("heading", { level: 1 })).toHaveText("e2e-guide");
    expect(await (await request.get("/api/v1/files/docs/e2e-guide.md")).text()).toBe("# e2e-guide\n");
  });
});
