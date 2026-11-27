import { apiBase } from "./api";
import { APIError } from "./files";

/** An action with data in the last run started in the editor (GET /runs/{id}/steps, E23a). */
export interface StepSummary {
  actionId: string;
  type: string;
  runs: number;
  status: string;
  entries?: number;
}

/** A place where the server cut the data to its limits (1 MB per step or entry). */
export interface Truncation {
  part: "input" | "row" | "output";
  path?: string;
  kind: "list" | "object" | "text";
  length: number;
  kept: number;
}

/** What one execution (or entry of forEach) of an action got and returned (GET /runs/{id}/steps/{action}). */
export interface StepData {
  runId: string;
  actionId: string;
  type: string;
  run: number;
  runs: number;
  status: string;
  reason?: string;
  durationMs: number;
  inputSources?: Record<string, string>;
  entry?: number;
  entryStatus?: string;
  attempts?: number;
  attemptErrors?: string[];
  row?: unknown;
  input?: Record<string, unknown>;
  secrets?: string[];
  connection?: { name: string; type: string; with: Record<string, unknown>; secrets?: string[] };
  output?: unknown;
  error?: string;
  truncated?: Truncation[];
  dropped?: boolean;
  entries?: { index: number; status: string }[];
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(apiBase + path, { headers: { Accept: "application/json" } });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    throw new APIError(body.error ?? `${res.status} ${res.statusText}`, res.status);
  }
  return (await res.json()) as T;
}

/** The actions with data of a run; null when the server keeps no data for it (404). */
export async function fetchSteps(runId: string): Promise<StepSummary[] | null> {
  try {
    return (await get<{ steps: StepSummary[] }>(`/runs/${encodeURIComponent(runId)}/steps`)).steps ?? [];
  } catch (e) {
    if (e instanceof APIError && e.status === 404) return null;
    throw e;
  }
}

export function fetchStep(runId: string, action: string, run?: number, entry?: number): Promise<StepData> {
  const q = new URLSearchParams();
  if (run) q.set("run", String(run));
  if (entry) q.set("entry", String(entry));
  const qs = q.toString();
  return get<StepData>(`/runs/${encodeURIComponent(runId)}/steps/${encodeURIComponent(action)}${qs ? `?${qs}` : ""}`);
}

const namePattern = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Appends a key or index to a path as expressions write it: items[0].name, headers["content-type"]. */
export function keyPath(path: string, key: string | number): string {
  if (typeof key === "number") return `${path}[${key}]`;
  if (!namePattern.test(key)) return `${path}[${JSON.stringify(key)}]`;
  return path ? `${path}.${key}` : key;
}

/** A path relative to a value (as keyPath builds it) below base. */
export const joinPath = (base: string, rel: string) => (!rel ? base : rel.startsWith("[") ? base + rel : `${base}.${rel}`);

/** Type of a value as the tree shows it. */
export function typeOf(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (Array.isArray(v)) return "list";
  if (typeof v === "object") return "object";
  if (typeof v === "number") return Number.isInteger(v) ? "integer" : "number";
  return typeof v === "boolean" ? "boolean" : "string";
}

export const plural = (n: number, one: string, many = one + "s") => `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;
