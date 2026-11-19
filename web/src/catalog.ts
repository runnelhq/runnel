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
