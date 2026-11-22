import { autocompletion, type Completion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete";
import type { Extension } from "@codemirror/state";
import { hoverTooltip, type EditorView } from "@codemirror/view";
import type { Descriptor, Schema } from "./catalog";
import { typeLabel, typesOf } from "./reference";

/**
 * Suggestions and descriptions in the YAML editor (E19): field names and values from the
 * schemas of the file kinds (S58) and, in workflows and pipelines, the types of the
 * catalog and the fields under with: of an action or transform (ADR 0014).
 */

/** What the editor knows about the open file. */
export interface AssistContext {
  /** Schema of the whole file (project.yaml, environment, trigger); null for workflows and pipelines. */
  schema: Schema | null;
  /** Descriptors of the steps of a workflow or pipeline by type; empty for other files. */
  catalog: Map<string, Descriptor>;
  /** "actions" or "transforms": where the steps of a workflow or pipeline are. */
  steps?: "actions" | "transforms";
  /** Environments of the project, offered for defaultEnvironment and environment. */
  environments: string[];
  /** The file is a trigger: under with: only the fields of its type. */
  trigger?: boolean;
}

/** The value of a top-level key, e.g. the type of a trigger. */
function topValue(lines: string[], key: string): string | null {
  for (const text of lines) {
    const l = parse(text);
    if (l.indent === 0 && l.dash < 0 && l.key === key) return unquote(l.value ?? "");
  }
  return null;
}

/** A line of the text: its list marker, the column of its key, key and value. */
interface Line {
  indent: number; // column of the key (after "- ")
  dash: number; // column of "- ", or -1
  key?: string;
  value?: string;
}

const lineRe = /^( *)(- +)?(?:([A-Za-z_][\w.-]*)\s*:(?:\s+(.*?))?\s*$)?/;

function parse(text: string): Line {
  const m = lineRe.exec(text)!;
  const value = m[4]?.replace(/\s+#.*$/, "");
  return { indent: m[1].length + (m[2]?.length ?? 0), dash: m[2] ? m[1].length : -1, key: m[3], value: value || undefined };
}

const blank = (text: string) => !text.trim() || text.trimStart().startsWith("#");

/**
 * pathAt returns the keys from the top of the document to the mapping a line belongs to,
 * with "[]" for list items, e.g. ["actions", "[]", "with"] inside the with: of an action,
 * and the first line of the innermost list item (-1 if none). indent is the column the
 * key of the line starts (or would start) at, dash the column of its "- " (or -1).
 */
export function pathAt(lines: string[], row: number, indent: number, dash = -1): { path: string[]; item: number } {
  const path: string[] = [];
  let item = -1;
  let level = indent;
  let list = false; // looking for the key that owns a list at column level
  const enterItem = (i: number, col: number) => {
    if (item < 0) item = i;
    path.unshift("[]");
    level = col;
    list = true;
  };
  if (dash >= 0) enterItem(row, dash);
  for (let i = row - 1; i >= 0 && (level > 0 || list); i--) {
    if (blank(lines[i])) continue;
    const l = parse(lines[i]);
    if (list && l.dash === level) continue; // an earlier item of the same list
    if (!list && l.dash >= 0 && l.indent === level) {
      enterItem(i, l.dash); // the line starts the list item the mapping belongs to
      continue;
    }
    if (l.key && (l.indent < level || (list && l.indent === level && l.dash < 0))) {
      path.unshift(l.key);
      level = l.indent;
      list = false;
      if (l.dash >= 0) enterItem(i, l.dash);
    }
  }
  return { path, item };
}

/** schemaAt follows a path through a schema: properties and the items of lists. */
export function schemaAt(root: Schema | null, path: string[]): Schema | null {
  let s: Schema | null = root;
  for (const p of path) s = (p === "[]" ? s?.items : s?.properties?.[p]) ?? null;
  return s;
}

/** The type of the step whose list item starts at line item (type: at the item's column). */
function stepType(lines: string[], item: number): string | null {
  if (item < 0) return null;
  const col = parse(lines[item]).indent;
  for (let i = item; i < lines.length; i++) {
    if (blank(lines[i])) continue;
    const l = parse(lines[i]);
    if (i > item && (l.indent < col || (l.dash >= 0 && l.indent <= col))) break;
    if (l.indent === col && l.key === "type") return unquote(l.value ?? "");
  }
  return null;
}

const unquote = (v: string) => v.replace(/^(["'])(.*)\1$/, "$2");

/** The schema of the mapping at a path: from the schema of the file kind, or for with: of a step from the catalog. */
function mappingSchema(ctx: AssistContext, lines: string[], path: string[], item: number): Schema | null {
  if (ctx.schema) return schemaAt(ctx.schema, path);
  if (ctx.steps && path.length === 3 && path[0] === ctx.steps && path[1] === "[]" && path[2] === "with") {
    const t = stepType(lines, item);
    return t ? (ctx.catalog.get(t)?.schema ?? null) : null;
  }
  return null;
}

/** Whether a path is a step of the workflow or pipeline (where type: names an action or transform). */
const isStep = (ctx: AssistContext, path: string[]) => !!ctx.steps && path.length === 2 && path[0] === ctx.steps && path[1] === "[]";

/** The text for a field in suggestions and on hover: description, allowed values, default, example. */
export function describe(s: Schema): string {
  const parts = [s.description ?? ""];
  if (s.enum) parts.push(`Allowed: ${s.enum.map(String).join(", ")}`);
  if (s.default !== undefined) parts.push(`Default: ${String(s.default)}`);
  if (s.examples?.[0] !== undefined) parts.push(`Example: ${String(s.examples[0])}`);
  return parts.filter(Boolean).join("\n");
}

/** The keys already set in the mapping of a line: same column, same block, same list item. */
function siblings(lines: string[], row: number, indent: number, dash: number): Set<string> {
  const out = new Set<string>();
  for (let i = row + 1; i < lines.length; i++) {
    if (blank(lines[i])) continue;
    const l = parse(lines[i]);
    if (l.indent < indent || (l.dash >= 0 && l.indent === indent)) break; // the block or the list item ends
    if (l.indent === indent && l.key) out.add(l.key);
  }
  if (dash >= 0) return out; // the line starts its list item
  for (let i = row - 1; i >= 0; i--) {
    if (blank(lines[i])) continue;
    const l = parse(lines[i]);
    if (l.indent < indent) break;
    if (l.indent === indent && l.key) out.add(l.key);
    if (l.dash >= 0 && l.indent === indent) break; // the start of the list item
  }
  return out;
}

function complete(get: () => AssistContext) {
  return (cx: CompletionContext): CompletionResult | null => {
    const ctx = get();
    const line = cx.state.doc.lineAt(cx.pos);
    const before = line.text.slice(0, cx.pos - line.from);
    const lines = cx.state.doc.toString().split("\n");
    const row = line.number - 1;
    // After "key: ": the allowed values of the field, or the types of the catalog after type:.
    const value = /^( *)(- +)?([A-Za-z_][\w.-]*)\s*:\s+["']?([\w.-]*)$/.exec(before);
    if (value) {
      const dash = value[2] ? value[1].length : -1;
      const { path, item } = pathAt(lines, row, value[1].length + (value[2]?.length ?? 0), dash);
      const key = value[3];
      let options: Completion[];
      if (key === "type" && isStep(ctx, path)) {
        options = [...ctx.catalog.values()].map((d) => ({ label: d.type, detail: d.label, info: d.description, type: "type" }));
      } else {
        const s = mappingSchema(ctx, lines, path, item)?.properties?.[key];
        if (!s) return null;
        const env = key === "defaultEnvironment" || (key === "environment" && s.type === "string");
        const values = s.enum ?? (s.type === "boolean" ? [true, false] : env ? ctx.environments : []);
        // In the order of the schema (the first allowed value usually is the default).
        options = values.map((v, i) => ({ label: String(v), detail: v === s.default ? "default" : undefined, type: "value", boost: -i }));
      }
      if (options.length === 0) return null;
      return { from: cx.pos - value[4].length, options, validFor: /^[\w.-]*$/ };
    }
    // At the start of a line or in a word without ":": the fields not set yet.
    const key = /^( *)(- +)?([A-Za-z_][\w.-]*)?$/.exec(before);
    if (!key || (!cx.explicit && !key[3])) return null;
    const indent = key[1].length + (key[2]?.length ?? 0);
    const dash = key[2] ? key[1].length : -1;
    const { path, item } = pathAt(lines, row, indent, dash);
    const s = mappingSchema(ctx, lines, path, item);
    if (!s?.properties) return null;
    const present = siblings(lines, row, indent, dash);
    // The fields of with: name the trigger types they apply to (S58).
    const type = ctx.trigger && path.length === 1 && path[0] === "with" ? topValue(lines, "type") : null;
    const fits = (ps: Schema) => !type || !typesOf(ps.description).length || typesOf(ps.description).includes(type);
    const pad = " ".repeat(indent + 2);
    const options: Completion[] = Object.entries(s.properties)
      .filter(([k, ps]) => !present.has(k) && fits(ps))
      .map(([k, ps]) => {
        const required = s.required?.includes(k) ?? false;
        return {
          label: k,
          detail: typeLabel(ps) + (required ? " · required" : ""),
          info: describe(ps) || undefined,
          type: "property",
          // Objects and lists continue on an indented line.
          apply: ps.type === "object" ? `${k}:\n${pad}` : ps.type === "array" ? `${k}:\n${pad}- ` : `${k}: `,
          boost: required ? 1 : 0,
        };
      });
    if (options.length === 0) return null;
    return { from: cx.pos - (key[3]?.length ?? 0), options, validFor: /^[\w.-]*$/ };
  };
}

function tooltipDom(head: string, body: string): HTMLElement {
  const dom = document.createElement("div");
  dom.className = "rx-hover";
  dom.dataset.testid = "yaml-hover";
  const h = document.createElement("div");
  h.className = "rx-hover-head";
  h.textContent = head;
  dom.append(h);
  if (body) {
    const b = document.createElement("div");
    b.className = "rx-hover-text";
    b.textContent = body;
    dom.append(b);
  }
  return dom;
}

/** The description of the field (key) or of the action or transform (value of type:) under the pointer. */
function hover(get: () => AssistContext) {
  return hoverTooltip((view: EditorView, pos: number) => {
    const ctx = get();
    const line = view.state.doc.lineAt(pos);
    const l = parse(line.text);
    if (!l.key) return null;
    const lines = view.state.doc.toString().split("\n");
    const { path, item } = pathAt(lines, line.number - 1, l.indent, l.dash);
    const keyFrom = line.from + l.indent;
    const keyTo = keyFrom + l.key.length;
    let head: string, body: string, from: number, to: number;
    if (pos >= keyFrom && pos <= keyTo) {
      const s = mappingSchema(ctx, lines, path, item)?.properties?.[l.key];
      if (!s) return null;
      [head, body, from, to] = [`${l.key} · ${typeLabel(s)}`, describe(s), keyFrom, keyTo];
    } else if (l.key === "type" && l.value && isStep(ctx, path)) {
      const d = ctx.catalog.get(unquote(l.value));
      from = line.from + line.text.indexOf(l.value, l.indent + l.key.length);
      to = from + l.value.length;
      if (!d || pos < from || pos > to) return null;
      [head, body] = [`${d.type} – ${d.label}`, d.description];
    } else {
      return null;
    }
    return { pos: from, end: to, above: true, create: () => ({ dom: tooltipDom(head, body) }) };
  });
}

/** yamlAssist returns the extensions of the YAML editor; get gives the current context of the file. */
export function yamlAssist(get: () => AssistContext): Extension {
  return [autocompletion({ override: [complete(get)], icons: false, tooltipClass: () => "rx-complete" }), hover(get)];
}
