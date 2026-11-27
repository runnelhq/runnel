// Starts "runnel serve" in a new empty folder "My Flows" without a project (R10), so that
// end-to-end tests can create the project from the start page. The folder and the run
// history are removed when the server stops. Usage: node serve-empty.mjs <port>
import { spawn } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const port = process.argv[2] ?? "7396";
const repo = fileURLToPath(new URL("../..", import.meta.url));
const base = mkdtempSync(join(tmpdir(), "runnel-e2e-empty-"));
const dir = join(base, "My Flows");
mkdirSync(dir);

const binary = join(repo, "bin", process.platform === "win32" ? "runnel.exe" : "runnel");
const server = spawn(binary, ["serve", dir, "--port", port, "--triggers=", "--history-dir", join(base, "history")], { stdio: "inherit" });
const stop = () => server.kill("SIGINT");
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
server.on("exit", (code) => {
  rmSync(base, { recursive: true, force: true });
  process.exit(code ?? 0);
});
