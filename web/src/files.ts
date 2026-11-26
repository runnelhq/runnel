import { apiBase } from "./api";

/** An entry of the project tree (GET /projects/current/tree). */
export interface Entry {
  name: string;
  path: string;
  type: "dir" | "file";
  kind?: FileKind;
  size?: number;
  children?: Entry[];
}

/** Kinds of files; documents (.md) and images since E17. */
export type FileKind = "workflow" | "pipeline" | "project" | "environment" | "trigger" | "connection" | "plugin" | "document" | "image" | "binary" | "runs" | "other";

/** Error of the file API with its HTTP status (404: file not found). */
export class APIError extends Error {
  readonly status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function check(res: Response): Promise<Response> {
  if (res.ok) return res;
  let msg = `${res.status} ${res.statusText}`;
  try {
    msg = ((await res.json()) as { error?: string }).error ?? msg;
  } catch {
    // body is not JSON
  }
  throw new APIError(msg, res.status);
}

export async function fetchTree(): Promise<Entry> {
  const res = await check(await fetch(`${apiBase}/projects/current/tree`));
  return (await res.json()) as Entry;
}

/** A file as shown in the editor. */
export interface FileContent {
  text: string;
  encoding: string;
  etag: string | null;
}

/** Encodes each path segment, keeping the "/" separators. */
export const fileURL = (path: string) =>
  `${apiBase}/files/${path.split("/").map(encodeURIComponent).join("/")}`;

export async function fetchFile(path: string): Promise<FileContent> {
  const res = await check(await fetch(fileURL(path), { cache: "no-store" }));
  const bytes = new Uint8Array(await res.arrayBuffer());
  return { ...decode(bytes), etag: res.headers.get("ETag") };
}

/**
 * Decodes a file like the runner reads it: UTF-8 (with or without BOM) or UTF-16 with
 * BOM. Anything else is shown as Windows-1252, the usual legacy encoding on Windows.
 */
export function decode(bytes: Uint8Array): { text: string; encoding: string } {
  if (bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) {
    return { text: new TextDecoder("utf-8").decode(bytes.subarray(3)), encoding: "UTF-8 with BOM" };
  }
  if (bytes[0] === 0xff && bytes[1] === 0xfe) {
    return { text: new TextDecoder("utf-16le").decode(bytes.subarray(2)), encoding: "UTF-16 LE" };
  }
  if (bytes[0] === 0xfe && bytes[1] === 0xff) {
    return { text: new TextDecoder("utf-16be").decode(bytes.subarray(2)), encoding: "UTF-16 BE" };
  }
  try {
    return { text: new TextDecoder("utf-8", { fatal: true }).decode(bytes), encoding: "UTF-8" };
  } catch {
    return { text: new TextDecoder("windows-1252").decode(bytes), encoding: "Windows-1252" };
  }
}

/** Characters of Windows-1252 in 0x80–0x9F that differ from Latin-1 (WHATWG), by character. */
const cp1252: Record<string, number> = Object.fromEntries(
  [..."€‚ƒ„…†‡ˆ‰Š‹ŒŽ‘’“”•–—˜™š›œžŸ"].map((c, i) => [c, [0x80, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8e, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0x9b, 0x9c, 0x9e, 0x9f][i]]),
);

/** A character the encoding of a file cannot hold, with its place. */
export class EncodeError extends Error {
  constructor(char: string, line: number, column: number, encoding: string) {
    const code = `U+${char.codePointAt(0)!.toString(16).toUpperCase().padStart(4, "0")}`;
    super(`the character "${char}" (${code}) in line ${line}, column ${column} cannot be saved in ${encoding}; remove it, or convert the file to UTF-8 in another editor`);
  }
}

/**
 * Encodes text in the encoding it was read with (see decode), so that saving keeps the
 * encoding of the file (K13): UTF-8 with or without BOM, UTF-16 LE/BE with BOM or
 * Windows-1252. A character Windows-1252 cannot hold is an EncodeError, not replaced.
 */
export function encode(text: string, encoding: string): Uint8Array<ArrayBuffer> {
  switch (encoding) {
    case "UTF-8 with BOM":
      return concat([0xef, 0xbb, 0xbf], new TextEncoder().encode(text));
    case "UTF-16 LE":
    case "UTF-16 BE": {
      const out = new Uint8Array(2 + text.length * 2);
      const le = encoding === "UTF-16 LE";
      const put = (i: number, u: number) => {
        out[i] = le ? u & 0xff : u >> 8;
        out[i + 1] = le ? u >> 8 : u & 0xff;
      };
      put(0, 0xfeff);
      for (let i = 0; i < text.length; i++) put(2 + i * 2, text.charCodeAt(i));
      return out;
    }
    case "Windows-1252": {
      const out = new Uint8Array(text.length);
      let n = 0;
      let line = 1;
      let column = 1;
      for (const c of text) {
        const u = c.codePointAt(0)!;
        // Bytes 0x81, 0x8D, 0x8F, 0x90 and 0x9D are read as the control characters of the same code.
        const b = u < 0x80 || (u >= 0xa0 && u <= 0xff) || [0x81, 0x8d, 0x8f, 0x90, 0x9d].includes(u) ? u : cp1252[c];
        if (b === undefined) throw new EncodeError(c, line, column, encoding);
        out[n++] = b;
        if (c === "\n") {
          line++;
          column = 1;
        } else column++;
      }
      return out.subarray(0, n);
    }
  }
  return new TextEncoder().encode(text);
}

function concat(head: number[], body: Uint8Array): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(head.length + body.length);
  out.set(head);
  out.set(body, head.length);
  return out;
}

/** Kind of a file by its path, as the server classifies it. */
export function kindOf(path: string): FileKind {
  if (path === ":runs") return "runs"; // the tab with the list of runs (S26e1)
  if (path.endsWith(".workflow.yaml")) return "workflow";
  if (path.endsWith(".pipeline.yaml")) return "pipeline";
  if (path === "project.yaml") return "project";
  if (/^environments\/[^/]+\.yaml$/.test(path)) return "environment";
  // Triggers also in subfolders of triggers/ (E12), not in hidden ones.
  if (/^triggers\/([^./][^/]*\/)*[^./][^/]*\.yaml$/.test(path)) return "trigger";
  // Connections (S20a, ADR 0022): connections/<name>.yaml.
  if (/^connections\/[^/]+\.yaml$/.test(path)) return "connection";
  // Plugins (S52b1): plugins/<name>/plugin.yaml opens as the overview of the plugin.
  if (/^plugins\/[^/]+\/plugin\.yaml$/.test(path)) return "plugin";
  if (/\.md$/i.test(path)) return "document";
  if (isImage(path)) return "image";
  if (isBinary(path)) return "binary";
  return "other";
}

export const kindLabel: Record<FileKind, string> = {
  workflow: "Workflow",
  pipeline: "Pipeline",
  project: "Project",
  environment: "Environment",
  trigger: "Trigger",
  connection: "Connection",
  plugin: "Plugin",
  document: "Document",
  image: "Image",
  binary: "Binary file",
  runs: "Runs",
  other: "File",
};

/** The name of a tab: the file name, "Runs" for the list of runs (S26e1). */
export const tabName = (path: string) => (path === ":runs" ? "Runs" : path.slice(path.lastIndexOf("/") + 1));

/** Images a document can show (as the server serves them, E17). */
export const isImage = (path: string) => /\.(png|jpe?g|gif|webp)$/i.test(path);

/**
 * Files the editor does not open (as the server's fileKind "binary"): programs, libraries,
 * archives, databases and office documents, e.g. the program of a plugin (S52).
 */
export const isBinary = (path: string) =>
  /\.(exe|dll|so|dylib|bin|msi|com|sys|zip|7z|gz|tgz|tar|rar|jar|class|pyc|db|sqlite3?|mdb|accdb|xlsx|xls|xlsm|docx|doc|pptx|pdf)$/i.test(path);

export const isYAML = (path: string) => /\.ya?ml$/i.test(path);

/** Full path of a project file for the clipboard, with the separator of the project directory. */
export function fullPath(dir: string, path: string): string {
  const sep = dir.includes("\\") ? "\\" : "/";
  return dir.replace(/[\\/]+$/, "") + sep + path.split("/").join(sep);
}

/**
 * Saves a file in its encoding (see encode; a character it cannot hold is an EncodeError
 * before anything is sent). etag is the ETag the text was loaded with ("*" overwrites
 * without check). A file changed in the meantime fails with APIError status 412. Returns
 * the new ETag.
 */
export async function saveFile(path: string, text: string, etag: string, encoding = "UTF-8"): Promise<string> {
  const body = encode(text, encoding);
  const res = await check(
    await fetch(fileURL(path), { method: "PUT", headers: { "If-Match": etag, "Content-Type": "application/octet-stream" }, body }),
  );
  return res.headers.get("ETag") ?? "*";
}

const encodePath = (path: string) => path.split("/").map(encodeURIComponent).join("/");
const postJSON = (url: string, body: unknown) =>
  fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });

/** Creates a file; fails if it exists (E11). */
export async function createFile(path: string, text: string): Promise<void> {
  await check(await fetch(fileURL(path), { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: text }));
}

/** Deletes a file for good (E11). */
export async function deleteFile(path: string): Promise<void> {
  await check(await fetch(fileURL(path), { method: "DELETE", headers: { "If-Match": "*" } }));
}

export async function createFolder(path: string): Promise<void> {
  await check(await postJSON(`${apiBase}/folders`, { path }));
}

/** Deletes an empty folder. */
export async function deleteFolder(path: string): Promise<void> {
  await check(await fetch(`${apiBase}/folders/${encodePath(path)}`, { method: "DELETE" }));
}

/** A reference to change when renaming: its file and line. */
export interface ReferenceKey {
  file: string;
  line: number;
}

export interface RenameResult {
  path: string;
  /** Files whose references were changed, at their new place. */
  updated: string[];
  notUpdated: (ReferenceKey & { reason: string })[];
}

/** Renames or moves a file or folder within the project; the given references are changed to the new path (E12). */
export async function renamePath(from: string, to: string, references: ReferenceKey[] = []): Promise<RenameResult> {
  const res = await check(await postJSON(`${apiBase}/rename`, { from, to, references }));
  return (await res.json()) as RenameResult;
}

/** A saved file that refers to another one, e.g. a trigger with it as target. */
export interface Reference {
  file: string;
  what: string;
  /** The field with the path: "target", "workflow" or "pipeline". */
  field: string;
  /** The file referred to; for a folder one of its files. */
  target: string;
  /** The path as written in the file. */
  value: string;
  line: number;
}

/** Where a path is after from was renamed or moved to to. */
export function movedPath(p: string, from: string, to: string): string {
  return p === from ? to : p.startsWith(from + "/") ? to + p.slice(from.length) : p;
}

export async function fetchReferences(path: string): Promise<Reference[]> {
  const res = await check(await fetch(`${apiBase}/references/${encodePath(path)}`));
  return ((await res.json()) as { references: Reference[] }).references;
}

/** What can be created in the explorer (mockup E11), with its folder and file name ending. */
export type NewKind = "workflow" | "pipeline" | "trigger" | "environment" | "document" | "file" | "folder";

export const newKinds: Record<NewKind, { label: string; folder: string; suffix: string; name: string }> = {
  workflow: { label: "Workflow", folder: "workflows", suffix: ".workflow.yaml", name: "new-workflow" },
  pipeline: { label: "Pipeline", folder: "pipelines", suffix: ".pipeline.yaml", name: "new-pipeline" },
  trigger: { label: "Trigger", folder: "triggers", suffix: ".yaml", name: "new-trigger" },
  environment: { label: "Environment", folder: "environments", suffix: ".yaml", name: "new-environment" },
  document: { label: "Document", folder: "docs", suffix: ".md", name: "new-document" },
  file: { label: "File", folder: "", suffix: "", name: "new-file.csv" },
  folder: { label: "Folder", folder: "", suffix: "", name: "new-folder" },
};

/**
 * The text of a new file: a small valid template with the name of the file (decision for
 * mockup E11). A new trigger points to the first workflow and starts disabled.
 */
export function template(kind: NewKind, name: string, firstWorkflow?: string): string {
  const head = (k: string) => `kind: ${k}\napiVersion: runnel/v1\nname: ${name}\n`;
  switch (kind) {
    case "workflow":
      return head("Workflow") + "actions:\n  - id: start\n    type: core.start\nlayout:\n  start: { x: 40, y: 120 }\n";
    case "pipeline":
      return head("Pipeline") + "transforms:\n  - id: rows\n    type: core.generate-rows\n    with:\n      count: 3\nlayout:\n  rows: { x: 40, y: 120 }\n";
    case "trigger":
      return (
        head("Trigger") +
        `description: Runs every hour once enabled\ntype: interval\ntarget: ${firstWorkflow ?? "workflows/main.workflow.yaml"}\nenabled: false\nwith:\n  every: 1h\n`
      );
    case "environment":
      return head("Environment") + "variables: {}\n";
    case "document":
      return `# ${name}\n`;
  }
  return "";
}

export const projectFile = "project.yaml";

/**
 * A new project.yaml (when it was deleted outside the editor): named after the project
 * folder, with characters other than letters, digits, "-" and "_" replaced by "-".
 */
export function projectTemplate(projectDir: string | null): string {
  const folder = (projectDir ?? "").split(/[\\/]/).filter(Boolean).pop() ?? "";
  const name = folder.replace(/[^A-Za-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "") || "project";
  return `kind: Project\napiVersion: runnel/v1\nname: ${name}\n`;
}

/** The kind and name ending of a file for renaming: the ending stays as it is. */
export function suffixOf(path: string): string {
  for (const s of [".workflow.yaml", ".pipeline.yaml"]) if (path.endsWith(s)) return s;
  if (/^environments\/[^/]+\.yaml$/.test(path) || kindOf(path) === "trigger") return ".yaml";
  return "";
}
