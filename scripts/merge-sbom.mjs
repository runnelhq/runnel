#!/usr/bin/env node
// Adds the editor (web/) to the CycloneDX SBOM of a runnel binary: the npm packages
// compiled into the editor bundle (from cyclonedx-npm, without dev dependencies) and
// the embedded fonts (design/fonts/). The Go SBOM file is updated in place.
//
// Usage: node scripts/merge-sbom.mjs <binary.cdx.json> <web.cdx.json>
import { readFileSync, writeFileSync } from "node:fs";

const [goFile, webFile] = process.argv.slice(2);
if (!goFile || !webFile) {
  console.error("usage: merge-sbom.mjs <binary.cdx.json> <web.cdx.json>");
  process.exit(2);
}
const bom = JSON.parse(readFileSync(goFile, "utf8"));
const web = JSON.parse(readFileSync(webFile, "utf8"));

// Font files copied from Fontsource (see design/fonts/fonts.css).
const fonts = [
  { name: "@fontsource/inter", version: "5.3.0", description: "Inter, Latin subset (woff2)" },
  { name: "@fontsource/jetbrains-mono", version: "5.3.0", description: "JetBrains Mono, Latin subset (woff2)" },
].map((f) => ({
  type: "library",
  "bom-ref": `runnel-editor-font|${f.name}@${f.version}`,
  name: f.name,
  version: f.version,
  description: f.description,
  licenses: [{ license: { id: "OFL-1.1" } }],
  purl: `pkg:npm/${f.name.replace("@", "%40")}@${f.version}`,
}));

const editor = { ...web.metadata.component, type: "application", description: "Runnel editor (embedded)" };
const refs = new Set(bom.components.map((c) => c["bom-ref"]));
for (const c of [editor, ...web.components, ...fonts]) {
  if (refs.has(c["bom-ref"])) throw new Error(`duplicate bom-ref ${c["bom-ref"]}`);
  refs.add(c["bom-ref"]);
  bom.components.push(c);
}

const deps = web.dependencies.map((d) => ({ ...d }));
const editorDeps = deps.find((d) => d.ref === editor["bom-ref"]);
editorDeps.dependsOn = [...(editorDeps.dependsOn ?? []), ...fonts.map((f) => f["bom-ref"])];
deps.push(...fonts.map((f) => ({ ref: f["bom-ref"] })));
bom.dependencies.push(...deps);

const main = bom.dependencies.find((d) => d.ref === bom.metadata.component["bom-ref"]);
main.dependsOn = [...(main.dependsOn ?? []), editor["bom-ref"]];

// cyclonedx-gomod writes the (older) list form of metadata.tools; add the npm tools in that form.
const webTools = web.metadata.tools?.components ?? [];
bom.metadata.tools = [
  ...(bom.metadata.tools ?? []),
  ...webTools.map((t) => ({ vendor: t.group ?? t.author, name: t.name, version: t.version })),
];
writeFileSync(goFile, JSON.stringify(bom, null, 2) + "\n");
console.log(`${goFile}: added the editor with ${web.components.length} npm packages and ${fonts.length} fonts`);
