// Starts "runnel serve" on a temporary copy of examples/hello, so that end-to-end tests
// can change and save files without touching the repository. Usage: node serve-copy.mjs <port>
import { spawn } from "node:child_process";
import { cpSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const port = process.argv[2] ?? "7398";
const repo = fileURLToPath(new URL("../..", import.meta.url));
const dir = mkdtempSync(join(tmpdir(), "runnel-e2e-"));
cpSync(join(repo, "examples", "hello"), dir, { recursive: true });

const binary = join(repo, "bin", process.platform === "win32" ? "runnel.exe" : "runnel");
const server = spawn(binary, ["serve", dir, "--port", port], { stdio: "inherit" });
const stop = () => server.kill("SIGINT");
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
server.on("exit", (code) => {
  rmSync(dir, { recursive: true, force: true });
  process.exit(code ?? 0);
});
