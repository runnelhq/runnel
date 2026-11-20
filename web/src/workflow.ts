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
  /** Pipelines: instances that share the incoming records (0 or missing: 1). */
  copies?: number;
  /** Pipelines: copy (default) or distribute, for several outgoing data links. */
  distribution?: string;
}

export interface Link {
  from: string;
  to: string;
  when?: string;
  loop?: boolean;
  disabled?: boolean;
  /** Pipelines: data (default) or error. */
  type?: string;
}

export interface Parameter {
  name: string;
  description?: string;
  type?: string;
  default?: string;
  required?: boolean;
  enum?: string[];
}

export interface Output {
  name: string;
  description?: string;
  type?: string;
  value: string;
}

/** Workflows and pipelines are shown on the same canvas; kind tells them apart. */
export type GraphKind = "workflow" | "pipeline";

export interface Workflow {
  /** Set by graphOf: a pipeline has transforms as "actions" and data or error links. */
  kind?: GraphKind;
  name: string;
  description?: string;
  parameters?: Parameter[];
  outputs?: Output[];
  settings?: { timeout?: string; maxConcurrentRuns?: number; batchSize?: number; bufferSize?: number; strict?: boolean };
  actions?: Action[];
  links?: Link[];
  layout?: Record<string, { x: number; y: number }>;
}

/** A pipeline as returned by POST /validate (model.Pipeline). */
export interface Pipeline {
  name: string;
  description?: string;
  parameters?: Parameter[];
  transforms?: Action[];
  links?: { from: string; to: string; type?: string }[];
  settings?: { batchSize?: number; bufferSize?: number; strict?: boolean };
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
  pipeline?: Pipeline;
  /** Pipelines: the fields around each transform, by id; missing while the file has errors in its fields or links. */
  schemas?: Record<string, TransformSchemas>;
}

/** Fields of the records on a link (record.Schema, ADR 0007). */
export interface RecordSchema {
  fields: { name: string; type?: string }[] | null;
  /** Further fields may occur, known only when the pipeline runs. */
  open?: boolean;
}

/** The schemas around a transform (pipeline.TransformSchemas). */
export interface TransformSchemas {
  /** Merged schema of the incoming records; missing for a transform without incoming links. */
  input?: RecordSchema;
  /** Records on its data links. */
  output: RecordSchema;
  /** Records on its error links. */
  errors: RecordSchema;
}

/**
 * Names of fields a configuration refers to: as record.<name> in an expression, or as a
 * value of its own (e.g. a name in a list of fields).
 */
export function usedFields(value: unknown, out = new Set<string>()): Set<string> {
  if (typeof value === "string") {
    out.add(value);
    for (const m of value.matchAll(/\brecord\.([A-Za-z_]\w*)/g)) out.add(m[1]);
  } else if (Array.isArray(value)) {
    for (const v of value) usedFields(v, out);
  } else if (value && typeof value === "object") {
    for (const v of Object.values(value)) usedFields(v, out);
  }
  return out;
}

/**
 * The graph of a validated file for the canvas: a workflow as it is, a pipeline with its
 * transforms as actions and every link with its type (data if not given).
 */
export function graphOf(v: Validation | null): Workflow | undefined {
  if (v?.workflow) return { ...v.workflow, kind: "workflow" };
  if (!v?.pipeline) return undefined;
  const { transforms, links, ...rest } = v.pipeline;
  return { ...rest, kind: "pipeline", actions: transforms, links: links?.map((l) => ({ ...l, type: l.type || "data" })) };
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

export type LinkKind = "success" | "failure" | "always" | "expression" | "data" | "error";

/** Kind of a link by its condition; no condition means success. */
export function linkKind(when: string | undefined): LinkKind {
  if (!when || when === "success") return "success";
  if (when === "failure" || when === "always") return when;
  return "expression";
}

/** Kind of a link of a workflow (by its condition) or of a pipeline (by its type). */
export function kindOfLink(l: Link): LinkKind {
  if (l.type !== undefined) return l.type === "error" ? "error" : "data";
  return linkKind(l.when);
}

/** Size of an action card on the canvas (design/mockups/e5-canvas.html). */
export const nodeWidth = 176;
export const nodeHeight = 52;

/** Name of the list of steps in the file: actions in a workflow, transforms in a pipeline. */
export const stepsKey = (wf: Workflow) => (wf.kind === "pipeline" ? "transforms" : "actions");

/** Index of the action, transform or link a YAML path points to, e.g. "$.actions[2].with". */
export function pathIndex(path: string | undefined, list: "actions" | "transforms" | "links"): number | null {
  const m = path?.match(new RegExp(`^\\$\\.'?${list}'?\\[(\\d+)\\]`));
  return m ? Number(m[1]) : null;
}

/** Problems per action (or transform) id; problems that belong to none are not returned. */
export function problemsByAction(wf: Workflow, problems: Problem[]) {
  const byAction = new Map<string, Problem[]>();
  for (const p of problems) {
    const i = pathIndex(p.path, stepsKey(wf));
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
  /** when: success, failure, always or an expression "{{ … }}"; type (pipelines): data or error. */
  updateLinks?: { index: number; when?: string; loop?: boolean; disabled?: boolean; type?: string }[];
  /** Changes one field of an action (properties panel); null removes the field. */
  set?: { id: string; field: string; value: string | number | boolean | null }[];
  /** Changes one field of the workflow itself (name, description, settings.*). */
  setWorkflow?: { field: string; value: string | number | boolean | null }[];
}

/** What a problem belongs to: an action, a link or the workflow, and the field in the properties panel. */
export interface ProblemTarget {
  action?: string;
  link?: number;
  /** Key of the field (data-field in the panel), e.g. "with.message", "timeout", "when", "settings.timeout". */
  field?: string;
}

/** Finds the action, link or workflow field a problem refers to, from its YAML path. */
export function problemTarget(wf: Workflow, p: Problem): ProblemTarget | null {
  const path = p.path;
  if (!path) return null;
  let m = path.match(/^\$\.(?:actions|transforms)\[(\d+)\](?:\.(.*))?$/);
  if (m) {
    const action = wf.actions?.[Number(m[1])]?.id;
    if (!action) return null;
    const rest = m[2] ?? "";
    let field: string | undefined;
    if (rest === "with") field = p.message.match(/field "([^"]+)"/)?.[1]?.replace(/^/, "with.");
    else if (rest.startsWith("with.")) field = rest.split(".").slice(0, 2).join(".");
    else if (rest === "retry.strategy") field = "retry.strategy";
    else if (rest.startsWith("retry")) field = "retry";
    else if (rest) field = rest.split(".")[0];
    return { action, field };
  }
  m = path.match(/^\$\.links\[(\d+)\](?:\.(\w+))?/);
  if (m) {
    const field = m[2] === "from" || m[2] === "to" ? undefined : m[2];
    return { link: Number(m[1]), field };
  }
  m = path.match(/^\$\.(name|description|settings\.\w+|parameters|outputs)/);
  if (m) return { field: m[1] };
  return {};
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

/** Changes made on the canvas of a pipeline (see model.PipelineEdit). */
export interface PipelineEdit {
  move?: Record<string, { x: number; y: number }>;
  delete?: string[];
  add?: { id: string; type: string }[];
  rename?: Record<string, string>;
  addLinks?: { from: string; to: string; type?: string }[];
  deleteLinks?: number[];
  updateLinks?: { index: number; type: string }[];
  /** copies, distribution or with.<name>; null removes the field. */
  set?: { id: string; field: string; value: string | number | boolean | null }[];
  /** name, description or settings.*. */
  setPipeline?: { field: string; value: string | number | boolean | null }[];
}

/**
 * Translates an edit of the canvas into one of a pipeline: links carry their type
 * (data links are written without it), and fields of the graph are fields of the pipeline.
 */
export function pipelineEdit(edit: WorkflowEdit): PipelineEdit {
  const { addLinks, updateLinks, setWorkflow, ...rest } = edit;
  return {
    ...rest,
    addLinks: addLinks?.map((l) => ({ from: l.from, to: l.to, ...(l.type === "error" ? { type: "error" } : {}) })),
    updateLinks: updateLinks?.flatMap((u) => (u.type ? [{ index: u.index, type: u.type }] : [])),
    setPipeline: setWorkflow,
  };
}

/** Applies an edit to the text on the server and returns the new text (not saved). */
export async function editWorkflow(path: string, content: string, edit: WorkflowEdit, kind: GraphKind = "workflow"): Promise<string> {
  const payload = kind === "pipeline" ? pipelineEdit(edit) : edit;
  const res = await fetch(`${apiBase}/${kind === "pipeline" ? "pipelines" : "workflows"}/edit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, content, ...payload }),
  });
  const body = (await res.json()) as { content?: string; error?: string };
  if (!res.ok) throw new APIError(body.error ?? `${res.status} ${res.statusText}`, res.status);
  return body.content ?? content;
}
