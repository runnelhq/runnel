import MarkdownIt from "markdown-it";
import type Token from "markdown-it/lib/token.mjs";
import type { Token as LineToken } from "./yaml";

// Markdown documents of a project (E17, mockup design/mockups/e17-markdown.html): shown as a
// formatted view without HTML. Links to files of the project open them in the editor,
// links to the web open in a new tab, images come from the project only.

/** What the formatted view needs to know of the project. */
export interface DocContext {
  /** Path of the document in the project, e.g. "docs/operations.md". */
  path: string;
  /** Files of the project (paths). */
  files: Set<string>;
  /** URL of a file of the project (the file API). */
  fileURL: (path: string) => string;
}

// HTML in the text is shown as text (html: false); markdown-it also refuses links with
// javascript:, vbscript:, file: and data: (except images).
const md = new MarkdownIt({ html: false, linkify: false, typographer: false });

/**
 * Resolves a relative link of a document against its folder: "../x.md" from
 * "docs/a.md" is "x.md". Returns null for a path outside the project.
 */
export function resolve(from: string, href: string): string | null {
  let target = href.split("#")[0].split("?")[0];
  try {
    target = decodeURI(target);
  } catch {
    return null;
  }
  if (target === "") return null;
  const parts = target.startsWith("/") ? [] : from.split("/").slice(0, -1);
  for (const p of target.split("/")) {
    if (p === "" || p === ".") continue;
    if (p === "..") {
      if (parts.length === 0) return null;
      parts.pop();
    } else parts.push(p);
  }
  return parts.join("/");
}

const external = /^https?:\/\//i;
const scheme = /^[a-z][a-z0-9+.-]*:|^\/\//i;

const escape = (s: string) => md.utils.escapeHtml(s);

// A link becomes an external link, a link to a file of the project (data-file), a missing
// file (data-missing) or plain text (other schemes, anchors); link_close follows the kind.
md.renderer.rules.link_open = (tokens: Token[], i: number, _o, env: { ctx: DocContext; links: string[] }) => {
  const href = tokens[i].attrGet("href") ?? "";
  let kind: string;
  let html: string;
  if (external.test(href)) {
    kind = "a";
    html = `<a class="ext" href="${escape(href)}" target="_blank" rel="noopener noreferrer" title="${escape(href)} – opens in a new tab">`;
  } else if (scheme.test(href) || href.startsWith("#")) {
    kind = "span";
    html = `<span class="nolink" title="${escape(href)}">`;
  } else {
    const path = resolve(env.ctx.path, href);
    kind = "a";
    if (path !== null && env.ctx.files.has(path)) {
      html = `<a class="file" href="#" data-file="${escape(path)}" title="Open ${escape(path)}"><span data-icon="${escape(path)}"></span>`;
    } else {
      const shown = path ?? href;
      html = `<a class="missing" href="#" data-missing="${escape(shown)}" title="✖ ${escape(shown)} does not exist">⚠ `;
    }
  }
  env.links.push(kind);
  return html;
};
md.renderer.rules.link_close = (_t, _i, _o, env: { links: string[] }) => `</${env.links.pop() ?? "a"}>`;

md.renderer.rules.image = (tokens: Token[], i: number, _o, env: { ctx: DocContext }) => {
  const t = tokens[i];
  const src = t.attrGet("src") ?? "";
  const alt = t.content;
  const path = external.test(src) || scheme.test(src) ? null : resolve(env.ctx.path, src);
  if (path !== null && env.ctx.files.has(path) && /\.(png|jpe?g|gif|webp)$/i.test(path)) {
    return `<img src="${escape(env.ctx.fileURL(path))}" alt="${escape(alt)}" title="${escape(path)}">`;
  }
  const why = path === null ? "only images of the project are loaded" : "the image is not in the project";
  return `<span class="imgbox">Image not shown: ${why} (${escape(src)})</span>`;
};

/** The formatted view of a document as HTML (without HTML from the text). */
export function renderMarkdown(text: string, ctx: DocContext): string {
  return md.render(text, { ctx, links: [] });
}

const linkPattern = /(!?\[[^\]]*\]\([^)]*\))/g;
const codePattern = /(`[^`]+`)/g;

/** Highlights a line of the text view: headings, links and code. */
export function highlightMarkdownLine(line: string): LineToken[] {
  if (/^\s{0,3}#{1,6}(\s|$)/.test(line)) return [{ kind: "heading", text: line }];
  if (/^\s{0,3}(```|~~~)/.test(line)) return [{ kind: "punct", text: line }];
  const out: LineToken[] = [];
  for (const part of line.split(linkPattern)) {
    if (part === "") continue;
    if (/^!?\[[^\]]*\]\([^)]*\)$/.test(part)) {
      out.push({ kind: "link", text: part });
      continue;
    }
    for (const p of part.split(codePattern)) if (p !== "") out.push({ kind: p.startsWith("`") && p.endsWith("`") && p.length > 1 ? "code" : "text", text: p });
  }
  return out;
}
