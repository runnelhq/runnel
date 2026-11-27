import { defineConfig, devices } from "@playwright/test";

// End-to-end tests of the editor against a real "runnel serve" with examples/hello.
// The binary must have been built with the editor: "make e2e" does both.
const port = 7399;
/** Port of the server on a temporary copy of the example project (see e2e/serve-copy.mjs). */
const editPort = 7398;
/** Port of the server on examples/pipelines (documents, E17). */
const docsPort = 7397;
/** Port of the server in an empty folder without project (start page, R10; see e2e/serve-empty.mjs). */
const emptyPort = 7396;
/** Port of the server on a copy of the example project with a key for the local secret store (S39g2). */
const secretsPort = 7395;
/** Port of the server on a copy of the example project for the data of each step (E23b), with a key for the local secret store. */
const dataPort = 7394;

/** Stop the servers with SIGTERM, so that the scripts in e2e/ remove their temporary folders. */
const gracefulShutdown = { signal: "SIGTERM" as const, timeout: 5_000 };

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
      // e2e/serve.mjs keeps the run history in a temporary folder (ADR 0026).
      command: `node e2e/serve.mjs ../examples/hello --port ${port}`,
      url: `http://127.0.0.1:${port}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown,
    },
    {
      // examples/pipelines with its README.md, which opens at the start (e2e/markdown.spec.ts);
      // only read, and on a server of its own so that other tests start without it.
      command: `node e2e/serve.mjs ../examples/pipelines --port ${docsPort} --triggers=`,
      url: `http://127.0.0.1:${docsPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown,
    },
    {
      // A copy of the example project for tests that save files (e2e/edit.spec.ts).
      command: `node e2e/serve-copy.mjs ${editPort}`,
      url: `http://127.0.0.1:${editPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown,
    },
    {
      // An empty folder without project: the start page creates one (e2e/start.spec.ts).
      command: `node e2e/serve-empty.mjs ${emptyPort}`,
      url: `http://127.0.0.1:${emptyPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown,
    },
    {
      // Setting and deleting values of the local secret store (e2e/secrets.spec.ts): a store
      // of its own in the configuration folder of the test, with a random key.
      command: `node e2e/serve-copy.mjs ${secretsPort} --secret-key --triggers=`,
      url: `http://127.0.0.1:${secretsPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown,
    },
    {
      // Runs whose step data the tab "Data" shows (e2e/data.spec.ts): a project of its own, so
      // that the last run started in the editor is that of the test, with a secret in the local store.
      command: `node e2e/serve-copy.mjs ${dataPort} --secret-key --triggers=`,
      url: `http://127.0.0.1:${dataPort}/api/v1/version`,
      reuseExistingServer: false,
      timeout: 30_000,
      gracefulShutdown,
    },
  ],
});
