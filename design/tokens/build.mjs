// Erzeugt runnel.css und tailwind.css aus tokens.json.
// Aufruf: node design/tokens/build.mjs   (Node ≥ 18, keine Abhängigkeiten)
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const tokens = JSON.parse(readFileSync(join(dir, "tokens.json"), "utf8"));
const header = "/* Automatisch erzeugt aus design/tokens/tokens.json – nicht von Hand bearbeiten. */\n";

const entries = (obj) => Object.entries(obj).filter(([k]) => !k.startsWith("$"));
const value = (t) => {
  const v = t.$value;
  if (Array.isArray(v) && t.$type === "fontFamily") return v.map((f) => (/\s/.test(f) ? `"${f}"` : f)).join(", ");
  if (Array.isArray(v) && t.$type === "cubicBezier") return `cubic-bezier(${v.join(", ")})`;
  return String(v);
};
// "{color.strom.600}" -> var(--rn-strom-600)
const ref = (s) =>
  s.replace(/\{color\.([\w-]+)\.([\w-]+)\}/g, (_, group, step) => `var(--rn-${group}-${step})`);

const lines = [];
const decl = (name, v) => lines.push(`  --${name}: ${v};`);

// 1) Basis-Tokens (modusunabhängig)
lines.push(":root {");
for (const [group, scale] of entries(tokens.color)) for (const [step, t] of entries(scale)) decl(`rn-${group}-${step}`, value(t));
for (const [k, t] of entries(tokens.font)) decl(`rn-font-${k}`, value(t));
for (const [k, t] of entries(tokens.fontWeight)) decl(`rn-font-weight-${k}`, value(t));
for (const [k, t] of entries(tokens.fontSize)) decl(`rn-text-${k}`, value(t));
for (const [k, t] of entries(tokens.radius)) decl(`rn-radius-${k}`, value(t));
for (const [k, t] of entries(tokens.space)) decl(`rn-space-${k}`, value(t));
for (const [k, t] of entries(tokens.shadow)) decl(`rn-shadow-${k}`, value(t));
for (const [k, t] of entries(tokens.motion)) decl(`rn-motion-${k}`, value(t));
decl("radius", "var(--rn-radius-md)");
lines.push("");
lines.push("  /* Semantische Rollen – hell (Standard) */");
lines.push("  color-scheme: light;");
for (const [k, v] of entries(tokens.theme.light)) decl(k, ref(v));
lines.push("}", "");

// 2) Dunkler Modus: Klasse .dark (shadcn/ui), data-theme="dark" oder data-theme="system" + Systemeinstellung
const dark = ["  color-scheme: dark;", ...entries(tokens.theme.dark).map(([k, v]) => `  --${k}: ${ref(v)};`)];
lines.push(".dark,", '[data-theme="dark"] {', ...dark, "}", "");
lines.push("@media (prefers-color-scheme: dark) {", '  [data-theme="system"] {', ...dark.map((l) => "  " + l), "  }", "}", "");

writeFileSync(join(dir, "runnel.css"), header + lines.join("\n"));

// 3) Tailwind v4: Tokens als Utilities verfügbar machen (bg-primary, text-strom-600, font-mono …)
const tw = [];
tw.push('@import "./runnel.css";', "");
tw.push("@custom-variant dark (&:where(.dark, .dark *, [data-theme=dark], [data-theme=dark] *));", "");
tw.push("@theme inline {");
for (const [group, scale] of entries(tokens.color))
  for (const [step] of entries(scale)) tw.push(`  --color-${group}-${step}: var(--rn-${group}-${step});`);
for (const k of Object.keys(tokens.theme.light)) tw.push(`  --color-${k}: var(--${k});`);
tw.push("  --font-sans: var(--rn-font-sans);", "  --font-mono: var(--rn-font-mono);");
for (const [k] of entries(tokens.radius)) if (k !== "full") tw.push(`  --radius-${k}: var(--rn-radius-${k});`);
for (const [k] of entries(tokens.shadow)) tw.push(`  --shadow-${k}: var(--rn-shadow-${k});`);
tw.push("  --ease-flow: var(--rn-motion-ease);");
tw.push("}", "");
writeFileSync(join(dir, "tailwind.css"), header + tw.join("\n"));
console.log("runnel.css und tailwind.css erzeugt");
