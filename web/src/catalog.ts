import { useEffect, useState } from "react";
import { apiBase } from "./api";

/** A JSON Schema as generated from a configuration struct (ADR 0014); only what the editor needs. */
export interface Schema {
  type?: string;
  description?: string;
  format?: string;
  enum?: unknown[];
  default?: unknown;
  examples?: unknown[];
  properties?: Record<string, Schema>;
  required?: string[];
  items?: Schema;
  /** The property applies only when a sibling has one of these values (x-runnel-only, E14). */
  "x-runnel-only"?: Record<string, unknown[]>;
}

/** Descriptor of an action or transform type (GET /catalog, ADR 0014). */
export interface Descriptor {
  type: string;
  category: string;
  label: string;
  description: string;
  schema?: Schema;
  /** Transforms: can pass records to an error link. */
  recordErrors?: boolean;
  /** Transforms: can run in several copies. */
  parallel?: boolean;
  /** A short configuration ("with:" in YAML) for the help in the panel (E14b); missing without configuration. */
  example?: string;
  /** Input transforms: can propose their fields ("Detect fields", S46). */
  detectFields?: boolean;
}

/** Category colours of the tiles (design/README.md, chapter 3.3); transforms: KONZEPT 12.2. */
export const categoryColor: Record<string, string> = {
  general: "var(--rn-kategorie-allgemein)",
  execution: "var(--rn-kategorie-ausfuehrung)",
  files: "var(--rn-kategorie-dateien)",
  network: "var(--rn-kategorie-netzwerk)",
  database: "var(--rn-kategorie-datenbank)",
  input: "var(--rn-kategorie-eingabe)",
  transform: "var(--rn-kategorie-transformation)",
  flow: "var(--rn-kategorie-fluss)",
  combine: "var(--rn-kategorie-transformation)",
  output: "var(--rn-kategorie-ausgabe)",
};

/** Names of the action categories in the palette, in this order (ADR 0014). */
export const categoryLabel: Record<string, string> = {
  general: "General",
  execution: "Execution",
  files: "Files",
  network: "Network",
  database: "Database",
};

/** Names of the transform categories in the palette, in this order (KONZEPT 12.2). */
export const transformCategoryLabel: Record<string, string> = {
  input: "Input",
  transform: "Transform",
  flow: "Flow",
  execution: "Execution",
  combine: "Combine",
  output: "Output",
};

/** Colour of the tile of a type; general for an unknown category. */
export const tileColor = (d: Descriptor) => categoryColor[d.category] ?? categoryColor.general;

/**
 * The action type being dragged from the palette. During a drag, the browser does not
 * reveal the dragged data to the drop target, but the canvas needs the type to show
 * the preview card.
 */
export const paletteDrag: { type: string | null } = { type: null };

/** Data type of a palette entry in a drag-and-drop transfer. */
export const paletteMime = "application/x-runnel-action";

/** Actions and transforms have separate namespaces (ADR 0013). */
export type CatalogKind = "workflow" | "pipeline";
type Catalogs = Record<CatalogKind, Map<string, Descriptor>>;

let cache: Promise<Catalogs> | null = null;

function load(): Promise<Catalogs> {
  cache ??= fetch(`${apiBase}/catalog`)
    .then((r) =>
      r.ok ? (r.json() as Promise<{ actions?: Descriptor[]; transforms?: Descriptor[] }>) : Promise.reject(new Error(r.statusText)),
    )
    .then((c) => ({
      workflow: new Map((c.actions ?? []).map((d) => [d.type, d])),
      pipeline: new Map((c.transforms ?? []).map((d) => [d.type, d])),
    }))
    .catch((err: unknown) => {
      cache = null; // retry on the next use
      throw err;
    });
  return cache;
}

const none = new Map<string, Descriptor>();

/**
 * useCatalog returns the descriptors by type (empty until loaded): of the actions for a
 * workflow, of the transforms for a pipeline.
 */
export function useCatalog(kind: CatalogKind = "workflow"): Map<string, Descriptor> {
  const [catalogs, setCatalogs] = useState<Catalogs | null>(null);
  useEffect(() => {
    load().then(setCatalogs, () => setCatalogs(null));
  }, []);
  return catalogs?.[kind] ?? none;
}
