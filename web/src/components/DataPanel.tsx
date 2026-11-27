import { useEffect, useState, type ReactNode } from "react";
import { tileColor, useCatalog } from "../catalog";
import { formatDuration, type RunView } from "../run";
import { fetchStep, joinPath, keyPath, plural, typeOf, type StepData, type StepSummary } from "../stepdata";
import { kindOfLink, type Workflow } from "../workflow";
import { ago } from "./SecretField";
import { ActionIcon, BracesIcon, ChevronIcon, CopyIcon, ExternalIcon, HistoryIcon, LockIcon, PlayIcon } from "./Icons";

/** The steps with data of a run; steps is null when the server keeps none for it. */
export interface StepsState {
  runId: string;
  steps: StepSummary[] | null;
}

const label = "text-2xs font-semibold tracking-[.06em] text-muted-foreground uppercase";
const smallButton =
  "inline-flex h-[22px] cursor-pointer items-center gap-1 rounded-md border border-input bg-background px-[7px] text-2xs font-medium whitespace-nowrap text-foreground hover:bg-muted disabled:cursor-default disabled:opacity-50";
const segment =
  "cursor-pointer rounded-sm border-0 bg-transparent px-[7px] py-px text-2xs whitespace-nowrap text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm";
const copy = (text: string) => void navigator.clipboard?.writeText(text);
const viewKey = "runnel.data.view";
type View = "tree" | "json";

const statusChip: Record<string, [string, string]> = {
  succeeded: ["✔ Succeeded", "bg-success-soft text-success"],
  failed: ["✖ Failed", "bg-danger-soft text-destructive"],
  skipped: ["– Skipped", "border border-dashed border-status-skipped bg-muted text-muted-foreground"],
  cancelled: ["■ Cancelled", "bg-muted text-muted-foreground"],
  running: ["Running", "bg-muted text-muted-foreground"],
};

/**
 * The tab "Data" below the canvas (mockup design/mockups/e23-step-data.html): for the step chosen on the canvas,
 * what reached it in the last run started in the editor (Input) and what it returned (Output as tree or JSON),
 * with the path of a value for expressions and its log lines. A step with forEach shows the data of the whole
 * step (the list of its entries), a step run more than once that of its last run.
 */
export function DataPanel(props: {
  workflow: Workflow;
  run: RunView | null;
  steps: StepsState | null;
  actionId: string | null;
  onSelect: (id: string) => void;
  onRun: () => void;
  onShowInLog: (id: string) => void;
  onOpenRuns: () => void;
}) {
  const { run, actionId, steps } = props;
  const summary = steps?.steps?.find((s) => s.actionId === actionId);
  const shown = useStep(summary && run ? run.id : null, actionId);

  const box = (content: ReactNode) => (
    <div data-testid="data" className="flex h-[412px] min-h-0 flex-col border-t border-border">
      {content}
    </div>
  );
  if (!run) {
    return box(
      <Empty icon={<BracesIcon width={20} height={20} />} title="No run yet">
        <p className="mt-0 mb-2.5">
          Run the workflow (F5) to see the data of each step: what reached it and what it returned. Select a step on the canvas to show its data here.
        </p>
        <button type="button" onClick={props.onRun} className={smallButton.replace("h-[22px]", "h-7") + " px-2.5 text-xs"}>
          <PlayIcon width={13} height={13} className="text-status-success" />
          Run <kbd className="font-mono text-[10px] opacity-75">F5</kbd>
        </button>
      </Empty>,
    );
  }
  if (run.status === "running") return box(<Empty icon={<BracesIcon width={20} height={20} />} title="The run has not ended yet">The data of each step shows here when it has ended.</Empty>);
  if (!actionId) return box(<Empty icon={<BracesIcon width={20} height={20} />} title="No step selected">Select a step on the canvas to show its data here.</Empty>);
  const type = props.workflow.actions?.find((a) => a.id === actionId)?.type ?? "";
  const act = run.actions[actionId];
  const runLink = (
    <span className="inline-flex items-center gap-1 text-2xs text-muted-foreground">
      Run
      <button type="button" title="Open this run in Runs" onClick={props.onOpenRuns} className="cursor-pointer border-0 bg-transparent p-0 font-mono text-2xs text-primary underline underline-offset-2">
        {run.id.slice(0, 12)}
      </button>
      {[run.environment, run.history?.trigger, ago(new Date(run.started).toISOString())].filter(Boolean).map((t) => ` · ${t}`)}
      <ExternalIcon width={11} height={11} />
    </span>
  );
  const headOf = (status: string, durationMs: number | undefined, d?: StepData) => (
    <Head id={actionId} type={type} status={status} durationMs={durationMs} data={d} retries={props.workflow.actions?.find((a) => a.id === actionId)?.retry?.attempts} runLink={runLink} />
  );
  const ran = (steps?.steps ?? []).filter((s) => s.status !== "skipped").map((s) => s.actionId);

  if (steps?.runId === run.id && steps.steps === null) {
    return box(
      <>
        {headOf(act?.status ?? "succeeded", act?.durationMs)}
        <Empty icon={<HistoryIcon width={20} height={20} />} title="No data kept for this run">
          <p className="mt-0 mb-2.5">
            The data of each step is kept only for the last run started in the editor, until the next run or a restart of the server. Runs in the history keep status, duration,
            error and log.
          </p>
          <button type="button" onClick={() => props.onShowInLog(actionId)} className={smallButton}>
            <ExternalIcon width={12} height={12} />
            Show in Log
          </button>
        </Empty>
      </>,
    );
  }
  if (act?.status === "skipped" || summary?.status === "skipped") {
    const incoming = (props.workflow.links ?? []).filter((l) => l.to === actionId && !l.disabled);
    return box(
      <>
        {headOf("skipped", undefined)}
        <Empty icon="–" title={`Not run: ${shown?.reason ?? "no incoming link applies"}`}>
          <p className="mt-0 mb-1">A step runs when one of its incoming links applies. In this run:</p>
          <div className="mx-auto mt-1 mb-2.5 inline-grid gap-1 text-left">
            {incoming.map((l, i) => {
              const kind = kindOfLink(l);
              return (
                <div key={i} data-testid="skipped-link" className="flex items-center gap-2 text-2xs">
                  <span className={"grid size-[18px] place-items-center rounded-full border-[1.5px] border-current bg-card text-[10px] font-bold " + (kind === "failure" ? "text-link-failure" : "text-link-success")}>
                    {kind === "failure" ? "✖" : "✔"}
                  </span>
                  <span className="font-mono text-foreground">
                    {l.from} → {l.to}
                  </span>
                  {linkKindText[kind] ?? `If ${l.when}`} · <span className="font-mono">{l.from}</span> {run.actions[l.from]?.status ?? "not run"}
                </div>
              );
            })}
          </div>
          <p className="m-0">
            Without a run there is no input or output.
            {ran.length > 0 && " Data shows the steps that ran: "}
            {ran.map((id, i) => (
              <span key={id}>
                {i > 0 && ", "}
                <button type="button" onClick={() => props.onSelect(id)} className="cursor-pointer border-0 bg-transparent p-0 text-xs text-primary underline underline-offset-2">
                  {id}
                </button>
              </span>
            ))}
            {ran.length > 0 && "."}
          </p>
        </Empty>
      </>,
    );
  }
  if (!summary) {
    return box(
      <>
        {headOf(act?.status ?? "skipped", act?.durationMs)}
        <Empty icon={<BracesIcon width={20} height={20} />} title={steps ? "No data for this step" : "Loading…"}>
          {steps ? "The step has no data in this run." : null}
        </Empty>
      </>,
    );
  }
  if (!shown || shown.actionId !== actionId || shown.runId !== run.id) return box(<Empty icon={<BracesIcon width={20} height={20} />} title="Loading…" />);

  const outBase = `${keyPath("results", actionId)}.output`;
  const failed = shown.status === "failed" && !!shown.error;
  const lines = run.log.filter((l) => l.actionId === actionId);
  return box(
    <>
      {headOf(shown.status, shown.durationMs, shown)}
      <div className="grid min-h-0 flex-1 grid-cols-[340px_1fr]">
        <InputPane key={actionId} data={shown} environment={run.environment} forEach={!!shown.entries?.length} />
        <div className="flex min-h-0 min-w-0 flex-col border-l border-border">
          {failed ? <ErrorPane id={actionId} data={shown} /> : <OutputPane key={outBase} data={shown} base={outBase} />}
        </div>
      </div>
      <StepLog key={`${actionId}|${failed}`} lines={lines} open={failed} onShowInLog={() => props.onShowInLog(actionId)} />
    </>,
  );
}

const linkKindText: Record<string, string> = { success: "On success", failure: "On failure", always: "Always" };

/** Loads the data of a step (its last execution); the last one stays while the next loads. */
function useStep(runId: string | null, action: string | null): StepData | null {
  const [data, setData] = useState<StepData | null>(null);
  useEffect(() => {
    if (!runId || !action) return setData(null);
    let live = true;
    fetchStep(runId, action).then(
      (d) => live && setData(d),
      () => live && setData(null),
    );
    return () => {
      live = false;
    };
  }, [runId, action]);
  return data;
}

function Empty(props: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="grid min-h-0 flex-1 place-items-center overflow-auto p-4">
      <div className="max-w-[460px] text-center text-xs text-muted-foreground">
        <div className="mb-2 inline-grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">{props.icon}</div>
        <h3 className="mt-0 mb-1 text-sm font-semibold text-foreground">{props.title}</h3>
        {props.children}
      </div>
    </div>
  );
}

function Head(props: { id: string; type: string; status: string; durationMs?: number; data?: StepData; retries?: number; runLink: ReactNode }) {
  const d = useCatalog("workflow").get(props.type);
  const [text, cls] = statusChip[props.status] ?? statusChip.cancelled;
  const attempts = props.data?.attempts ?? 0;
  const sep = <span className="text-border">|</span>;
  return (
    <div data-testid="data-head" className="flex h-10 shrink-0 items-center gap-2.5 border-b border-border px-3 whitespace-nowrap">
      <span className="grid size-6 shrink-0 place-items-center rounded-sm" style={{ background: d ? tileColor(d) : "var(--rn-kategorie-allgemein)", color: "var(--rn-tiefe-0)" }}>
        <ActionIcon type={props.type} known={!!d} width={14} height={14} />
      </span>
      <span className="flex items-baseline gap-1.5">
        <b className="font-semibold">{props.id}</b>
        <span className="font-mono text-2xs text-muted-foreground">{props.type}</span>
      </span>
      <span className="flex items-center gap-2 text-xs text-muted-foreground">
        <span data-testid="data-status" className={`inline-flex h-5 items-center rounded-full px-2 text-2xs font-semibold ${cls}`}>
          {text}
        </span>
        {props.status !== "skipped" && props.durationMs !== undefined && (
          <>
            {sep}
            <span>{formatDuration(props.durationMs)}</span>
          </>
        )}
        {attempts > 1 && (
          <>
            {sep}
            <span
              data-testid="data-attempts"
              title={`Attempt 1 of ${Math.max(attempts, props.retries ?? 0)} failed: ${props.data?.attemptErrors?.[0] ?? ""}. Input and output are those of the last attempt.`}
              className="inline-flex items-center gap-1"
            >
              <span className="font-bold text-warning">⚠</span>
              {attempts} of {Math.max(attempts, props.retries ?? 0)} attempts
            </span>
          </>
        )}
      </span>
      <span className="flex-1" />
      {props.runLink}
    </div>
  );
}

function PaneHead(props: { title: string; sub: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex h-8 shrink-0 items-center gap-2 pr-2 pl-3 whitespace-nowrap">
      <span className={label}>{props.title}</span>
      <span className="truncate text-2xs text-muted-foreground">{props.sub}</span>
      <span className="flex-1" />
      {props.children}
    </div>
  );
}

function Note(props: { tone: "info" | "err"; children: ReactNode }) {
  const look = { info: ["i", "bg-muted text-muted-foreground", "rounded-full border-[1.5px] border-current text-[10px]"], err: ["✖", "bg-danger-soft text-foreground", "text-[13px] text-destructive"] }[props.tone];
  return (
    <div role={props.tone === "info" ? undefined : "note"} className={`mx-2.5 my-2 grid grid-cols-[18px_1fr] gap-2 rounded-md px-2.5 py-2 text-2xs leading-normal ${look[1]}`}>
      <span className={`grid size-4 place-items-center font-bold ${look[2]}`}>{look[0]}</span>
      <div>{props.children}</div>
    </div>
  );
}

// ---------- Input ----------

interface InputContext {
  sources: Record<string, string>;
  secrets: Set<string>;
}

function InputPane(props: { data: StepData; environment: string; forEach: boolean }) {
  const { data } = props;
  const order = Object.keys(useCatalog("workflow").get(data.type)?.schema?.properties ?? {});
  const [connOpen, setConnOpen] = useState(false);
  const input = data.input ?? {};
  const keys = Object.keys(input).sort((a, b) => (order.indexOf(a) + 1 || 999) - (order.indexOf(b) + 1 || 999));
  const ctx: InputContext = { sources: data.inputSources ?? {}, secrets: new Set(data.secrets) };
  const c = data.connection;
  return (
    <div className="flex min-h-0 min-w-0 flex-col">
      <PaneHead title="Input" sub={<><span className="font-mono">with:</span> as it reached the step</>} />
      <div data-testid="data-input" className="min-h-0 flex-1 overflow-auto border-t border-border">
        {!data.input && <p className="m-0 px-3 py-2 text-2xs text-muted-foreground">{props.forEach ? "Each entry of forEach has an input of its own." : "No input."}</p>}
        {keys.map((k) => (
          <div key={k}>
            <InputEntry name={k} value={input[k]} path={keyPath("", k)} depth={0} ctx={ctx} />
            {k === "connection" && c && (
              <>
                <button
                  type="button"
                  aria-expanded={connOpen}
                  onClick={() => setConnOpen(!connOpen)}
                  className="flex h-[26px] w-full cursor-pointer items-center gap-1.5 border-0 border-b border-border bg-muted px-3 text-left text-2xs font-semibold text-muted-foreground"
                >
                  <ChevronIcon open={connOpen} width={11} height={11} className="shrink-0" />
                  Connection <span className="font-mono font-medium text-foreground">{c.name}</span>
                  <span className="ml-auto shrink-0 font-normal">{`${c.type} · as resolved for ${props.environment || "the run"}`}</span>
                </button>
                {connOpen && <InputEntries obj={c.with} path="" depth={1} ctx={{ sources: {}, secrets: new Set(c.secrets) }} />}
              </>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function InputEntries(props: { obj: Record<string, unknown> | unknown[]; path: string; depth: number; ctx: InputContext }) {
  const list = Array.isArray(props.obj) ? props.obj.map((v, i) => [i, v] as const) : Object.entries(props.obj);
  return list.map(([k, v]) => <InputEntry key={k} name={typeof k === "number" ? `[${k}]` : k} value={v} path={keyPath(props.path, k)} depth={props.depth} ctx={props.ctx} />);
}

function InputEntry(props: { name: string; value: unknown; path: string; depth: number; ctx: InputContext }) {
  const { value, ctx, path } = props;
  const [open, setOpen] = useState(true);
  const secret = ctx.secrets.has(path);
  const t = typeOf(value);
  const group = !secret && (t === "object" || t === "list");
  const src = ctx.sources[path];
  const size = group ? (t === "list" ? plural((value as unknown[]).length, "entry", "entries") : plural(Object.keys(value as object).length, "key")) : "";
  return (
    <>
      <div data-testid={`input-${path}`} className="grid grid-cols-[112px_1fr] gap-2 border-b border-border px-3 py-1 text-2xs">
        <span title={props.name} className={"truncate pt-px font-mono " + (props.depth > 0 ? "text-muted-foreground" : "font-medium")} style={{ paddingLeft: props.depth * 12 }}>
          {props.depth > 0 ? "└ " : ""}
          {props.name}
        </span>
        <span className="min-w-0 font-mono [overflow-wrap:anywhere]">
          {secret ? (
            <span className="inline-flex items-center gap-[5px]" title="The value never leaves the server">
              <LockIcon width={12} height={12} className="text-muted-foreground" />
              <span className="tracking-[.1em]">***</span>
              {typeof value === "string" && value.includes("${") && <span className="text-[10px] text-expression">{value}</span>}
            </span>
          ) : group ? (
            <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="inline-flex cursor-pointer items-center gap-1 border-0 bg-transparent p-0 font-sans text-2xs text-muted-foreground">
              {size}
              <ChevronIcon open={open} width={10} height={10} />
            </button>
          ) : t === "string" ? (
            (value as string).includes("\n") ? (
              <span className="block whitespace-pre-wrap">{(value as string).replace(/\n$/, "")}</span>
            ) : (
              <span>"{value as string}"</span>
            )
          ) : (
            <span className={t === "null" ? "font-sans text-muted-foreground italic" : "text-expression"}>{String(value ?? "null")}</span>
          )}
          {src && (
            <span title={src} className="mt-px block truncate text-[10px] whitespace-nowrap text-expression">
              <span className="font-sans text-muted-foreground">from </span>
              {src}
            </span>
          )}
        </span>
      </div>
      {group && open && <InputEntries obj={value as Record<string, unknown>} path={path} depth={props.depth + 1} ctx={ctx} />}
    </>
  );
}

// ---------- Output ----------

function OutputPane(props: { data: StepData; base: string }) {
  const { data } = props;
  const [view, setView] = useState<View>(() => {
    try {
      const v = localStorage.getItem(viewKey);
      return v === "json" ? v : "tree";
    } catch {
      return "tree";
    }
  });
  const [path, setPath] = useState<string | null>(null);
  const choose = (v: View) => {
    setView(v);
    try {
      localStorage.setItem(viewKey, v);
    } catch {
      // only a convenience
    }
  };
  const value = data.output;
  const select = (rel: string) => setPath(joinPath(props.base, rel));
  return (
    <>
      <PaneHead title="Output" sub={<span className="font-mono">result.output</span>}>
        <div role="group" aria-label="View" className="inline-flex gap-0.5 rounded-md border border-border bg-muted p-0.5">
          {(["tree", "json"] as const).map((v) => (
            <button key={v} type="button" aria-pressed={view === v} onClick={() => choose(v)} className={segment}>
              {v === "json" ? "JSON" : v[0].toUpperCase() + v.slice(1)}
            </button>
          ))}
        </div>
        <button type="button" title="Copy the output as JSON" onClick={() => copy(JSON.stringify(value ?? null, null, 2))} className={smallButton}>
          <CopyIcon width={12} height={12} />
          Copy
        </button>
      </PaneHead>
      <div data-testid="data-output" className="min-h-0 flex-1 overflow-auto border-t border-border">
        {value === undefined || value === null ? (
          <p className="m-0 px-3 py-2 text-2xs text-muted-foreground">No output.</p>
        ) : view === "json" ? (
          <JSONView value={value} />
        ) : (
          <Tree value={value} selected={path} base={props.base} onSelect={select} />
        )}
      </div>
      <div className="flex h-8 shrink-0 items-center gap-1.5 border-t border-border bg-muted pr-2 pl-3 text-2xs whitespace-nowrap text-muted-foreground">
        <span>Path</span>
        {path ? (
          <span data-testid="data-path" title={path} className="min-w-0 truncate rounded-sm bg-expression-soft px-1.5 py-px font-mono text-expression">
            {path}
          </span>
        ) : (
          <span className="truncate">Click a value to show its path for expressions</span>
        )}
        <span className="flex-1" />
        <button type="button" disabled={!path} onClick={() => path && copy(path)} className={smallButton}>
          <CopyIcon width={12} height={12} />
          Copy path
        </button>
        <button type="button" disabled={!path} title="Copy as {{ … }} to paste into a field" onClick={() => path && copy(`{{ ${path} }}`)} className={smallButton}>
          {"{{ }}"}
        </button>
      </div>
    </>
  );
}

function Tree(props: { value: unknown; selected: string | null; base: string; onSelect: (rel: string) => void }) {
  const [open, setOpen] = useState(() => new Set<string>());
  const [more, setMore] = useState<Record<string, number>>({});
  const toggle = (rel: string) => setOpen((o) => new Set(o.has(rel) ? [...o].filter((x) => x !== rel) : [...o, rel]));
  const rows: ReactNode[] = [];
  const row = (key: string | number, v: unknown, rel: string, depth: number) => {
    const t = typeOf(v);
    const container = t === "list" || t === "object";
    const isOpen = open.has(rel);
    const full = joinPath(props.base, rel);
    const n = container ? Object.keys(v as object).length : 0;
    rows.push(
      <div
        key={rel}
        role="treeitem"
        aria-expanded={container ? isOpen : undefined}
        aria-selected={props.selected === full}
        title={full}
        onClick={() => {
          props.onSelect(rel);
          if (container) toggle(rel);
        }}
        className="grid min-h-6 cursor-pointer grid-cols-[minmax(150px,210px)_64px_1fr] items-center pr-2.5 pl-1 font-mono text-2xs whitespace-nowrap hover:bg-muted aria-selected:bg-accent aria-selected:text-accent-foreground"
      >
        <span className="flex items-center gap-0.5 overflow-hidden">
          <span className="shrink-0" style={{ width: depth * 14 }} />
          <span className="inline-grid w-3.5 shrink-0 place-items-center text-muted-foreground">{container && <ChevronIcon open={isOpen} width={11} height={11} />}</span>
          {typeof key === "number" ? <span className="text-muted-foreground">[{key}]</span> : key}
        </span>
        <span className="text-[10px] text-muted-foreground">{t}</span>
        <span className={"truncate " + (container || t === "null" ? "font-sans text-muted-foreground italic" : t === "string" ? "" : "text-expression")}>
          {t === "list" ? plural(n, "entry", "entries") : t === "object" ? plural(n, "key") : t === "string" ? `"${v as string}"` : String(v ?? "null")}
        </span>
      </div>,
    );
    if (container && isOpen) children(v, rel, depth + 1);
  };
  const children = (v: unknown, rel: string, depth: number) => {
    const list = Array.isArray(v) ? v.map((x, i) => [i, x] as const) : Object.entries(v as object);
    const limit = Array.isArray(v) ? (more[rel] ?? 100) : list.length;
    for (const [k, x] of list.slice(0, limit)) row(k, x, keyPath(rel, k), depth);
    if (list.length > limit) {
      rows.push(
        <div key={rel + "…"} className="grid min-h-6 grid-cols-[minmax(150px,210px)_64px_1fr] items-center pr-2.5 pl-1 text-2xs">
          <span className="font-mono text-muted-foreground" style={{ paddingLeft: depth * 14 + 16 }}>
            …
          </span>
          <span />
          <span className="text-muted-foreground italic">
            {`${list.length - limit} more entries`}
            <button type="button" onClick={() => setMore((m) => ({ ...m, [rel]: limit + 100 }))} className="ml-1.5 cursor-pointer border-0 bg-transparent p-0 text-2xs text-primary not-italic underline underline-offset-2">
              Show more
            </button>
          </span>
        </div>,
      );
    }
  };
  if (props.value !== null && typeof props.value === "object") children(props.value, "", 0);
  else row("output", props.value, "", 0);
  return <div role="tree" aria-label="Output">{rows}</div>;
}

/** The output as JSON: keys stand out, numbers and true/false/null in the colour of expressions. */
function JSONView(props: { value: unknown }) {
  const text = JSON.stringify(props.value, null, 2);
  const parts = text.split(/("(?:[^"\\]|\\.)*"(?=:)|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|\btrue\b|\bfalse\b|\bnull\b)/g);
  return (
    <pre data-testid="data-json" className="m-0 px-3 py-2 font-mono text-2xs leading-relaxed text-foreground">
      {parts.map((p, i) => (i % 2 === 0 ? p : p.startsWith('"') ? <span key={i} className="font-medium">{p}</span> : <span key={i} className="text-expression">{p}</span>))}
    </pre>
  );
}

function ErrorPane(props: { id: string; data: StepData }) {
  const err = keyPath("results", props.id);
  return (
    <>
      <PaneHead title="Error" sub="of the step">
        <button type="button" title="Copy the error" onClick={() => copy(props.data.error ?? "")} className={smallButton}>
          <CopyIcon width={12} height={12} />
          Copy
        </button>
      </PaneHead>
      <div data-testid="data-error" className="min-h-0 flex-1 overflow-auto border-t border-border">
        <Note tone="err">
          <b className="font-semibold break-words text-destructive">{props.data.error}</b>
          <div className="mt-1.5 text-muted-foreground">No output: the step failed.</div>
        </Note>
        <Note tone="info">
          In expressions the error is <span className="font-mono text-expression">{err}.error</span>.
        </Note>
      </div>
    </>
  );
}

function StepLog(props: { lines: RunView["log"]; open: boolean; onShowInLog: () => void }) {
  const [open, setOpen] = useState(props.open);
  return (
    <div data-testid="data-log" className="shrink-0 border-t border-border">
      <div className="flex h-7 items-center gap-1.5 px-3 text-2xs text-muted-foreground">
        <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent p-0 text-2xs text-muted-foreground">
          <ChevronIcon open={open} width={11} height={11} />
          <b className="font-medium text-foreground">Log of this step</b>
 · {plural(props.lines.length, "line")}
        </button>
        <button type="button" onClick={props.onShowInLog} className="ml-auto cursor-pointer border-0 bg-transparent p-0 text-2xs text-primary underline underline-offset-2">
          Show in Log
        </button>
      </div>
      {open && (
        <div className="max-h-[132px] overflow-auto">
          {props.lines.map((l, i) => (
            <div key={i} className={"grid min-h-[22px] grid-cols-[88px_44px_1fr] items-center gap-2 px-3 font-mono text-2xs whitespace-nowrap " + (l.level === "error" ? "bg-danger-soft" : "")}>
              <span className="text-muted-foreground">{l.time.slice(11, 23)}</span>
              <span className={"font-semibold " + (l.level === "error" ? "text-destructive" : l.level === "warn" ? "text-warning" : l.level === "debug" ? "text-muted-foreground" : "")}>{l.level}</span>
              <span title={l.message} className="truncate">
                {l.message}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
