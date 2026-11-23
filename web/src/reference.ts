import { useEffect, useState } from "react";
import { apiBase } from "./api";
import type { Schema } from "./catalog";

/**
 * Help for project.yaml, environments and triggers (E18b, mockup E18): the fields of the
 * file kind from its schema (S58), which of them the file already has, inserting a field
 * at the right place, and the variables of the project (E18a).
 */

/** File kinds with a reference: their schemas come from GET /schemas/{kind}. */
export type ReferenceKind = "project" | "environment" | "trigger" | "connection";

const schemas = new Map<ReferenceKind, Promise<Schema>>();

/** useSchema loads the schema of a file kind once per page (null: none). */
export function useSchema(kind: ReferenceKind | null): Schema | null {
  const [schema, setSchema] = useState<Schema | null>(null);
  useEffect(() => {
    if (!kind) return setSchema(null);
    let p = schemas.get(kind);
    if (!p) {
      p = fetch(`${apiBase}/schemas/${kind}`).then((r) => (r.ok ? (r.json() as Promise<Schema>) : Promise.reject(new Error(r.statusText))));
      p.catch(() => schemas.delete(kind));
      schemas.set(kind, p);
    }
    let live = true;
    p.then((s) => live && setSchema(s), () => live && setSchema(null));
    return () => {
      live = false;
    };
  }, [kind]);
  return schema;
}

/** A connection of the project as GET /connections returns it. */
export interface ConnectionInfo {
  name: string;
  type: string;
  file: string;
}

/** useConnections loads the connections of the project (null while loading or on an error). */
export function useConnections(): ConnectionInfo[] | null {
  const [list, setList] = useState<ConnectionInfo[] | null>(null);
  useEffect(() => {
    let live = true;
    fetch(`${apiBase}/connections`)
      .then((r) => (r.ok ? (r.json() as Promise<{ connections: ConnectionInfo[] }>) : Promise.reject(new Error(r.statusText))))
      .then((v) => live && setList(v.connections), () => live && setList(null));
    return () => {
      live = false;
    };
  }, []);
  return list;
}

/** A variable of the project as GET /variables returns it (E18a). */
export interface VariableInfo {
  name: string;
  project: VariableValue | null;
  environments: Record<string, VariableValue>;
  uses: { file: string; line: number; column: number; parameter?: boolean }[];
}

export interface VariableValue {
  value: string;
  file: string;
  line: number;
}

export interface VariablesInfo {
  environments: string[];
  defaultEnvironment?: string;
  variables: VariableInfo[];
  truncated?: boolean;
}

/** useVariables loads the variables of the project, again whenever key changes. */
export function useVariables(key: unknown): VariablesInfo | null {
  const [info, setInfo] = useState<VariablesInfo | null>(null);
  useEffect(() => {
    let live = true;
    fetch(`${apiBase}/variables`)
      .then((r) => (r.ok ? (r.json() as Promise<VariablesInfo>) : Promise.reject(new Error(r.statusText))))
      .then((v) => live && setInfo(v), () => live && setInfo(null));
    return () => {
      live = false;
    };
  }, [key]);
  return info;
}

/** Whether a variable is used without being set for an environment (warning ⚠). */
export function missingIn(v: VariableInfo, env: string): boolean {
  return !v.project && !v.environments[env] && v.uses.some((u) => !u.parameter);
}

/** Trigger types: fields under with: name the types they belong to (S58 descriptions). */
export const triggerTypes = ["cron", "interval", "file", "http"];

/**
 * typesOf returns the trigger types a field of with: applies to: its description starts
 * each part (separated by "; ") with the types before a colon, e.g. "cron, interval:
 * …" or "file: …; http: …".
 */
export function typesOf(description = ""): string[] {
  const out = new Set<string>();
  for (const part of description.split("; ")) {
    const head = part.split(":")[0];
    for (const word of head.split(/[\s,]+/)) if (triggerTypes.includes(word)) out.add(word);
  }
  return [...out];
}

/** The description without the leading type names, for the short text of a field. */
export function shortText(description = ""): string {
  const first = description.split("; ")[0];
  const i = first.indexOf(":");
  const text = i > 0 && typesOf(first.slice(0, i)).length > 0 ? first.slice(i + 1).trim() : first;
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** A label for the type of a field: text, number, yes/no, choice, list, map, object, duration. */
export function typeLabel(s: Schema): string {
  if (s.enum) return "choice";
  if (s.format === "runnel-duration") return "duration";
  switch (s.type) {
    case "boolean":
      return "yes/no";
    case "integer":
    case "number":
      return "number";
    case "array":
      return "list";
    case "object":
      return s.properties ? "object" : "map";
    case "string":
      return "text";
  }
  return "value";
}

const keyLine = /^( *)([A-Za-z_][\w-]*):(.*)$/;

/**
 * fieldLines returns the line (1-based) of every key of the file: top-level keys by name,
 * keys one level below as "parent.key" (e.g. "with.schedule", "variables.BASE").
 */
export function fieldLines(text: string): Map<string, number> {
  const out = new Map<string, number>();
  let parent = "";
  text.split(/\r?\n/).forEach((line, i) => {
    const m = keyLine.exec(line);
    if (!m) return;
    if (m[1].length === 0) {
      parent = m[2];
      if (!out.has(m[2])) out.set(m[2], i + 1);
    } else if (m[1].length === 2 && parent && !out.has(`${parent}.${m[2]}`)) {
      out.set(`${parent}.${m[2]}`, i + 1);
    }
  });
  return out;
}

/** The result of inserting a field: the new text and the part to select (1-based line, columns from 0). */
export interface Inserted {
  text: string;
  line: number;
  from: number;
  to: number;
}

/** The value written for a new field: an example, the default, the first allowed value, or empty. */
export function placeholder(s: Schema): string {
  const v = s.examples?.[0] ?? s.default ?? s.enum?.[0];
  if (v === undefined || v === null) return s.type === "string" || !s.type ? '""' : "";
  const text = String(v);
  return /^[\w./-][\w ./:-]*$/.test(text) && !/^(true|false|yes|no|null|~)$/i.test(text) && typeof v === "string" ? text : JSON.stringify(v);
}

/**
 * insertField adds "key: value" to the file: at the top level (parent empty) after the
 * last top-level block, or as the last entry of the block parent (created if missing).
 * Objects and maps get an empty indented line to type into. The value (or the empty line)
 * is selected.
 */
export function insertField(text: string, parent: string, key: string, schema: Schema): Inserted {
  const eol = text.includes("\r\n") ? "\r\n" : "\n";
  const lines = text.replace(/\r?\n$/, "").split(/\r?\n/);
  const block = schema.type === "object" || schema.type === "array";
  const indent = parent ? "  " : "";
  const value = block ? "" : placeholder(schema);
  const added = block ? [`${indent}${key}:`, `${indent}  ${schema.type === "array" ? "- " : ""}`] : [`${indent}${key}: ${value}`];
  let at = lines.length; // index to insert before
  if (parent) {
    const start = lines.findIndex((l) => l.startsWith(`${parent}:`));
    if (start < 0) {
      added.unshift(`${parent}:`);
    } else {
      at = start + 1;
      while (at < lines.length && (/^\s+\S/.test(lines[at]) || lines[at].trim() === "")) at++;
      while (at > start + 1 && lines[at - 1].trim() === "") at--;
    }
  }
  lines.splice(at, 0, ...added);
  const last = at + added.length; // 1-based line of the last added line
  const lastText = lines[last - 1];
  const from = block ? lastText.length : lastText.length - value.length + (value === '""' ? 1 : 0);
  const to = block ? lastText.length : value === '""' ? from : lastText.length;
  return { text: lines.join(eol) + eol, line: last, from, to };
}
