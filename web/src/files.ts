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
export type FileKind = "workflow" | "pipeline" | "project" | "environment" | "trigger" | "document" | "image" | "other";

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

/** Kind of a file by its path, as the server classifies it. */
export function kindOf(path: string): FileKind {
  if (path.endsWith(".workflow.yaml")) return "workflow";
  if (path.endsWith(".pipeline.yaml")) return "pipeline";
  if (path === "project.yaml") return "project";
  if (/^environments\/[^/]+\.yaml$/.test(path)) return "environment";
  // Triggers also in subfolders of triggers/ (E12), not in hidden ones.
  if (/^triggers\/([^./][^/]*\/)*[^./][^/]*\.yaml$/.test(path)) return "trigger";
  if (/\.md$/i.test(path)) return "document";
  if (isImage(path)) return "image";
  return "other";
}

export const kindLabel: Record<FileKind, string> = {
  workflow: "Workflow",
  pipeline: "Pipeline",
  project: "Project",
  environment: "Environment",
  trigger: "Trigger",
  document: "Document",
  image: "Image",
  other: "File",
};

/** Images a document can show (as the server serves them, E17). */
export const isImage = (path: string) => /\.(png|jpe?g|gif|webp)$/i.test(path);

export const isYAML = (path: string) => /\.ya?ml$/i.test(path);

/** Full path of a project file for the clipboard, with the separator of the project directory. */
export function fullPath(dir: string, path: string): string {
  const sep = dir.includes("\\") ? "\\" : "/";
  return dir.replace(/[\\/]+$/, "") + sep + path.split("/").join(sep);
}

/**
 * Saves a file. etag is the ETag the text was loaded with ("*" overwrites without check).
 * A file changed in the meantime fails with APIError status 412. Returns the new ETag.
 */
export async function saveFile(path: string, text: string, etag: string): Promise<string> {
  const res = await check(
    await fetch(fileURL(path), { method: "PUT", headers: { "If-Match": etag, "Content-Type": "application/octet-stream" }, body: text }),
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
      return head("Pipeline") + "transforms:\n  - id: rows\n    type: core.generate-rows\n    with:\n      rows: 3\nlayout:\n  rows: { x: 40, y: 120 }\n";
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
