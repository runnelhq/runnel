/**
 * Minimal YAML highlighting for the read-only file view: comments, mapping keys,
 * punctuation, values and ${…} / {{ … }} references. It works line by line and never
 * fails; unusual YAML is simply shown with less colour.
 */
/** Kinds of tokens; heading, link and code are those of Markdown (E17, markdown.ts). */
export type TokenKind = "comment" | "key" | "punct" | "value" | "var" | "text" | "heading" | "link" | "code";

export interface Token {
  kind: TokenKind;
  text: string;
}

// ${…} references, {{ … }} expressions and the escape $${ (which is not a reference).
const varPattern = /\$\$\{|\$\{[^}]*\}|\{\{.*?\}\}/g;

/** Splits a value into plain text and references. */
function valueTokens(v: string): Token[] {
  const out: Token[] = [];
  let last = 0;
  for (const m of v.matchAll(varPattern)) {
    const start = m.index ?? 0;
    if (m[0] === "$${") continue;
    if (start > last) out.push({ kind: "value", text: v.slice(last, start) });
    out.push({ kind: "var", text: m[0] });
    last = start + m[0].length;
  }
  if (last < v.length) out.push({ kind: "value", text: v.slice(last) });
  return out;
}

/** Splits a trailing " # comment" (outside quotes) from a value. */
function splitComment(v: string): [string, string] {
  let quote = "";
  for (let i = 0; i < v.length; i++) {
    const c = v[i];
    if (quote) {
      if (c === quote) quote = "";
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === "#" && (i === 0 || /\s/.test(v[i - 1]))) {
      return [v.slice(0, i), v.slice(i)];
    }
  }
  return [v, ""];
}

/** Flow collections ({ a: 1, b: [x] }): keys, punctuation and values. */
function flowTokens(v: string): Token[] {
  const out: Token[] = [];
  const re = /([{}[\],])|([\w.-]+)(\s*:)(?=\s|$)|([^{}[\],]+)/g;
  for (const m of v.matchAll(re)) {
    if (m[1]) out.push({ kind: "punct", text: m[1] });
    else if (m[2]) out.push({ kind: "key", text: m[2] }, { kind: "punct", text: m[3] });
    else out.push(...valueTokens(m[4]));
  }
  return out;
}

function restTokens(rest: string): Token[] {
  const [value, comment] = splitComment(rest);
  const trimmed = value.trim();
  const tokens = trimmed.startsWith("{") || trimmed.startsWith("[") ? flowTokens(value) : valueTokens(value);
  if (comment) tokens.push({ kind: "comment", text: comment });
  return tokens;
}

const keyLine = /^(\s*)(- )?([^\s:#'"{}[\],][^:#]*?|"[^"]*"|'[^']*')(:)(\s.*|)$/;
const itemLine = /^(\s*)(- )(.*)$/;

export function highlightLine(line: string): Token[] {
  if (/^\s*#/.test(line)) return [{ kind: "comment", text: line }];
  const m = line.match(keyLine);
  if (m) {
    const [, indent, dash, key, colon, rest] = m;
    const out: Token[] = [{ kind: "text", text: indent }];
    if (dash) out.push({ kind: "punct", text: dash });
    out.push({ kind: "key", text: key }, { kind: "punct", text: colon }, ...restTokens(rest));
    return out;
  }
  const d = line.match(itemLine);
  if (d) return [{ kind: "text", text: d[1] }, { kind: "punct", text: d[2] }, ...restTokens(d[3])];
  return restTokens(line);
}

/**
 * Where the steps and links of a workflow or pipeline are in its text, line by line, so
 * that switching between canvas and YAML view keeps the selected element. Works on the
 * text as it is (also with errors); only block lists directly under the top-level keys
 * are understood, which is how the editor writes them.
 */
export interface Item {
  /** First and last line (1-based) of the list item. */
  from: number;
  to: number;
  /** The id of a step; empty for links. */
  id: string;
}

const topKey = /^([A-Za-z_][\w-]*)\s*:/;
const idValue = /(?:^|[{,\s])id:\s*["']?([A-Za-z0-9_-]+)["']?/;

/** The items of the top-level list key (e.g. "actions", "links") with their lines. */
export function listItems(text: string, key: string): Item[] {
  const lines = text.split("\n");
  const items: Item[] = [];
  let inList = false;
  let indent = -1;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].replace(/\r$/, "");
    const top = topKey.exec(line);
    if (top) {
      inList = top[1] === key;
      indent = -1;
      continue;
    }
    if (!inList || line.trim() === "" || line.trim().startsWith("#")) continue;
    const dash = /^(\s*)-(\s|$)/.exec(line);
    if (dash && (indent < 0 || dash[1].length === indent)) {
      indent = dash[1].length;
      items.push({ from: i + 1, to: i + 1, id: "" });
    }
    const item = items[items.length - 1];
    if (!item) continue;
    item.to = i + 1;
    if (!item.id) {
      const m = idValue.exec(dash ? line.slice(dash[0].length - 1) : line);
      if (m && (dash || /^\s*(\{.*\s)?id:/.test(line))) item.id = m[1];
    }
  }
  return items;
}
