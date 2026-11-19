import { useCallback, useEffect, useRef, useState } from "react";
import { apiBase } from "./api";
import { APIError } from "./files";
import type { Problem, Workflow } from "./workflow";

/** Status of an action in a run (GET /runs/{id}, docs/dev/API.md). */
export type ActionStatus = "pending" | "waiting" | "running" | "succeeded" | "failed" | "skipped" | "cancelled";
export type RunStatus = "running" | "succeeded" | "failed" | "cancelled";

export interface ActionRun {
  status: ActionStatus;
  /** Start time in milliseconds (for the time shown while running). */
  started?: number;
  durationMs?: number;
  error?: string;
}

/** A line of the run log (mockup E8), worded like "runnel run -v". */
export interface LogLine {
  time: string;
  level: "debug" | "info" | "warn" | "error";
  actionId?: string;
  message: string;
}

/** Lines kept per run in the browser (like the server, docs/dev/API.md). */
export const maxLogLines = 5000;

/** A run of the open workflow as the canvas shows it (mockup E8). */
export interface RunView {
  id: string;
  workflow: string;
  environment: string;
  status: RunStatus;
  started: number;
  durationMs?: number;
  error?: string;
  actions: Record<string, ActionRun>;
  log: LogLine[];
  /** Actions and links when the run started; the status is hidden once they change (decision for mockup E8). */
  signature: string;
}

/** Event of the live stream (GET /events). */
interface RunEvent {
  kind: string;
  time: string;
  actionId?: string;
  actionType?: string;
  level?: LogLine["level"];
  message?: string;
  status?: string;
  durationMs?: number;
  error?: string;
  outputs?: Record<string, unknown>;
}

/** The log lines of an event, worded like the CLI (internal/cli/run.go). */
function logLines(ev: RunEvent, run: RunView): LogLine[] {
  const at = (level: LogLine["level"], message: string, actionId = ev.actionId): LogLine => ({ time: ev.time, level, actionId, message });
  const took = formatDuration(ev.durationMs ?? 0);
  switch (ev.kind) {
    case "run.started":
      return [at("info", `workflow "${run.workflow}" started${run.environment ? ` (environment ${run.environment})` : ""}`)];
    case "action.started":
      return [at("debug", `started (${ev.actionType ?? ""})`)];
    case "action.waiting":
      return [at("debug", ev.message ?? "waiting")];
    case "action.skipped":
      return [at("debug", "skipped (no incoming link applies)")];
    case "log":
      return [at(ev.level ?? "info", ev.message ?? "")];
    case "action.finished":
      if (ev.status === "cancelled") return [at("warn", `cancelled after ${took}`)];
      if (ev.error) return [at("error", `failed after ${took}: ${ev.error}`)];
      return [at("debug", `${ev.status ?? "succeeded"} in ${took}`)];
    case "run.finished": {
      const outputs = Object.entries(ev.outputs ?? {}).map(([k, v]) =>
        at("info", `output ${k} = ${typeof v === "string" ? v : JSON.stringify(v)}`, undefined),
      );
      const name = `workflow "${run.workflow}"`;
      if (ev.status === "cancelled") return [...outputs, at("warn", `${name} cancelled after ${took}`, undefined)];
      if (ev.status === "failed") return [...outputs, at("error", `${name} failed after ${took}: ${ev.error ?? ""}`, undefined)];
      return [...outputs, at("info", `${name} succeeded in ${took}`, undefined)];
    }
  }
  return [];
}

/** The parts of a workflow a run status belongs to: actions and links, not their positions. */
export function structureSignature(wf: Workflow): string {
  return JSON.stringify([
    (wf.actions ?? []).map((a) => [a.id, a.type]),
    (wf.links ?? []).map((l) => [l.from, l.to, l.when ?? "", !!l.loop, !!l.disabled]),
  ]);
}

/** Values of the run dialog, remembered per workflow in the browser (decision for mockup E8). */
export interface RunSettings {
  environment: string;
  params: Record<string, string>;
}

const settingsKey = (path: string) => `runnel.run:${path}`;

export function loadRunSettings(path: string): RunSettings | null {
  try {
    const raw = localStorage.getItem(settingsKey(path));
    return raw ? (JSON.parse(raw) as RunSettings) : null;
  } catch {
    return null;
  }
}

export function saveRunSettings(path: string, s: RunSettings) {
  try {
    localStorage.setItem(settingsKey(path), JSON.stringify(s));
  } catch {
    // storage not available: the values are asked again next time
  }
}

/** Error when a run cannot start; problems are set when the workflow has errors (422). */
export class StartError extends APIError {
  constructor(
    message: string,
    status: number,
    readonly problems: Problem[] = [],
  ) {
    super(message, status);
  }
}

/**
 * useRun starts runs of a workflow file and follows the last one via the event stream
 * (filtered by run, from its first event on).
 */
export function useRun(path: string) {
  const [run, setRun] = useState<RunView | null>(null);
  const source = useRef<EventSource | null>(null);
  useEffect(() => () => source.current?.close(), []);

  const start = useCallback(
    async (settings: RunSettings, workflow: Workflow) => {
      const params = Object.fromEntries(Object.entries(settings.params).filter(([, v]) => v !== ""));
      const res = await fetch(`${apiBase}/runs`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path, environment: settings.environment || undefined, params }),
      });
      const body = (await res.json()) as { id?: string; environment?: string; error?: string; problems?: Problem[] };
      if (!res.ok || !body.id) {
        throw new StartError(body.error ?? `${res.status} ${res.statusText}`, res.status, body.problems);
      }
      const id = body.id;
      const actions: Record<string, ActionRun> = {};
      for (const a of workflow.actions ?? []) actions[a.id] = { status: "pending" };
      setRun({
        id,
        workflow: workflow.name,
        log: [],
        environment: body.environment ?? settings.environment,
        status: "running",
        started: Date.now(),
        actions,
        signature: structureSignature(workflow),
      });

      source.current?.close();
      const es = new EventSource(`${apiBase}/events?run=${encodeURIComponent(id)}&since=0`);
      source.current = es;
      // Every event adds its lines to the log (oldest lines are dropped beyond maxLogLines).
      const update = (ev: RunEvent, f: (r: RunView) => RunView) =>
        setRun((r) => {
          if (!r || r.id !== id) return r;
          const next = f(r);
          const log = [...next.log, ...logLines(ev, next)];
          return { ...next, log: log.length > maxLogLines ? log.slice(-maxLogLines) : log };
        });
      const action = (ev: RunEvent, a: Partial<ActionRun>) =>
        update(ev, (r) => ({ ...r, actions: { ...r.actions, [ev.actionId!]: { ...r.actions[ev.actionId!], ...a } as ActionRun } }));
      const on = (kind: string, f: (ev: RunEvent) => void) =>
        es.addEventListener(kind, (e) => f(JSON.parse((e as MessageEvent).data as string) as RunEvent));
      on("run.started", (ev) => update(ev, (r) => r));
      on("log", (ev) => update(ev, (r) => r));
      on("action.waiting", (ev) => action(ev, { status: "waiting" }));
      on("action.started", (ev) => action(ev, { status: "running", started: Date.parse(ev.time), durationMs: undefined, error: undefined }));
      on("action.finished", (ev) =>
        action(ev, { status: (ev.status as ActionStatus) ?? "succeeded", durationMs: ev.durationMs ?? 0, error: ev.error }),
      );
      on("action.skipped", (ev) => action(ev, { status: "skipped" }));
      on("run.finished", (ev) => {
        es.close();
        update(ev, (r) => ({
          ...r,
          status: (ev.status as RunStatus) ?? "succeeded",
          durationMs: ev.durationMs,
          error: ev.error,
          // Actions that never started were skipped.
          actions: Object.fromEntries(
            Object.entries(r.actions).map(([k, a]) => [
              k,
              a.status === "pending" || a.status === "waiting" || a.status === "running"
                ? { ...a, status: a.status === "running" ? "cancelled" : "skipped" }
                : a,
            ]),
          ),
        }));
      });
      return id;
    },
    [path],
  );

  const cancel = useCallback(async () => {
    if (!run || run.status !== "running") return;
    await fetch(`${apiBase}/runs/${encodeURIComponent(run.id)}/cancel`, { method: "POST" });
  }, [run]);

  return { run, start, cancel };
}

/** "1.4 s", "2 min 5 s". */
export function formatDuration(ms: number): string {
  if (ms < 50) return "0 s";
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`;
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)} min ${s % 60} s`;
}
