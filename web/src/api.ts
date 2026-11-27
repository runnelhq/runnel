import { useEffect, useState } from "react";

/** Base path of the server API (docs/dev/API.md). */
export const apiBase = "/api/v1";

export interface ProjectInfo {
  name: string;
  dir: string;
  /** Names of the environments, sorted. */
  environments?: string[];
  defaultEnvironment?: string;
  /** How long ended runs are kept (S26c), with the defaults. */
  history?: { keepDays: number; maxRuns: number };
  /** The error workflow of the project (settings.onError, ADR 0027), if set. */
  onError?: string;
}

export interface VersionInfo {
  version: string;
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(apiBase + path, { headers: { Accept: "application/json" } });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      // body is not JSON
    }
    throw new Error(msg);
  }
  return (await res.json()) as T;
}

export const fetchProject = () => getJSON<ProjectInfo>("/projects/current");

/** Whether the server's folder has a project; without one, what a new project would get (R10). */
export interface ProjectStatus {
  exists: boolean;
  dir: string;
  name?: string;
  defaultName?: string;
  /** The folder has no files yet (apart from .git). */
  empty: boolean;
}

export const fetchProjectStatus = () => getJSON<ProjectStatus>("/project");

/**
 * Creates the project in the server's folder, like "runnel init" (R10); the folder is the
 * server's, never one of the browser. force adds the files to a folder that is not empty
 * (existing files are kept). Returns the paths created.
 */
export async function createProject(name: string, force: boolean): Promise<string[]> {
  const res = await fetch(apiBase + "/project", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, force }),
  });
  const body = (await res.json().catch(() => ({}))) as { error?: string; created?: string[] };
  if (!res.ok) throw new Error(body.error ?? `${res.status} ${res.statusText}`);
  return body.created ?? [];
}
export const fetchVersion = () => getJSON<VersionInfo>("/version");

export type Connection = "connecting" | "connected" | "disconnected";

/**
 * useConnection follows the server's event stream (server-sent events, ADR 0016).
 * The browser reconnects by itself; the state shows whether the server is reachable.
 * epoch increases on every (re)connect, so that data can be reloaded.
 */
export function useConnection(): { state: Connection; epoch: number; runsTick: number } {
  const [state, setState] = useState<Connection>("connecting");
  const [epoch, setEpoch] = useState(0);
  // runsTick changes when runs start or end (the list of runs, S26e1), at most every half second.
  const [runsTick, setRunsTick] = useState(0);
  useEffect(() => {
    const es = new EventSource(apiBase + "/events");
    es.onopen = () => {
      setState("connected");
      setEpoch((e) => e + 1);
    };
    es.onerror = () => setState("disconnected");
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = () => {
      timer ??= setTimeout(() => {
        timer = undefined;
        setRunsTick((t) => t + 1);
      }, 500);
    };
    for (const kind of ["run.started", "run.finished", "reset"]) es.addEventListener(kind, tick);
    return () => {
      es.close();
      clearTimeout(timer);
    };
  }, []);
  return { state, epoch, runsTick };
}
