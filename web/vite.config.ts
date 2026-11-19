import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The editor is embedded into runnel.exe: the build goes to internal/webui/dist (go:embed).
const outDir = fileURLToPath(new URL("../internal/webui/dist", import.meta.url));

// Keeps the placeholder that makes go:embed work in checkouts without a frontend build.
const keepPlaceholder: Plugin = {
  name: "runnel-keep-placeholder",
  closeBundle() {
    writeFileSync(`${outDir}/.gitkeep`, "");
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss(), keepPlaceholder],
  build: {
    outDir,
    emptyOutDir: true,
    assetsInlineLimit: 0, // fonts and icons as files, so that the CSP needs no data: URLs for them
  },
  server: {
    // design/ (tokens, fonts, logos) lies outside web/.
    fs: { allow: [".."] },
    // "npm run dev" talks to a running "runnel serve" (port 7300).
    proxy: { "/api": "http://127.0.0.1:7300" },
  },
});
