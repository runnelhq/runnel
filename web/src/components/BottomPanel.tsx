import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { formatDuration, type RunView } from "../run";
import { problemTarget, type Problem, type Workflow } from "../workflow";
import { ChevronIcon } from "./Icons";

/** Names of fields for "Where", as labelled in the properties panel. */
const fieldLabels: Record<string, string> = {
  id: "ID",
  description: "Description",
  timeout: "Timeout",
  retry: "Retry",
  "retry.strategy": "Backoff strategy",
  continueOnError: "Continue on error",
  join: "Join",
  joinTimeout: "Join timeout",
  when: "Condition",
  loop: "Loop link",
  disabled: "Disabled",
  name: "Name",
  "settings.timeout": "Timeout",
  "settings.maxConcurrentRuns": "Max. concurrent runs",
  parameters: "Parameters",
  outputs: "Outputs",
};

export function fieldLabel(key: string): string {
  if (fieldLabels[key]) return fieldLabels[key];
  const name = key.replace(/^with\./, "");
  return name.charAt(0).toUpperCase() + name.slice(1).replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase();
}

/** Where a problem is, e.g. "notify · Message", "Link #3 · Condition" or "Workflow · Name". */
function where(wf: Workflow, p: Problem): string {
  const t = problemTarget(wf, p);
  if (!t) return "—";
  // A field of a parameter or output: its name and the field, e.g. "COUNT · Default".
  const entry = t.field?.match(/^(parameters|outputs)\[(\d+)\]\.(\w+)$/);
  if (entry) {
    const list = entry[1] === "parameters" ? wf.parameters : wf.outputs;
    return `${list?.[Number(entry[2])]?.name || `#${Number(entry[2]) + 1}`} · ${fieldLabel(entry[3])}`;
  }
  const field = t.field ? ` · ${fieldLabel(t.field)}` : "";
  if (t.action) return t.action + field;
  if (t.link !== undefined) return `Link #${t.link + 1}${field}`;
  return `${wf.kind === "pipeline" ? "Pipeline" : "Workflow"}${field}`;
}

export type BottomTab = "problems" | "log";

const debugKey = "runnel.log.debug";

/** Debug lines are shown by default; the choice is remembered in the browser (decision for mockup E8). */
function useDebug(): [boolean, (on: boolean) => void] {
  const [on, setOn] = useState(() => {
    try {
      return localStorage.getItem(debugKey) !== "off";
    } catch {
      return true;
    }
  });
  return [
    on,
    (v: boolean) => {
      setOn(v);
      try {
        localStorage.setItem(debugKey, v ? "on" : "off");
      } catch {
        // not remembered
      }
    },
  ];
}

/**
 * The panel below the canvas (mockups E7, E8) with the tabs "Problems" and "Log". Both are
 * collapsed to their tab until opened. Problems: a table of message, place and line; a click
 * selects the action or link and shows the field in the properties panel. Log: the lines of
 * the current or last run, following it while it runs.
 */
export function BottomPanel(props: {
  /** null while the file cannot be read as a graph (problems are still listed, E9). */
  workflow: Workflow | null;
  pipeline: boolean;
  /** Whether the tab "Log" is shown (only files that run, E10). */
  log?: boolean;
  problems: Problem[];
  tab: BottomTab | null;
  onTab: (tab: BottomTab | null) => void;
  onOpen: (p: Problem) => void;
  run: RunView | null;
  /** A request to show the first line of an action (from the properties panel); seq changes with every request. */
  logFocus?: { actionId: string; seq: number } | null;
  onSelectAction: (id: string) => void;
}) {
  const { problems, tab } = props;
  const errors = problems.filter((p) => p.severity === "error").length;
  const tabButton = (t: BottomTab, label: string, extra?: ReactNode) => (
    <button
      type="button"
      aria-expanded={tab === t}
      onClick={() => props.onTab(tab === t ? null : t)}
      className={
        "inline-flex h-[26px] cursor-pointer items-center gap-1.5 rounded-sm border-0 px-2.5 text-xs " +
        (tab === t ? "bg-muted font-medium text-foreground" : "bg-transparent text-muted-foreground hover:text-foreground")
      }
    >
      <ChevronIcon open={tab === t} width={12} height={12} />
      {label}
      {extra}
    </button>
  );
  return (
    <section aria-label="Problems and log" className="shrink-0 border-t border-border bg-background">
      <div className="flex h-8 items-center gap-0.5 px-2">
        {tabButton(
          "problems",
          "Problems",
          problems.length > 0 && (
            <span
              data-testid="problem-count"
              className={
                "inline-grid h-4 min-w-[18px] place-items-center rounded-full px-[5px] text-[10px] font-semibold " +
                (errors > 0 ? "bg-danger-soft text-destructive" : "bg-warning-soft text-warning")
              }
            >
              {problems.length}
            </span>
          ),
        )}
        {props.log !== false && tabButton("log", "Log")}
      </div>
      {tab === "problems" && <ProblemsTable {...props} />}
      {tab === "log" && props.log !== false && <LogView run={props.run} pipeline={props.pipeline} focus={props.logFocus} onSelectAction={props.onSelectAction} />}
    </section>
  );
}

function ProblemsTable(props: { workflow: Workflow | null; problems: Problem[]; onOpen: (p: Problem) => void }) {
  const { problems } = props;
  return (
    <div role="table" aria-label="Problems" className="h-[168px] overflow-auto border-t border-border text-xs">
      <div role="row" className="grid h-6 grid-cols-[20px_1fr_180px_90px] items-center gap-2 bg-muted px-3 text-2xs text-muted-foreground">
        <span role="columnheader" />
        <span role="columnheader">Message</span>
        <span role="columnheader">Where</span>
        <span role="columnheader">Line</span>
      </div>
      {problems.length === 0 && <p className="m-0 px-3 py-2 text-muted-foreground">No problems.</p>}
      {problems.map((p, i) => (
        <div
          key={i}
          role="row"
          tabIndex={0}
          onClick={() => props.onOpen(p)}
          onKeyDown={(e) => e.key === "Enter" && props.onOpen(p)}
          className="grid min-h-[30px] cursor-pointer grid-cols-[20px_1fr_180px_90px] items-center gap-2 border-b border-border px-3 py-1 outline-none hover:bg-muted focus-visible:bg-accent focus-visible:text-accent-foreground"
        >
          <span role="cell" className={"font-bold " + (p.severity === "error" ? "text-destructive" : "text-warning")}>
            {p.severity === "error" ? "✖" : "⚠"}
          </span>
          <span role="cell">{p.message}</span>
          <span role="cell" className="truncate font-mono text-2xs text-muted-foreground">
            {props.workflow ? where(props.workflow, p) : ""}
          </span>
          <span role="cell" className="font-mono text-2xs text-muted-foreground">
            {p.line ? `line ${p.line}${p.column ? `:${p.column}` : ""}` : ""}
          </span>
        </div>
      ))}
    </div>
  );
}

const chip: Record<string, { className: string; text: string }> = {
  running: { className: "bg-status-running/15 text-status-running", text: "Running" },
  succeeded: { className: "bg-status-success/15 text-status-success", text: "✔ Succeeded" },
  failed: { className: "bg-status-failed/15 text-status-failed", text: "✖ Failed" },
  cancelled: { className: "bg-muted text-muted-foreground", text: "■ Cancelled" },
};

const levelClass: Record<string, string> = {
  debug: "text-muted-foreground",
  info: "text-foreground",
  warn: "text-warning",
  error: "text-destructive",
};

/** The log of the current or last run; it follows new lines while scrolled to the end. */
function LogView(props: {
  run: RunView | null;
  pipeline?: boolean;
  focus?: { actionId: string; seq: number } | null;
  onSelectAction: (id: string) => void;
}) {
  const { run, focus } = props;
  const [debug, setDebug] = useDebug();
  const box = useRef<HTMLDivElement>(null);
  const atEnd = useRef(true);
  const [highlight, setHighlight] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const running = run?.status === "running";
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => setNow(Date.now()), 200);
    return () => clearInterval(t);
  }, [running]);

  const lines = (run?.log ?? []).map((l, i) => ({ ...l, i })).filter((l) => debug || l.level !== "debug");
  useLayoutEffect(() => {
    if (atEnd.current && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines.length]);

  // "Show in log" from the properties panel: the first error of the action, else its first line.
  useEffect(() => {
    if (!focus || !run) return;
    const of = run.log.map((l, i) => ({ ...l, i })).filter((l) => l.actionId === focus.actionId);
    const line = of.find((l) => l.level === "error") ?? of.find((l) => debug || l.level !== "debug") ?? of[0];
    if (!line) return;
    if (line.level === "debug" && !debug) setDebug(true);
    setHighlight(line.i);
    atEnd.current = false;
    requestAnimationFrame(() => box.current?.querySelector(`[data-line="${line.i}"]`)?.scrollIntoView({ block: "nearest" }));
  }, [focus]); // only on a new request

  if (!run) {
    return (
      <p className="m-0 border-t border-border px-3 py-2 text-xs text-muted-foreground">
        No run yet. Run the {props.pipeline ? "pipeline" : "workflow"} with ▶ Run (F5).
      </p>
    );
  }
  const c = chip[run.status];
  const ms = run.durationMs ?? (running ? Math.max(0, now - run.started) : undefined);
  return (
    <>
      <div className="flex h-[30px] items-center gap-2.5 border-t border-border px-3 text-xs text-muted-foreground">
        <span data-testid="log-status" className={`inline-flex h-[22px] items-center gap-1.5 rounded-full px-2 text-2xs font-semibold ${c.className}`}>
          {running && <span className="runnel-spin block size-[9px] rounded-full border-2 border-current border-r-transparent" />}
          {c.text}
          {ms !== undefined ? ` · ${formatDuration(ms)}` : ""}
        </span>
        <span className="truncate font-mono text-2xs" title={run.id}>
          {run.id.slice(0, 12)} · {run.workflow}
          {run.environment ? ` · ${run.environment}` : ""}
        </span>
        <span className="flex-1" />
        <label className="inline-flex cursor-pointer items-center gap-1.5">
          <input type="checkbox" checked={debug} onChange={(e) => setDebug(e.target.checked)} className="size-3.5 accent-primary" />
          Debug
        </label>
      </div>
      <div
        ref={box}
        role="log"
        aria-label="Run log"
        onScroll={(e) => {
          const el = e.currentTarget;
          atEnd.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
        className="h-[168px] overflow-auto border-t border-border font-mono text-2xs"
      >
        {lines.map((l) => (
          <div
            key={l.i}
            data-line={l.i}
            data-level={l.level}
            className={
              "grid min-h-6 grid-cols-[88px_60px_120px_1fr] items-center gap-2 px-3 " +
              (l.level === "error" ? "bg-danger-soft/70 " : "") +
              (highlight === l.i ? "outline outline-1 -outline-offset-1 outline-ring" : "")
            }
          >
            <span className="text-muted-foreground">{l.time.slice(11, 23)}</span>
            <span className={`font-semibold ${levelClass[l.level]}`}>
              {l.level === "error" ? "✖ " : l.level === "warn" ? "⚠ " : ""}
              {l.level.toUpperCase()}
            </span>
            {l.actionId ? (
              <button
                type="button"
                title={`Select ${l.actionId}`}
                onClick={() => props.onSelectAction(l.actionId!)}
                className="cursor-pointer justify-self-start truncate rounded-sm border-0 bg-accent px-[5px] font-mono text-2xs text-accent-foreground"
              >
                {l.actionId}
              </button>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
            <span className="py-0.5 whitespace-pre-wrap">{l.message}</span>
          </div>
        ))}
      </div>
    </>
  );
}
