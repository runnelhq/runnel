import { apiBase } from "./api";
import { APIError } from "./files";
import type { Counts, RunView } from "./run";
import type { Workflow } from "./workflow";

/** Records of a link or of the output of the previewed transform (POST /preview, S44). */
export interface PreviewRecords {
  /** Records that went over the link; at most the limit are shown. */
  rows: number;
  fields: { name: string; type: string }[];
  records: unknown[][];
}

export interface PreviewResult {
  status: "succeeded" | "failed" | "cancelled";
  error?: string;
  failedAt?: string;
  durationMs: number;
  limit: number;
  counts: Record<string, Counts>;
  links: Record<string, PreviewRecords>;
  output: PreviewRecords;
}

/** Runs a pipeline up to a transform with the first records of its inputs; nothing is written. */
export async function runPreview(req: {
  path: string;
  content: string;
  transform: string;
  limit: number;
  environment?: string;
  params?: Record<string, string>;
}): Promise<PreviewResult> {
  const res = await fetch(`${apiBase}/preview`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const body = (await res.json()) as PreviewResult & { error?: string; problems?: { message: string }[] };
  if (!res.ok) {
    const first = body.problems?.find(Boolean)?.message;
    throw new APIError(first ? `${body.error ?? "the pipeline has problems"}: ${first}` : (body.error ?? `${res.status} ${res.statusText}`), res.status);
  }
  return body;
}

/**
 * The preview as a run for the canvas: the transforms of the preview with their counts and
 * status, the others pending (shown faded), and the records of the links that ran.
 */
export function previewRun(result: PreviewResult, pipeline: Workflow, upTo: string): RunView {
  const actions: RunView["actions"] = {};
  for (const a of pipeline.actions ?? []) {
    const counts = result.counts[a.id];
    actions[a.id] = counts
      ? { status: result.failedAt === a.id ? "failed" : result.failedAt ? "cancelled" : "succeeded", counts }
      : { status: "pending" };
  }
  const linkRows: Record<number, number> = {};
  for (const [i, recs] of Object.entries(result.links)) linkRows[Number(i)] = recs.rows;
  return {
    id: "preview",
    kind: "pipeline",
    workflow: upTo,
    environment: "",
    status: result.status === "succeeded" ? "succeeded" : "failed",
    started: 0,
    durationMs: result.durationMs,
    error: result.error,
    actions,
    linkRows,
    log: [],
    signature: "",
  };
}
