/**
 * Minimal YAML highlighting for the read-only file view: comments, mapping keys,
 * punctuation, values and ${…} / {{ … }} references. It works line by line and never
 * fails; unusual YAML is simply shown with less colour.
 */
export type TokenKind = "comment" | "key" | "punct" | "value" | "var" | "text";

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
