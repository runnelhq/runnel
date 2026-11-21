import type { Page } from "@playwright/test";

/**
 * The whole text of the YAML editor (E9). The editor draws only the lines in view, so the
 * text comes from its state: CodeMirror keeps its view at the content element (the same
 * way EditorView.findFromDOM finds it; @codemirror/view is pinned in package.json).
 */
export async function editorText(page: Page): Promise<string> {
  return page
    .getByTestId("yaml-editor")
    .locator(".cm-content")
    .evaluate((el) => (el as unknown as { cmTile: { root: { view: { state: { doc: { toString(): string } } } } } }).cmTile.root.view.state.doc.toString());
}
