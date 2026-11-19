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

export type FileKind = "workflow" | "pipeline" | "project" | "environment" | "other";

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
  return "other";
}

export const kindLabel: Record<FileKind, string> = {
  workflow: "Workflow",
  pipeline: "Pipeline",
  project: "Project",
  environment: "Environment",
  other: "File",
};

export const isYAML = (path: string) => /\.ya?ml$/i.test(path);

/** Full path of a project file for the clipboard, with the separator of the project directory. */
export function fullPath(dir: string, path: string): string {
  const sep = dir.includes("\\") ? "\\" : "/";
  return dir.replace(/[\\/]+$/, "") + sep + path.split("/").join(sep);
}
