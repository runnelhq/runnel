// Starts "runnel serve" with the given arguments and a run history (ADR 0026) in a
// temporary folder, removed when the server stops: tests start without old runs and
// leave nothing in the user's data folder. Usage: node serve.mjs <serve arguments…>
import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const repo = fileURLToPath(new URL("../..", import.meta.url));
const history = mkdtempSync(join(tmpdir(), "runnel-e2e-history-"));
const binary = join(repo, "bin", process.platform === "win32" ? "runnel.exe" : "runnel");
const server = spawn(binary, ["serve", ...process.argv.slice(2), "--history-dir", history], { stdio: "inherit" });
const stop = () => server.kill("SIGINT");
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
server.on("exit", (code) => {
  rmSync(history, { recursive: true, force: true });
  process.exit(code ?? 0);
});
