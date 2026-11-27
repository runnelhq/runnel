// Starts "runnel serve" on a temporary copy of examples/hello, so that end-to-end tests
// can change and save files without touching the repository. Usage: node serve-copy.mjs <port>
// [--secret-key] [serve arguments…]; --secret-key gives the local secret store a random key
// (RUNNEL_MASTER_KEY) in the configuration folder of the test (S39g2).
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { cpSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const port = process.argv[2] ?? "7398";
const repo = fileURLToPath(new URL("../..", import.meta.url));
const dir = mkdtempSync(join(tmpdir(), "runnel-e2e-"));
cpSync(join(repo, "examples", "hello"), dir, { recursive: true });

const binary = join(repo, "bin", process.platform === "win32" ? "runnel.exe" : "runnel");
// The run history (ADR 0026) goes to a folder of the copy that the explorer hides.
// Approvals of plugins (S52b1) go to a configuration folder of the test, not to that of the user.
const config = mkdtempSync(join(tmpdir(), "runnel-e2e-config-"));
// The local secret store (S39d) lives there too: XDG_CONFIG_HOME, on Windows LOCALAPPDATA.
const env = { ...process.env, XDG_CONFIG_HOME: config, APPDATA: config, LOCALAPPDATA: config };
const args = process.argv.slice(3);
if (args.includes("--secret-key")) env.RUNNEL_MASTER_KEY = randomBytes(32).toString("base64");
const extra = args.filter((a) => a !== "--secret-key");
const server = spawn(binary, ["serve", dir, "--port", port, "--history-dir", join(dir, ".history"), ...extra], { stdio: "inherit", env });
const stop = () => server.kill("SIGINT");
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
server.on("exit", (code) => {
  rmSync(dir, { recursive: true, force: true });
  rmSync(config, { recursive: true, force: true });
  process.exit(code ?? 0);
});
