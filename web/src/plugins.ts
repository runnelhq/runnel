import { useCallback, useEffect, useState } from "react";
import { apiBase } from "./api";
import { APIError } from "./files";

/** The approval of a plugin as stored; current is false if the plugin changed since. */
export interface Approval {
  version?: string;
  hash: string;
  trusted: string;
  current: boolean;
}

export interface PluginType {
  type: string;
  label: string;
  description?: string;
}

/** A step or connection of the project with a type of the plugin. */
export interface PluginUse {
  file: string;
  kind: "action" | "transform" | "connection";
  id: string;
  type: string;
}

export type PluginState = "trusted" | "not-trusted" | "changed" | "error";

/** A plugin of the project as GET /plugins returns it (S52a). */
export interface PluginInfo {
  name: string;
  version: string;
  namespace: string;
  description?: string;
  folder: string;
  program?: string;
  executable: string | Record<string, string>;
  args?: string[];
  files: number;
  bytes: number;
  hash?: string;
  state: PluginState;
  message?: string;
  user?: Approval;
  service?: Approval;
  types?: { actions: PluginType[]; transforms: PluginType[]; connectionTypes: PluginType[] };
  uses: PluginUse[];
}

export interface PluginsInfo {
  plugins: PluginInfo[];
  serviceApprovals: boolean;
  canTrust: boolean;
}

/** The plugin that a file belongs to: plugins/<name>/… → name. */
export function pluginOf(path: string): string | null {
  const m = /^plugins\/([^/]+)(\/|$)/.exec(path);
  return m ? m[1] : null;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(apiBase + path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      // body is not JSON
    }
    throw new APIError(msg, res.status);
  }
  return (await res.json()) as T;
}

export const fetchPlugins = () => call<PluginsInfo>("GET", "/plugins");

/** Trusts the plugin as shown: the server refuses (409) if its folder changed since. */
export const trustPlugin = (name: string, hash: string) => call<PluginInfo>("POST", `/plugins/${encodeURIComponent(name)}/trust`, { hash });
export const untrustPlugin = (name: string) => call<PluginInfo>("POST", `/plugins/${encodeURIComponent(name)}/untrust`, {});
export const reloadPlugin = (name: string) => call<PluginInfo>("POST", `/plugins/${encodeURIComponent(name)}/reload`, {});

/**
 * usePlugins loads the plugins of the project again whenever key changes (e.g. the
 * tree of the project) and after reload(); null while loading or when it failed. A key
 * of null loads nothing.
 */
export function usePlugins(key: unknown): { info: PluginsInfo | null; error: string | null; reload: () => void } {
  const [info, setInfo] = useState<PluginsInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [seq, setSeq] = useState(0);
  useEffect(() => {
    if (key === null) return; // not needed (e.g. not a plugin file)
    let live = true;
    fetchPlugins().then(
      (v) => {
        if (!live) return;
        setInfo(v);
        setError(null);
      },
      (e: Error) => live && setError(e.message),
    );
    return () => {
      live = false;
    };
  }, [key, seq]);
  const reload = useCallback(() => setSeq((s) => s + 1), []);
  return { info, error, reload };
}

/** Text of a number of bytes, e.g. 6.2 MB. */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** A hash shortened for lists: the first and last characters, without "sha256:". */
export const shortHash = (hash?: string) => {
  const h = (hash ?? "").replace(/^sha256:/, "");
  return h.length > 12 ? `${h.slice(0, 6)}…${h.slice(-4)}` : h;
};

/**
 * The plugin a message of the checks is about (mockup S52, state canvas), e.g.
 * `action "say": plugin "ldap" is not trusted on this computer; …` or `plugin ldap:
 * initialize: …` for a plugin that cannot start; null for other messages.
 */
export function pluginInMessage(message: string): { name: string; title: string } | null {
  const m = /\bplugin "?([a-z][a-z0-9-]*)"?(:| is | changed)/.exec(message);
  if (!m) return null;
  const name = m[1];
  const title = message.includes("is not trusted")
    ? `Plugin ${name} is not trusted on this computer`
    : message.includes("changed since it was trusted")
      ? `Plugin ${name} changed since it was trusted`
      : `Plugin ${name} cannot start`;
  return { name, title };
}

/** The file that opens the overview of a plugin. */
export const pluginFile = (name: string) => `plugins/${name}/plugin.yaml`;
