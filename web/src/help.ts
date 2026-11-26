import { useEffect, useState } from "react";
import { fetchVersion } from "./api";
import { fetchPlugins } from "./plugins";
import { useCatalogVersion, type CatalogKind, type Schema } from "./catalog";

/** A request to show the help of a type (mockup E15c), e.g. from the palette; seq changes with every request. */
export interface HelpRequest {
  type: string;
  kind: CatalogKind;
  seq: number;
}

// The palette is outside of the file view that shows the help: requests go through here.
const listeners = new Set<(r: HelpRequest) => void>();
let seq = 0;

/** Asks the active file view to show the help of a type. */
export function requestHelp(type: string, kind: CatalogKind) {
  const r = { type, kind, seq: ++seq };
  for (const l of listeners) l(r);
}

/** Calls on for every help request while enabled (the active file view of a workflow or pipeline). */
export function useHelpRequests(enabled: boolean, on: (r: HelpRequest) => void) {
  useEffect(() => {
    if (!enabled) return;
    listeners.add(on);
    return () => {
      listeners.delete(on);
    };
  }, [enabled, on]);
}

/** A row of the table of fields: a field under with:, or a field of its entries (depth > 0). */
export interface HelpField {
  name: string;
  schema: Schema;
  required: boolean;
  depth: number;
  /** For a sub-field: where it is, e.g. "In each entry of aggregates". */
  where?: string;
}

/** The fields of a schema with their sub-fields (lists of objects, objects) right after them. */
export function helpFields(s: Schema | undefined, depth = 0): HelpField[] {
  return Object.entries(s?.properties ?? {}).flatMap(([name, f]) => {
    const row: HelpField = { name, schema: f, required: s?.required?.includes(name) ?? false, depth };
    const list = f.type === "array" && f.items?.properties;
    const inner = list ? f.items : f.type === "object" && f.properties ? f : undefined;
    const children = helpFields(inner, depth + 1).map((c) =>
      c.depth === depth + 1 ? { ...c, where: list ? `In each entry of ${name}` : `In ${name}` } : c,
    );
    return [row, ...children];
  });
}

const formatTypes: Record<string, string> = {
  "runnel-field": "field",
  "runnel-info-field": "field",
  "runnel-connection": "connection",
  "runnel-duration": "duration",
};

/** A readable type of a field (decision for mockup E15c): "list of objects", "field", "connection" … */
export function helpType(s: Schema): string {
  if (s.format && formatTypes[s.format]) return formatTypes[s.format];
  switch (s.type) {
    case "array": {
      const item = s.items ? helpType(s.items) : "any";
      return item === "any" ? "list" : `list of ${item.startsWith("list") ? "lists" : item + "s"}`;
    }
    case "object":
      return s.properties ? "object" : "map";
    case undefined:
      return "any";
  }
  return s.type;
}

/** A value as written in YAML: texts as they are, quoted when they start or end with a space. */
export function showValue(v: unknown): string {
  if (typeof v === "string") return v === "" || v.trim() !== v ? JSON.stringify(v) : v;
  return JSON.stringify(v);
}

/** The example of the help: the whole step (id, type, with) as in E14. */
export function exampleStep(id: string, type: string, example?: string): string {
  return `- id: ${id}\n  type: ${type}\n` + (example ? "  with:\n" + example.replace(/\n$/, "").replace(/^/gm, "    ") + "\n" : "");
}

let version: Promise<string> | null = null;
const pluginVersions = new Map<string, Promise<string>>();

/** The version of Runnel for "Built-in · <version>"; empty until loaded. */
export function useVersion(): string {
  const [v, setV] = useState("");
  useEffect(() => {
    let live = true;
    version ??= fetchVersion().then((x) => x.version);
    version.then(
      (x) => live && setV(x),
      () => (version = null),
    );
    return () => {
      live = false;
    };
  }, []);
  return v;
}

/** The version of a plugin for its badge; empty until loaded or without plugin. */
export function usePluginVersion(name?: string): string {
  const [v, setV] = useState("");
  // A plugin may change (reload, S52): its catalog is loaded again, and so is its version.
  const catalog = useCatalogVersion();
  useEffect(() => {
    setV("");
    if (!name) return;
    let live = true;
    const key = `${name}@${catalog}`;
    let p = pluginVersions.get(key);
    if (!p) {
      p = fetchPlugins().then((i) => i.plugins.find((x) => x.name === name)?.version ?? "");
      pluginVersions.set(key, p);
    }
    p.then(
      (x) => live && setV(x),
      () => pluginVersions.delete(key),
    );
    return () => {
      live = false;
    };
  }, [name, catalog]);
  return v;
}
