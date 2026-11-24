import { useEffect, useState } from "react";
import { apiBase } from "./api";
import type { Parameter, Workflow } from "./workflow";

/**
 * Expressions in the editor (roadmap S35, mockup design/mockups/s35-expressions.html):
 * the reference of names and functions from the server, tokens for highlighting,
 * suggestions at the cursor, and where a reported problem lies in a value.
 */

/** A name of the context or a function (internal/expr/docs.go). */
export interface ExprDoc {
  name: string;
  signature?: string;
  type: string;
  description: string;
  example?: string;
  group?: string;
  fields?: ExprDoc[];
}

export interface VariableDoc {
  name: string;
  value: string;
  source: string;
}

/** GET /expressions. */
export interface ExprReference {
  roots: ExprDoc[];
  functions: ExprDoc[];
  variables: VariableDoc[];
}

const empty: ExprReference = { roots: [], functions: [], variables: [] };
let cache: Promise<ExprReference> | null = null;

function load(): Promise<ExprReference> {
  cache ??= fetch(`${apiBase}/expressions`)
    .then((r) => (r.ok ? (r.json() as Promise<ExprReference>) : Promise.reject(new Error(r.statusText))))
    .catch((err: unknown) => {
      cache = null; // retry on the next use
      throw err;
    });
  return cache;
}

/** Loads the reference again on the next use, e.g. after project.yaml changed. */
export function reloadReference() {
  cache = null;
}

/** useReference returns the reference of names and functions (empty until loaded). */
export function useReference(): ExprReference {
  const [ref, setRef] = useState<ExprReference>(empty);
  useEffect(() => {
    load().then(setRef, () => setRef(empty));
  }, []);
  return ref;
}

/** What an expression field knows besides the reference: the workflow it belongs to. */
export interface ExprContext {
  ref: ExprReference;
  params: Parameter[];
  /** Ids of the actions of the workflow (for results.<id>). */
  actions: string[];
}

export function exprContext(ref: ExprReference, wf: Workflow): ExprContext {
  // A pipeline has no results of actions; record follows with the fields of the links (S25a2).
  if (wf.kind === "pipeline") {
    return { ref: { ...ref, roots: ref.roots.filter((r) => r.name !== "result" && r.name !== "results") }, params: wf.parameters ?? [], actions: [] };
  }
  return { ref, params: wf.parameters ?? [], actions: (wf.actions ?? []).map((a) => a.id) };
}

// --- Tokens ---------------------------------------------------------------------

export type TokenKind = "var" | "brace" | "expr" | "root" | "fn" | "string" | "number";

export interface Token {
  from: number;
  to: number;
  kind: TokenKind;
}

const rootNames = ["params", "vars", "env", "run", "result", "results"];

/**
 * tokens finds ${…} variables and {{ … }} expressions in a value and, inside the
 * expressions, the names of the context, function calls, texts and numbers.
 * The kind "expr" covers a whole expression (its background).
 */
export function tokens(text: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  while (i < text.length) {
    const v = text.indexOf("${", i);
    const e = text.indexOf("{{", i);
    if (v < 0 && e < 0) break;
    if (v >= 0 && (e < 0 || v < e)) {
      const end = text.indexOf("}", v + 2);
      const to = end < 0 ? text.length : end + 1;
      out.push({ from: v, to, kind: "var" });
      i = to;
      continue;
    }
    const close = text.indexOf("}}", e + 2);
    const to = close < 0 ? text.length : close + 2;
    out.push({ from: e, to, kind: "expr" }, { from: e, to: e + 2, kind: "brace" });
    inner(text, e + 2, close < 0 ? text.length : close, out);
    if (close >= 0) out.push({ from: close, to: close + 2, kind: "brace" });
    i = to;
  }
  return out;
}

function inner(text: string, from: number, to: number, out: Token[]) {
  const re = /'(?:[^'\\]|\\.)*'?|"(?:[^"\\]|\\.)*"?|\$\{[^}]*\}?|\b\d+(?:\.\d+)?\b|[A-Za-z_]\w*/g;
  re.lastIndex = from;
  const part = text.slice(0, to);
  let m: RegExpExecArray | null;
  while ((m = re.exec(part))) {
    const s = m[0];
    const start = m.index;
    const end = start + s.length;
    const before = text[start - 1];
    if (s[0] === "'" || s[0] === '"') out.push({ from: start, to: end, kind: "string" });
    else if (s.startsWith("${")) out.push({ from: start, to: end, kind: "var" });
    else if (/^\d/.test(s)) out.push({ from: start, to: end, kind: "number" });
    else if (before !== "." && text.slice(end).match(/^\s*\(/)) out.push({ from: start, to: end, kind: "fn" });
    else if (before !== "." && rootNames.includes(s)) out.push({ from: start, to: end, kind: "root" });
  }
}

// --- Suggestions ----------------------------------------------------------------

export interface Suggestion {
  label: string;
  /** Text inserted instead of the word before the cursor. */
  apply: string;
  /** Short type shown on the right, e.g. "integer". */
  detail?: string;
  info?: string;
  /** P parameter, V variable, R field of a result or the run, ƒ function, C context. */
  kind: "param" | "variable" | "field" | "function" | "root" | "pattern";
}

export interface Suggestions {
  /** Start of the word before the cursor that the suggestions replace. */
  from: number;
  options: Suggestion[];
}

/** Common patterns of formatDate and parseDate, with an example. */
export const datePatterns: [string, string][] = [
  ["dd.MM.yyyy", "19.11.2026"],
  ["yyyy-MM-dd", "2026-11-19"],
  ["yyyyMMdd", "20261119"],
  ["dd.MM.yyyy HH:mm", "19.11.2026 14:30"],
];

/** Where the cursor is: in a variable ${…}, in an expression {{ … }} or in plain text. */
export function place(text: string, pos: number): { in: "var" | "expr" | "text"; start: number } {
  const before = text.slice(0, pos);
  const e = before.lastIndexOf("{{");
  const v = before.lastIndexOf("${");
  if (e >= 0 && e > v && before.indexOf("}}", e) < 0) return { in: "expr", start: e + 2 };
  // After ":-" the default of ${NAME:-text} is plain text (K1).
  if (v >= 0 && before.indexOf("}", v) < 0 && !before.slice(v).includes(":-")) return { in: "var", start: v + 2 };
  return { in: "text", start: pos };
}

/** The innermost function call around the cursor and the open text the cursor is in, if any. */
export function scan(text: string, pos: number): { call: { name: string; arg: number } | null; quote: { char: string; start: number } | null } {
  const p = place(text, pos);
  if (p.in !== "expr") return { call: null, quote: null };
  const s = text.slice(p.start, pos);
  const stack: { name: string | null; arg: number }[] = [];
  let quote: { char: string; start: number } | null = null;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote.char) quote = null;
      continue;
    }
    if (c === "'" || c === '"') quote = { char: c, start: p.start + i };
    else if (c === "(") stack.push({ name: s.slice(0, i).match(/([A-Za-z_]\w*)\s*$/)?.[1] ?? null, arg: 0 });
    else if (c === "[") stack.push({ name: null, arg: 0 });
    else if (c === ")" || c === "]") stack.pop();
    else if (c === "," && stack.length > 0) stack[stack.length - 1].arg++;
  }
  const top = stack[stack.length - 1];
  return { call: top?.name ? { name: top.name, arg: top.arg } : null, quote };
}

function paramInfo(p: Parameter) {
  const parts = ["Parameter of this workflow"];
  if (p.default !== undefined && p.default !== "") parts.push(`default ${p.default}`);
  if (p.required) parts.push("required");
  return parts.join(" · ") + (p.description ? `\n${p.description}` : "");
}

/**
 * suggest returns the suggestions for the cursor position, or null where there are
 * none (plain text). explicit is set when they were asked for (Ctrl+Space).
 */
export function suggest(text: string, pos: number, ctx: ExprContext, explicit = false): Suggestions | null {
  const p = place(text, pos);
  if (p.in === "text") return null;
  const before = text.slice(p.start, pos);

  if (p.in === "var") {
    const word = before.match(/[\w:]*$/)![0];
    const close = text[pos] === "}" ? "" : "}";
    const seen = new Set<string>();
    const options: Suggestion[] = [];
    for (const prm of ctx.params) {
      seen.add(prm.name);
      options.push({ label: prm.name, apply: prm.name + close, detail: prm.type ?? "string", info: paramInfo(prm), kind: "param" });
    }
    for (const v of ctx.ref.variables) {
      if (seen.has(v.name)) continue;
      options.push({ label: v.name, apply: v.name + close, detail: v.value, info: `Variable · ${v.source}`, kind: "variable" });
    }
    return { from: pos - word.length, options };
  }

  // In a text of formatDate/parseDate's pattern argument: common patterns.
  const { call, quote } = scan(text, pos);
  if (quote) {
    if (call && (call.name === "formatDate" || call.name === "parseDate") && call.arg === 1) {
      const q = quote.char;
      const from = quote.start;
      const closing = text[pos] === q ? "" : q;
      return {
        from,
        options: datePatterns.map(([pat, ex]) => ({ label: `${q}${pat}${q}`, apply: `${q}${pat}${closing}`, detail: ex, kind: "pattern" })),
      };
    }
    return null;
  }

  const m = before.match(/(?:([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)\.)?([A-Za-z_]\w*)?$/)!;
  const path = m[1];
  const word = m[2] ?? "";
  const from = pos - word.length;
  if (!path) {
    if (!word && !explicit && !/(^|[\s(,!&|+\-*/<>=?:[])$/.test(before)) return null;
    const options: Suggestion[] = ctx.ref.roots.map((r) => ({ label: r.name, apply: r.name, detail: r.type, info: r.description, kind: "root" as const }));
    for (const f of ctx.ref.functions) {
      options.push({ label: f.name, apply: f.name + "(", detail: f.type, info: `${f.signature}\n${f.description}`, kind: "function" });
    }
    return { from, options };
  }
  const parts = path.split(".");
  const root = ctx.ref.roots.find((r) => r.name === parts[0]);
  if (!root) return null;
  const fieldsOf = (docs: ExprDoc[] | undefined): Suggestion[] =>
    (docs ?? []).map((f) => ({ label: f.name, apply: f.name, detail: f.type, info: f.description, kind: "field" }));
  if (parts.length === 1) {
    switch (root.name) {
      case "params":
        return { from, options: ctx.params.map((prm) => ({ label: prm.name, apply: prm.name, detail: prm.type ?? "string", info: paramInfo(prm), kind: "param" })) };
      case "vars": {
        const names = new Map<string, Suggestion>();
        for (const v of ctx.ref.variables) names.set(v.name, { label: v.name, apply: v.name, detail: v.value, info: `Variable · ${v.source}`, kind: "variable" });
        for (const prm of ctx.params) names.set(prm.name, { label: prm.name, apply: prm.name, detail: "string", info: paramInfo(prm), kind: "param" });
        return { from, options: [...names.values()] };
      }
      case "results":
        return { from, options: ctx.actions.map((id) => ({ label: id, apply: id, detail: "result", info: `Result of action ${id}`, kind: "field" })) };
      default:
        return root.fields ? { from, options: fieldsOf(root.fields) } : null;
    }
  }
  if (parts.length === 2 && root.name === "results") return { from, options: fieldsOf(root.fields) };
  return null;
}

// --- Problems ---------------------------------------------------------------------

/** A problem reported by the server, located in the value of a field. */
export interface Located {
  from: number;
  to: number;
  message: string;
  /** A replacement for a misspelt name: replace the text at from–to with fix. */
  fix?: string;
}

function distance(a: string, b: string): number {
  const d = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array<number>(b.length).fill(0)]);
  for (let j = 1; j <= b.length; j++) d[0][j] = j;
  for (let i = 1; i <= a.length; i++)
    for (let j = 1; j <= b.length; j++)
      d[i][j] = Math.min(d[i - 1][j] + 1, d[i][j - 1] + 1, d[i - 1][j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1));
  return d[a.length][b.length];
}

/** The closest of the names, if it is close enough to be a typing error. */
export function closest(name: string, names: string[]): string | undefined {
  let best: string | undefined;
  let bestD = Math.max(1, Math.min(2, Math.floor(name.length / 3)));
  for (const n of names) {
    const dist = distance(name.toLowerCase(), n.toLowerCase());
    if (dist <= bestD && n !== name) {
      best = n;
      bestD = dist - 1;
    }
  }
  return best;
}

/**
 * locate finds a problem of an expression in a value: the server reports
 * `expression {{ CODE }}: MESSAGE`; an unknown name is marked itself, other problems
 * mark the whole expression. Messages without an expression are not located.
 */
export function locate(text: string, message: string, ctx: ExprContext): Located | null {
  const m = message.match(/expression \{\{ (.*?) \}\}: (.*)$/s);
  if (!m) return null;
  const [, code, msg] = m;
  let start = -1;
  for (const t of tokens(text)) {
    if (t.kind === "expr" && text.slice(t.from + 2, t.to - 2).trim() === code.trim()) {
      start = t.from;
      break;
    }
  }
  if (start < 0) {
    const i = text.indexOf(code);
    if (i < 0) return null;
    start = i;
  }
  const end = text.indexOf("}}", start);
  const exprEnd = end < 0 ? text.length : end + 2;
  const unknown = msg.match(/unknown name (\w+)/);
  if (unknown) {
    const re = new RegExp(`(^|[^.\\w])(${unknown[1]})\\b`);
    const at = re.exec(text.slice(start, exprEnd));
    if (at) {
      const from = start + at.index + at[1].length;
      const names = [...rootNames, ...ctx.ref.functions.map((f) => f.name)];
      const fix = closest(unknown[1], names);
      return { from, to: from + unknown[1].length, message: fix ? `${msg} – did you mean ${fix}?` : msg, fix };
    }
  }
  return { from: start, to: exprEnd, message: msg };
}
