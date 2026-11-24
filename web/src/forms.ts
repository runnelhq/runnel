import { useCallback, useEffect, useRef, useState } from "react";
import { apiBase } from "./api";
import type { FieldValue } from "./components/PropertiesPanel";

/**
 * The forms of files with metadata (S39c2a, E20a): POST /forms/edit reads the fields of a
 * connection or trigger from the text and sets one of them, changing only its line.
 */

/** A change of one field: e.g. description, type, with.<name> or params.<NAME>. */
export interface FieldChange {
  field: string;
  value?: FieldValue;
  map?: { key: string; value: string | number }[];
}

/** A connection as the form reads it. */
export interface ConnectionData {
  name: string;
  type: string;
  description?: string;
  with: Record<string, unknown>;
}

/** A trigger as the form reads it (E20a); schedule for cron: in words and the next times. */
export interface TriggerData {
  name: string;
  type: string;
  description?: string;
  target: string;
  environment?: string;
  enabled?: boolean;
  params: Record<string, unknown>;
  with: Record<string, unknown>;
  schedule?: { text?: string; next: string[] };
}

/** An environment or project.yaml as the form reads it (E20b); secrets only by name. */
export interface SettingsData {
  name: string;
  description?: string;
  defaultEnvironment?: string;
  variables: Record<string, unknown>;
  secretStore?: string;
  secrets: string[];
  workflow?: Record<string, unknown>;
  settings?: Record<string, unknown>;
}

export interface FormResult {
  content: string;
  connection?: ConnectionData;
  trigger?: TriggerData;
  environment?: SettingsData;
  project?: SettingsData;
  /** Fields under with: a new type of a trigger removed. */
  removed?: string[];
}

export async function editForm(path: string, content: string, set?: FieldChange): Promise<FormResult> {
  const res = await fetch(`${apiBase}/forms/edit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, content, set }),
  });
  const body = await res.json();
  if (!res.ok) throw new Error(body.error ?? res.statusText);
  return body as FormResult;
}

/** useForm reads the form of a file from its text, again after every change (also undo or the YAML view); null: not readable. */
export function useForm(path: string, text: string): FormResult | null | undefined {
  const [data, setData] = useState<FormResult | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    const t = setTimeout(() => {
      editForm(path, text).then(
        (r) => live && setData(r),
        () => live && setData(null),
      );
    }, 150);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [path, text]);
  return data;
}

/** A trigger as GET /triggers lists it, with its next and last time on the server. */
export interface TriggerInfo {
  name: string;
  file: string;
  type: string;
  target: string;
  enabled: boolean;
  next?: string;
  last?: { time: string; runId?: string; skipped?: boolean; error?: string };
}

/** useTriggers loads the triggers of the server (S37), again whenever key changes; null while loading or on an error. */
export function useTriggers(key?: unknown): { triggers: TriggerInfo[]; http?: string } | null {
  const [info, setInfo] = useState<{ triggers: TriggerInfo[]; http?: string } | null>(null);
  useEffect(() => {
    let live = true;
    fetch(`${apiBase}/triggers`)
      .then((r) => (r.ok ? (r.json() as Promise<{ triggers: TriggerInfo[]; http?: string }>) : Promise.reject(new Error(r.statusText))))
      .then((v) => live && setInfo(v), () => live && setInfo(null));
    return () => {
      live = false;
    };
  }, [key]);
  return info;
}

/** "Run target now" (E20a): starts the target of the saved trigger with its environment and parameters. */
export async function runTriggerTarget(trigger: string): Promise<{ id: string; target: string }> {
  const res = await fetch(`${apiBase}/runs`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ trigger }),
  });
  const body = (await res.json()) as { id?: string; path?: string; error?: string; problems?: { message: string }[] };
  if (!res.ok || !body.id) throw new Error(body.problems?.[0]?.message ?? body.error ?? res.statusText);
  return { id: body.id, target: body.path ?? "" };
}

/**
 * useFormChange applies changes of a form one after another, each to the text the one
 * before returned: two quick changes (e.g. leaving a field by choosing in another) must
 * not both start from the same text, or the second would undo the first.
 */
export function useFormChange(path: string, text: string, onText: (text: string) => void, onError: (message: string) => void) {
  const latest = useRef(text);
  const queue = useRef<Promise<unknown>>(Promise.resolve());
  const pending = useRef(0);
  // Changes from outside (undo, the YAML view) count once nothing is under way.
  if (pending.current === 0) latest.current = text;
  return useCallback(
    (c: FieldChange): Promise<FormResult | null> => {
      pending.current++;
      const next = queue.current.then(() =>
        editForm(path, latest.current, c).then(
          (r) => {
            latest.current = r.content;
            onText(r.content);
            return r;
          },
          (e: Error) => {
            onError(e.message);
            return null;
          },
        ),
      );
      queue.current = next.finally(() => pending.current--);
      return next;
    },
    [path, onText, onError],
  );
}
