import { useEffect, useState } from "react";
import { apiBase } from "./api";
import type { ActionStatus, Counts, LogLine, RunStatus } from "./run";

/**
 * The runs of the project from the run history (ADR 0026) for the list of runs (mockup
 * S26d): GET /runs with filters and pages, GET /runs/{id} for the details.
 */

/** The tab with the list of runs; ":" cannot be in the path of a file of the project. */
export const runsTab = ":runs";

/** A run as GET /runs lists it (runs.Run). */
export interface RunInfo {
  id: string;
  path: string;
  kind: "workflow" | "pipeline";
  workflow: string;
  /** cron:<name>, http:<name>, cli …; missing for runs started in the editor. */
  trigger?: string;
  client?: string;
  environment?: string;
  params?: Record<string, string>;
  status: RunStatus;
  started: string;
  finished?: string;
  durationMs: number;
  error?: string;
  /** Pipelines: records per link (index in links). */
  linkRows?: number[];
}

/** A run with its actions and log (GET /runs/{id}). */
export interface RunDetail extends RunInfo {
  actions: { id: string; type: string; status: ActionStatus; durationMs?: number; error?: string; runs: number; counts?: Counts }[];
  log: LogLine[];
  logTruncated?: boolean;
}

/** Filters of the list; trigger "editor" selects the runs without trigger (S26e1). */
export interface RunFilter {
  status?: RunStatus;
  path?: string;
  trigger?: string;
}

/** Runs per page, as the server lists them by default (runs.DefaultList). */
export const runsPage = 200;

export async function fetchRuns(filter: RunFilter, before?: string, limit = runsPage): Promise<RunInfo[]> {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries({ ...filter, before, limit: String(limit) })) if (v) q.set(k, v);
  const res = await fetch(`${apiBase}/runs?${q}`);
  const body = (await res.json()) as { runs?: RunInfo[]; error?: string };
  if (!res.ok) throw new Error(body.error ?? `${res.status} ${res.statusText}`);
  return body.runs ?? [];
}

export async function fetchRun(id: string): Promise<RunDetail> {
  const res = await fetch(`${apiBase}/runs/${encodeURIComponent(id)}`);
  const body = (await res.json()) as RunDetail & { error?: string };
  if (!res.ok) throw new Error(body.error ?? `${res.status} ${res.statusText}`);
  return body;
}

/** The number of running runs of the project for the header, again whenever tick changes. */
export function useRunningCount(tick: unknown): number {
  const [n, setN] = useState(0);
  useEffect(() => {
    let live = true;
    fetchRuns({ status: "running" }).then(
      (r) => live && setN(r.length),
      () => live && setN(0),
    );
    return () => {
      live = false;
    };
  }, [tick]);
  return n;
}

const pad = (n: number) => String(n).padStart(2, "0");

/** "Today · Tue 24 Nov 2026", "Yesterday · Mon 23 Nov 2026", "Sun 22 Nov 2026": the day of a run in local time. */
export function dayLabel(iso: string, now = new Date()): string {
  const d = new Date(iso);
  const day = d.toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short", year: "numeric" }).replace(",", "");
  const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((start(now) - start(d)) / 86_400_000);
  return diff === 0 ? `Today · ${day}` : diff === 1 ? `Yesterday · ${day}` : day;
}

/** "22:12:47" in local time. */
export const timeOf = (iso: string) => {
  const d = new Date(iso);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

/** How the trigger of a run is shown: "Editor", "runnel run" or "cron:nightly". */
export const triggerLabel = (trigger?: string) => (!trigger ? "Editor" : trigger === "cli" ? "runnel run" : trigger);
