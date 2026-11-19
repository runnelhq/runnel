import dagre from "@dagrejs/dagre";
import { apiBase } from "./api";
import { APIError } from "./files";

/** The parts of a workflow the canvas shows (model of internal/model, as JSON). */
export interface Action {
  id: string;
  type: string;
  description?: string;
  with?: Record<string, unknown>;
  timeout?: string;
  retry?: { attempts?: number; backoff?: string; strategy?: string };
  continueOnError?: boolean;
  join?: string;
  joinTimeout?: string;
}

export interface Link {
  from: string;
  to: string;
  when?: string;
  loop?: boolean;
  disabled?: boolean;
}

export interface Workflow {
  name: string;
  actions?: Action[];
  links?: Link[];
  layout?: Record<string, { x: number; y: number }>;
}

export interface Problem {
  file: string;
  line?: number;
  column?: number;
  path?: string;
  severity: "error" | "warning";
  message: string;
}

export interface Validation {
  errors: number;
  warnings: number;
  problems: Problem[];
  workflow?: Workflow;
}

/** Checks a workflow on the server; content is the text shown in the editor. */
export async function validate(path: string, content: string): Promise<Validation> {
  const res = await fetch(`${apiBase}/validate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, content }),
  });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      // body is not JSON
    }
    throw new APIError(msg, res.status);
  }
  return (await res.json()) as Validation;
}

export type LinkKind = "success" | "failure" | "always" | "expression";

/** Kind of a link by its condition; no condition means success. */
export function linkKind(when: string | undefined): LinkKind {
  if (!when || when === "success") return "success";
  if (when === "failure" || when === "always") return when;
  return "expression";
}

/** Size of an action card on the canvas (design/mockups/e5-canvas.html). */
export const nodeWidth = 176;
export const nodeHeight = 52;

/** Index of the action (or link) a YAML path points to, e.g. "$.actions[2].with". */
export function pathIndex(path: string | undefined, list: "actions" | "links"): number | null {
  const m = path?.match(new RegExp(`^\\$\\.'?${list}'?\\[(\\d+)\\]`));
  return m ? Number(m[1]) : null;
}

/** Problems per action id; problems that belong to no action are returned separately. */
export function problemsByAction(wf: Workflow, problems: Problem[]) {
  const byAction = new Map<string, Problem[]>();
  for (const p of problems) {
    const i = pathIndex(p.path, "actions");
    const id = i === null ? undefined : wf.actions?.[i]?.id;
    if (id) byAction.set(id, [...(byAction.get(id) ?? []), p]);
  }
  return byAction;
}

export interface Placement {
  positions: Map<string, { x: number; y: number }>;
  /** Set when positions were computed because the file has no (complete) layout. */
  auto: "none" | "all" | "some";
}

/**
 * Positions of all actions: from the layout block (pixels, top-left corner), or
 * arranged automatically from left to right if the file has no layout for some actions.
 */
export function place(wf: Workflow): Placement {
  const actions = wf.actions ?? [];
  const layout = wf.layout ?? {};
  const missing = actions.filter((a) => !layout[a.id]);
  const positions = new Map<string, { x: number; y: number }>();
  if (missing.length === 0) {
    for (const a of actions) positions.set(a.id, layout[a.id]);
    return { positions, auto: "none" };
  }

  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: "LR", nodesep: 48, ranksep: 64, marginx: 40, marginy: 40 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const a of actions) g.setNode(a.id, { width: nodeWidth, height: nodeHeight });
  for (const l of wf.links ?? []) {
    if (!l.loop && g.hasNode(l.from) && g.hasNode(l.to)) g.setEdge(l.from, l.to);
  }
  dagre.layout(g);
  for (const a of actions) {
    const n = g.node(a.id);
    positions.set(a.id, layout[a.id] ?? { x: Math.round(n.x - nodeWidth / 2), y: Math.round(n.y - nodeHeight / 2) });
  }
  return { positions, auto: missing.length === actions.length ? "all" : "some" };
}

/** Changes made on the canvas (see model.WorkflowEdit). */
export interface WorkflowEdit {
  move?: Record<string, { x: number; y: number }>;
  delete?: string[];
  add?: { id: string; type: string }[];
  rename?: Record<string, string>;
  addLinks?: Link[];
  deleteLinks?: number[];
  updateLinks?: { index: number; when: "success" | "failure" | "always" }[];
  /** Changes one field of an action (properties panel); null removes the field. */
  set?: { id: string; field: string; value: string | number | boolean | null }[];
}

/** Allowed action ids (as checked by the server). */
export const idPattern = /^[A-Za-z][A-Za-z0-9_-]*$/;

/** A new id for an action of the given type: its name and the lowest free number, e.g. "log-1". */
export function newActionId(wf: Workflow, type: string): string {
  const name = type.slice(type.lastIndexOf(".") + 1);
  const used = new Set((wf.actions ?? []).map((a) => a.id));
  for (let i = 1; ; i++) {
    if (!used.has(`${name}-${i}`)) return `${name}-${i}`;
  }
}

/**
 * Whether a new link from → to would close a cycle, i.e. "to" already leads to "from"
 * over links that are neither loop links nor disabled. Such a link is a link back to an
 * earlier action and is created with loop: true (decision for mockup E6b).
 */
export function closesCycle(wf: Workflow, from: string, to: string): boolean {
  const next = new Map<string, string[]>();
  for (const l of wf.links ?? []) {
    if (!l.loop && !l.disabled) next.set(l.from, [...(next.get(l.from) ?? []), l.to]);
  }
  const seen = new Set<string>();
  const stack = [to];
  while (stack.length > 0) {
    const id = stack.pop()!;
    if (id === from) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    stack.push(...(next.get(id) ?? []));
  }
  return false;
}

/** Applies an edit to the text on the server and returns the new text (not saved). */
export async function editWorkflow(path: string, content: string, edit: WorkflowEdit): Promise<string> {
  const res = await fetch(`${apiBase}/workflows/edit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, content, ...edit }),
  });
  const body = (await res.json()) as { content?: string; error?: string };
  if (!res.ok) throw new APIError(body.error ?? `${res.status} ${res.statusText}`, res.status);
  return body.content ?? content;
}
