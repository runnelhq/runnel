import { useEffect, useState } from "react";
import { apiBase } from "./api";

/** Descriptor of an action type (GET /catalog, ADR 0014); only what the canvas needs. */
export interface Descriptor {
  type: string;
  category: string;
  label: string;
  description: string;
}

/** Category colours of the tiles (design/README.md, chapter 3.3). */
export const categoryColor: Record<string, string> = {
  general: "var(--rn-kategorie-allgemein)",
  execution: "var(--rn-kategorie-ausfuehrung)",
  files: "var(--rn-kategorie-dateien)",
  network: "var(--rn-kategorie-netzwerk)",
  database: "var(--rn-kategorie-datenbank)",
};

/** Names of the categories in the palette, in this order (ADR 0014). */
export const categoryLabel: Record<string, string> = {
  general: "General",
  execution: "Execution",
  files: "Files",
  network: "Network",
  database: "Database",
};

/**
 * The action type being dragged from the palette. During a drag, the browser does not
 * reveal the dragged data to the drop target, but the canvas needs the type to show
 * the preview card.
 */
export const paletteDrag: { type: string | null } = { type: null };

/** Data type of a palette entry in a drag-and-drop transfer. */
export const paletteMime = "application/x-runnel-action";

let cache: Promise<Map<string, Descriptor>> | null = null;

function load(): Promise<Map<string, Descriptor>> {
  cache ??= fetch(`${apiBase}/catalog`)
    .then((r) => (r.ok ? (r.json() as Promise<{ actions: Descriptor[] }>) : Promise.reject(new Error(r.statusText))))
    .then((c) => new Map(c.actions.map((d) => [d.type, d])))
    .catch((err: unknown) => {
      cache = null; // retry on the next use
      throw err;
    });
  return cache;
}

/** useCatalog returns the action descriptors by type (empty until loaded). */
export function useCatalog(): Map<string, Descriptor> {
  const [catalog, setCatalog] = useState<Map<string, Descriptor>>(new Map());
  useEffect(() => {
    load().then(setCatalog, () => setCatalog(new Map()));
  }, []);
  return catalog;
}
