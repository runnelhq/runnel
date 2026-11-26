import { expect, test, type Page } from "@playwright/test";

// Fix K13: the editor saves a file in the encoding it was opened with (UTF-8 with BOM,
// UTF-16 LE/BE, Windows-1252) instead of always as UTF-8. Against the server on a
// temporary copy of examples/hello (e2e/serve-copy.mjs).
test.use({ baseURL: "http://127.0.0.1:7398" });
test.describe.configure({ mode: "serial" });

const path = "workflows/encoded.workflow.yaml";
const fileURL = `/api/v1/files/${path}`;
const text = (name: string) =>
  `apiVersion: runnel/v1\nkind: Workflow\nname: ${name}\n# Grüße – 5 € für Müller\nactions:\n  - id: start\n    type: core.start\n`;

/** Windows-1252: Latin-1 plus € (0x80) and – (0x96) for the test text. */
const cp1252 = (s: string) => Buffer.from([...s].map((c) => (c === "€" ? 0x80 : c === "–" ? 0x96 : c.charCodeAt(0))));
const utf16be = (s: string) => Buffer.from(s, "utf16le").swap16();

const encodings: { label: string; bytes: (s: string) => Buffer }[] = [
  { label: "UTF-8 with BOM", bytes: (s) => Buffer.concat([Buffer.from([0xef, 0xbb, 0xbf]), Buffer.from(s, "utf8")]) },
  { label: "UTF-16 LE", bytes: (s) => Buffer.concat([Buffer.from([0xff, 0xfe]), Buffer.from(s, "utf16le")]) },
  { label: "UTF-16 BE", bytes: (s) => Buffer.concat([Buffer.from([0xfe, 0xff]), utf16be(s)]) },
  { label: "Windows-1252", bytes: cp1252 },
  { label: "UTF-8", bytes: (s) => Buffer.from(s, "utf8") },
];

test.afterAll(async ({ request }) => {
  await request.delete(fileURL, { headers: { "If-Match": "*" } });
});

async function open(page: Page) {
  await page.goto("/");
  await page.getByRole("tree").getByText("encoded.workflow.yaml", { exact: true }).click();
  await page.getByRole("button", { name: "YAML", exact: true }).click();
  await expect(page.getByTestId("yaml-editor")).toContainText("# Grüße – 5 € für Müller");
}

/** Types at the end of the line with the name of the workflow and waits until the editor shows it. */
async function typeAfterName(page: Page, typed: string) {
  const line = page.getByTestId("yaml-editor").locator(".cm-line", { hasText: "name: encoded" });
  await line.click();
  await page.keyboard.press("End");
  await page.keyboard.type(typed);
  await expect(line).toHaveText("name: encoded" + typed);
}

/** Compares bytes as hex, so that a difference shows where it is (and the text as Latin-1). */
function expectBytes(got: Buffer, want: Buffer, label: string) {
  expect(got.toString("hex"), `${label}: ${JSON.stringify(got.toString("latin1"))}`).toBe(want.toString("hex"));
}

for (const enc of encodings) {
  test(`keeps the encoding ${enc.label} when saving`, async ({ page, request }) => {
    expect((await request.put(fileURL, { headers: { "If-Match": "*" }, data: enc.bytes(text("encoded")) })).ok()).toBe(true);
    await open(page);
    await expect(page.getByTestId("file-summary")).toContainText(enc.label);
    await typeAfterName(page, "-ä");
    // Saved within the typing pause, before the tab shows "●": its absence alone does not
    // mean the save is done, so wait for the answer to the save.
    const saved = page.waitForResponse((r) => r.request().method() === "PUT" && r.url().endsWith(fileURL));
    await page.keyboard.press("Control+s");
    expect((await saved).ok()).toBe(true);
    await expect(page.getByRole("tab", { name: /encoded\.workflow\.yaml/ })).not.toContainText("●");
    expectBytes(Buffer.from(await (await request.get(fileURL)).body()), enc.bytes(text("encoded-ä")), enc.label);
  });
}

test("reports a character Windows-1252 cannot hold instead of saving", async ({ page, request }) => {
  const original = cp1252(text("encoded"));
  expect((await request.put(fileURL, { headers: { "If-Match": "*" }, data: original })).ok()).toBe(true);
  await open(page);
  await typeAfterName(page, "-Ж");
  await page.keyboard.press("Control+s");
  await expect(page.getByRole("alert")).toContainText(
    'Could not save: the character "Ж" (U+0416) in line 3, column 15 cannot be saved in Windows-1252',
  );
  // The file stays as it was; the changes stay unsaved.
  await expect(page.getByRole("tab", { name: /encoded\.workflow\.yaml/ })).toContainText("●");
  expectBytes(Buffer.from(await (await request.get(fileURL)).body()), original, "Windows-1252");
});
