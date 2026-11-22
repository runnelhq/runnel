import { defineConfig, devices } from "@playwright/test";

// End-to-end tests of the editor against a real "runnel serve" with examples/hello.
// The binary must have been built with the editor: "make e2e" does both.
const port = 7399;
/** Port of the server on a temporary copy of the example project (see e2e/serve-copy.mjs). */
const editPort = 7398;
/** Port of the server on examples/pipelines (documents, E17). */
const docsPort = 7397;
const binary = process.platform === "win32" ? "..\\bin\\runnel.exe" : "../bin/runnel";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: "off",
  },
  // A desktop screen (the editor targets desktop PCs, decision for mockup E4); with the
  // properties panel (E7), 1280 × 720 leaves the canvas too narrow for the examples at 100 %.
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } }],
  webServer: [
    {
      command: `${binary} serve ../examples/hello --port ${port}`,
      url: `http://127.0.0.1:${port}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
    },
    {
      // examples/pipelines with its README.md, which opens at the start (e2e/markdown.spec.ts);
      // only read, and on a server of its own so that other tests start without it.
      command: `${binary} serve ../examples/pipelines --port ${docsPort} --triggers=`,
      url: `http://127.0.0.1:${docsPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
    },
    {
      // A copy of the example project for tests that save files (e2e/edit.spec.ts).
      command: `node e2e/serve-copy.mjs ${editPort}`,
      url: `http://127.0.0.1:${editPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
    },
  ],
});
