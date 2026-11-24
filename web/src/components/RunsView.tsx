import { useEffect, useMemo, useState } from "react";
import { formatDuration, type RunStatus } from "../run";
import type { TriggerInfo } from "../forms";
import { dayLabel, fetchRun, fetchRuns, runsPage, timeOf, triggerLabel, type RunDetail, type RunFilter, type RunInfo } from "../runs";
import {
  ChevronIcon,
  ClockIcon,
  CloseIcon,
  CopyIcon,
  GlobeIcon,
  HistoryIcon,
  KindIcon,
  PencilIcon,
  ReloadIcon,
  TerminalIcon,
} from "./Icons";

/**
 * The list of runs of the project (mockup S26d, states "list", "filter" and "empty"): runs of
 * the editor, of triggers and of runnel run from the run history, newest first and grouped by
 * day, with filters for status, file and trigger. Running runs and new ones appear without
 * reloading (tick changes on run.started and run.finished). The panel on the right shows the
 * selected run.
 */
export function RunsView(props: {
  active: boolean;
  /** Changes when a run starts or ends. */
  tick: number;
  /** Workflows and pipelines of the project for the file filter. */
  files: string[];
  triggers: TriggerInfo[];
  keepDays?: number;
  maxRuns?: number;
  onOpenFile: (path: string) => void;
  onSummary: (text: string | null) => void;
}) {
  const [filter, setFilter] = useState<RunFilter>({});
  const [runs, setRuns] = useState<RunInfo[] | null>(null);
  const [more, setMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<RunDetail | null>(null);
  const { onSummary, active, tick } = props;

  // The first page, again when the filter changes or a run starts or ends.
  useEffect(() => {
    let live = true;
    fetchRuns(filter).then(
      (list) => {
        if (!live) return;
        setRuns(list);
        setMore(list.length === runsPage);
        setError(null);
      },
      (err: unknown) => live && setError(err instanceof Error ? err.message : String(err)),
    );
    return () => {
      live = false;
    };
  }, [filter, tick]);

  const selectedRun = runs?.find((r) => r.id === selected);
  const selectedStatus = selectedRun?.status;
  useEffect(() => {
    if (!selected) return setDetail(null);
    let live = true;
    fetchRun(selected).then(
      (d) => live && setDetail(d),
      () => live && setDetail(null),
    );
    return () => {
      live = false;
    };
  }, [selected, selectedStatus]);

  useEffect(() => {
    if (!active || !runs) return;
    const running = runs.filter((r) => r.status === "running").length;
    const n = `${runs.length}${more ? "+" : ""} ${runs.length === 1 ? "run" : "runs"}`;
    onSummary(filter.path ? `${n} of ${filter.path.slice(filter.path.lastIndexOf("/") + 1)}` : running > 0 ? `${n} · ${running} running` : n);
  }, [active, runs, more, filter.path, onSummary]);

  const loadOlder = () => {
    const last = runs?.[runs.length - 1];
    if (!last) return;
    fetchRuns(filter, last.id).then(
      (list) => {
        setRuns((r) => [...(r ?? []), ...list.filter((x) => !r?.some((y) => y.id === x.id))]);
        setMore(list.length === runsPage);
      },
      (err: unknown) => setError(err instanceof Error ? err.message : String(err)),
    );
  };

  const filtered = !!(filter.status || filter.path || filter.trigger);
  const triggerOptions = useMemo(
    () => [
      ["editor", "Editor"],
      ["cli", "runnel run"],
      ...props.triggers.map((t) => [`${t.type}:${t.name}`, `${t.type}:${t.name}`]),
    ],
    [props.triggers],
  );

  return (
    <div className="grid min-h-0 flex-1 grid-cols-[1fr_320px]" data-testid="runs-view">
      <div className="flex min-h-0 min-w-0 flex-col">
        <div className="flex h-11 shrink-0 items-center gap-2 border-b border-border px-4" role="toolbar" aria-label="Filter">
          <span className="label-caps mr-0.5">Filter</span>
          <FilterSelect
            label="Status"
            value={filter.status ?? ""}
            options={[["running", "Running"], ["succeeded", "Succeeded"], ["failed", "Failed"], ["cancelled", "Cancelled"]]}
            onChange={(v) => setFilter((f) => ({ ...f, status: (v || undefined) as RunStatus | undefined }))}
          />
          <FilterSelect
            label="File"
            value={filter.path ?? ""}
            all="All files"
            options={[...new Set([...props.files, ...(filter.path ? [filter.path] : [])])].map((p) => [p, p])}
            onChange={(v) => setFilter((f) => ({ ...f, path: v || undefined }))}
          />
          <FilterSelect
            label="Trigger"
            value={filter.trigger ?? ""}
            options={triggerOptions}
            onChange={(v) => setFilter((f) => ({ ...f, trigger: v || undefined }))}
          />
          {filtered && (
            <button type="button" onClick={() => setFilter({})} className="cursor-pointer border-0 bg-transparent px-1 text-xs font-medium text-primary">
              Clear filters
            </button>
          )}
          <span className="flex-1" />
          <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" title="Running and new runs appear without reloading">
            <span className="size-[7px] rounded-full bg-success" />
            Live
          </span>
          <button
            type="button"
            title="Reload"
            aria-label="Reload"
            onClick={() => setFilter((f) => ({ ...f }))}
            className="grid size-7 cursor-pointer place-items-center rounded-md border border-input bg-background text-foreground"
          >
            <ReloadIcon width={14} height={14} />
          </button>
        </div>
        {error && (
          <div role="alert" className="border-b border-border bg-danger-soft px-4 py-2 text-xs text-destructive">
            The runs cannot be loaded: {error}
          </div>
        )}
        {runs && runs.length === 0 ? (
          <Empty filtered={filtered} keep={<Keep {...props} />} onClear={() => setFilter({})} />
        ) : (
          <div className="min-h-0 flex-1 overflow-auto text-xs" role="table" aria-label="Runs">
            <div role="row" className={rowClass + " sticky top-0 z-[1] h-7 bg-muted text-2xs font-semibold text-muted-foreground"}>
              <span role="columnheader">Status</span>
              <span role="columnheader">Started ↓</span>
              <span role="columnheader" className="text-right">
                Duration
              </span>
              <span role="columnheader">File</span>
              <span role="columnheader">Trigger</span>
              <span role="columnheader">Env.</span>
            </div>
            {(runs ?? []).map((r, i) => (
              <RunRow key={r.id} run={r} day={i === 0 || dayLabel(runs![i - 1].started) !== dayLabel(r.started)} selected={r.id === selected} onSelect={() => setSelected(r.id)} />
            ))}
            {runs && (
              <div className="flex h-10 items-center gap-3 px-4 text-muted-foreground">
                <span>{more ? `Newest ${runs.length} runs` : runs.length === 1 ? "1 run" : `${runs.length} runs`}</span>
                {more && (
                  <button type="button" onClick={loadOlder} className="h-[26px] cursor-pointer rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground">
                    Load older runs
                  </button>
                )}
                <span className="flex-1" />
                <Keep {...props} />
              </div>
            )}
          </div>
        )}
      </div>
      <section aria-label="Run" className="flex min-h-0 flex-col border-l border-border bg-background">
        <div className="flex h-9 shrink-0 items-center border-b border-border px-4">
          <span className="label-caps">Run</span>
          <span className="flex-1" />
          {selected && (
            <button type="button" title="Close" aria-label="Close the run" onClick={() => setSelected(null)} className="grid size-6 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground">
              <CloseIcon width={12} height={12} />
            </button>
          )}
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {detail && selected === detail.id ? (
            <Details run={detail} triggers={props.triggers} onOpenFile={props.onOpenFile} />
          ) : (
            <p className="m-4 text-xs text-muted-foreground">Select a run to see its details.</p>
          )}
        </div>
      </section>
    </div>
  );
}

const rowClass = "grid grid-cols-[104px_64px_52px_minmax(120px,1fr)_minmax(96px,150px)_48px] items-center gap-2.5 px-4 whitespace-nowrap";

const statusInfo: Record<RunStatus, { symbol: string; text: string; color: string; dot: string }> = {
  running: { symbol: "", text: "Running", color: "text-status-running", dot: "bg-status-running" },
  succeeded: { symbol: "✔", text: "Succeeded", color: "text-status-success", dot: "bg-status-success" },
  failed: { symbol: "✖", text: "Failed", color: "text-status-failed", dot: "bg-status-failed" },
  cancelled: { symbol: "■", text: "Cancelled", color: "text-muted-foreground", dot: "bg-status-skipped" },
};

/** The status with symbol and word (functional colours never without a symbol). */
export function RunStatusLabel({ status }: { status: RunStatus }) {
  const s = statusInfo[status];
  return (
    <span className={"inline-flex items-center gap-1.5 font-semibold " + s.color}>
      <span className={"grid size-4 shrink-0 place-items-center rounded-full text-[9px] text-white " + s.dot}>
        {status === "running" ? <span className="size-2 animate-spin rounded-full border-2 border-current border-r-transparent motion-reduce:animate-none" /> : s.symbol}
      </span>
      {s.text}
    </span>
  );
}

/** The trigger as a chip: schedule, http call, runnel run or the editor. */
function TriggerChip({ trigger }: { trigger?: string }) {
  const Icon = !trigger ? PencilIcon : trigger === "cli" ? TerminalIcon : trigger.startsWith("http:") ? GlobeIcon : ClockIcon;
  return (
    <span className={"inline-flex h-5 max-w-full items-center gap-1 truncate rounded-full bg-secondary px-2 text-[10px] font-medium text-secondary-foreground " + (trigger ? "font-mono" : "")}>
      <Icon width={11} height={11} className="shrink-0" />
      <span className="truncate">{triggerLabel(trigger)}</span>
    </span>
  );
}

function RunRow(props: { run: RunInfo; day: boolean; selected: boolean; onSelect: () => void }) {
  const r = props.run;
  const name = r.path.slice(r.path.lastIndexOf("/") + 1);
  return (
    <>
      {props.day && (
        <div role="row" className="flex h-6 items-center border-b border-border px-4 text-2xs font-semibold text-muted-foreground">
          <span role="cell">{dayLabel(r.started)}</span>
        </div>
      )}
      <div
        role="row"
        aria-selected={props.selected}
        tabIndex={0}
        onClick={props.onSelect}
        onKeyDown={(e) => e.key === "Enter" && props.onSelect()}
        className={rowClass + " h-[34px] cursor-default border-b border-border " + (props.selected ? "bg-accent text-accent-foreground" : "hover:bg-muted")}
      >
        <span role="cell">
          <RunStatusLabel status={r.status} />
        </span>
        <span role="cell" className="font-mono text-2xs">
          {timeOf(r.started)}
        </span>
        <span role="cell" className="text-right font-mono text-2xs">
          {r.status === "running" ? "…" : formatDuration(r.durationMs)}
        </span>
        <span role="cell" className="inline-flex min-w-0 items-center gap-1.5" title={r.path}>
          <KindIcon kind={r.kind} width={14} height={14} className="shrink-0 text-muted-foreground" />
          <span className="truncate">{name}</span>
        </span>
        <span role="cell" className="min-w-0">
          <TriggerChip trigger={r.trigger} />
        </span>
        <span role="cell" className="truncate font-mono text-2xs">
          {r.environment}
        </span>
      </div>
    </>
  );
}

/** How long runs are kept (settings.history in project.yaml, S26c). */
function Keep(props: { keepDays?: number; maxRuns?: number; onOpenFile: (path: string) => void }) {
  return (
    <span className="text-xs text-muted-foreground">
      Kept for {props.keepDays ?? 30} days, at most {(props.maxRuns ?? 10000).toLocaleString("en-US")} runs ·{" "}
      <button type="button" onClick={() => props.onOpenFile("project.yaml")} className="cursor-pointer border-0 bg-transparent p-0 text-xs text-primary underline">
        project.yaml
      </button>
    </span>
  );
}

function Empty(props: { filtered: boolean; keep: React.ReactNode; onClear: () => void }) {
  return (
    <div className="grid flex-1 place-items-center p-8">
      <div className="max-w-[420px] text-center text-muted-foreground">
        <span className="inline-grid size-11 place-items-center rounded-full bg-muted">
          <HistoryIcon width={22} height={22} />
        </span>
        {props.filtered ? (
          <>
            <h2 className="mt-3 mb-1 text-md font-semibold text-foreground">No runs match the filters</h2>
            <button type="button" onClick={props.onClear} className="cursor-pointer border-0 bg-transparent text-sm font-medium text-primary">
              Clear filters
            </button>
          </>
        ) : (
          <>
            <h2 className="mt-3 mb-1 text-md font-semibold text-foreground">No runs yet</h2>
            <p className="m-0 mb-2">
              Runs from the editor, from triggers and from <span className="font-mono">runnel run</span> appear here as soon as they start.
            </p>
            {props.keep}
          </>
        )}
      </div>
    </div>
  );
}

/** A select of the filter bar; a set filter is highlighted and has × to clear it. */
function FilterSelect(props: { label: string; value: string; all?: string; options: string[][]; onChange: (v: string) => void }) {
  const set = props.value !== "";
  return (
    <span
      className={
        "relative inline-flex h-7 items-center gap-1.5 rounded-md border pr-2 pl-2.5 text-xs whitespace-nowrap " +
        (set ? "border-ring bg-accent text-accent-foreground" : "border-input bg-background")
      }
    >
      <span className={set ? "opacity-80" : "text-muted-foreground"}>{props.label}</span>
      <select
        aria-label={props.label}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        className="max-w-[220px] cursor-pointer appearance-none border-0 bg-transparent p-0 pr-4 text-xs text-inherit outline-none"
      >
        <option value="">{props.all ?? "All"}</option>
        {props.options.map(([v, label]) => (
          <option key={v} value={v}>
            {label}
          </option>
        ))}
      </select>
      {set ? (
        <button type="button" aria-label={`Clear ${props.label.toLowerCase()}`} title="Clear" onClick={() => props.onChange("")} className="-ml-4 grid size-4 cursor-pointer place-items-center rounded-full border-0 bg-transparent text-inherit">
          <CloseIcon width={10} height={10} />
        </button>
      ) : (
        <ChevronIcon open width={14} height={14} className="pointer-events-none -ml-4 text-muted-foreground" />
      )}
    </span>
  );
}

/** The selected run (GET /runs/{id}): error, where it came from, its parameters, the status of each action and its log. */
function Details(props: { run: RunDetail; triggers: TriggerInfo[]; onOpenFile: (path: string) => void }) {
  const r = props.run;
  const [log, setLog] = useState(false);
  const failed = r.actions.filter((a) => a.status === "failed").length;
  const trigger = r.trigger && props.triggers.find((t) => `${t.type}:${t.name}` === r.trigger);
  const params = Object.entries(r.params ?? {});
  const fileLink = (path: string) => (
    <button type="button" onClick={() => props.onOpenFile(path)} className="cursor-pointer border-0 bg-transparent p-0 text-left font-mono text-2xs break-all text-primary underline">
      {path}
    </button>
  );
  return (
    <div data-testid="run-details">
      <div className="px-4 pt-3.5 pb-2">
        <div className="text-sm">
          <RunStatusLabel status={r.status} />
        </div>
        <h3 className="mt-1.5 mb-0.5 font-mono text-sm font-semibold break-all">{r.path.slice(r.path.lastIndexOf("/") + 1)}</h3>
        <div className="text-2xs text-muted-foreground">
          {dayLabel(r.started).split(" · ")[0]} {timeOf(r.started)}
          {r.finished && ` – ${timeOf(r.finished)}`}
          {r.status !== "running" && ` · ${formatDuration(r.durationMs)}`}
        </div>
      </div>
      <div className="flex gap-2 px-4 pb-3">
        <button
          type="button"
          title="Copy run ID"
          onClick={() => void navigator.clipboard?.writeText(r.id)}
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium"
        >
          <CopyIcon width={14} height={14} />
          Copy ID
        </button>
      </div>
      {r.error && (
        <div role="alert" className="mx-4 mb-3 rounded-md bg-danger-soft px-2.5 py-2 text-xs break-words text-destructive">
          ✖ {r.error}
        </div>
      )}
      <dl className="m-0 grid grid-cols-[92px_1fr] gap-x-2.5 gap-y-1.5 px-4 pb-3 text-xs">
        <dt className="text-muted-foreground">Run ID</dt>
        <dd className="m-0 font-mono text-2xs">{r.id}</dd>
        <dt className="text-muted-foreground">File</dt>
        <dd className="m-0">{fileLink(r.path)}</dd>
        <dt className="text-muted-foreground">Trigger</dt>
        <dd className="m-0">{trigger ? <>{fileLink(trigger.file)} <span className="text-muted-foreground">({trigger.type})</span></> : <span className={r.trigger ? "font-mono text-2xs" : ""}>{triggerLabel(r.trigger)}</span>}</dd>
        {r.client && (
          <>
            <dt className="text-muted-foreground">Caller</dt>
            <dd className="m-0 font-mono text-2xs">{r.client}</dd>
          </>
        )}
        <dt className="text-muted-foreground">Environment</dt>
        <dd className="m-0 font-mono text-2xs">{r.environment || "–"}</dd>
        {params.length > 0 && (
          <>
            <dt className="text-muted-foreground">Parameters</dt>
            <dd className="m-0 font-mono text-2xs break-all">
              {params.map(([k, v]) => (
                <div key={k}>
                  {k} = {v}
                </div>
              ))}
            </dd>
          </>
        )}
      </dl>
      <section className="border-t border-border" aria-label="Actions">
        <h4 className="m-0 flex h-[34px] items-center gap-1.5 px-4 text-xs font-semibold">
          <ChevronIcon open width={12} height={12} className="text-muted-foreground" />
          {r.kind === "pipeline" ? "Transforms" : "Actions"}
          <span className="ml-auto font-mono text-2xs font-normal text-muted-foreground">
            {r.actions.length}
            {failed > 0 && ` · ${failed} failed`}
          </span>
        </h4>
        {r.actions.length > 0 && (
          <div className="mx-4 mb-3 rounded-md border border-border text-xs">
            {r.actions.map((a) => (
              <div key={a.id} title={a.error} className="grid h-7 grid-cols-[18px_1fr_auto] items-center gap-2 border-b border-border px-2.5 last:border-b-0">
                <span className={"text-center text-[11px] font-bold " + actionColor[a.status]}>{actionSymbol[a.status]}</span>
                <span className="truncate">
                  {a.id} <span className="font-mono text-2xs text-muted-foreground">{a.type}</span>
                </span>
                <span className="font-mono text-2xs text-muted-foreground">{ended.has(a.status) ? formatDuration(a.durationMs ?? 0) : "–"}</span>
              </div>
            ))}
          </div>
        )}
      </section>
      <section className="border-t border-border" aria-label="Log">
        <h4 className="m-0 flex h-[34px] items-center gap-1.5 px-4 text-xs font-semibold">
          <button type="button" aria-expanded={log} onClick={() => setLog((l) => !l)} className="-ml-1 inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent p-0 text-xs font-semibold">
            <ChevronIcon open={log} width={12} height={12} className="text-muted-foreground" />
            Log
          </button>
          <span className="ml-auto font-mono text-2xs font-normal text-muted-foreground">
            {r.log.length} {r.log.length === 1 ? "line" : "lines"}
          </span>
        </h4>
        {log && (
          <div className="mx-4 mb-3 font-mono text-2xs" role="log" aria-label="Run log">
            {r.logTruncated && <div className="mb-1 text-muted-foreground">The oldest lines are not kept.</div>}
            {r.log.map((l, i) => (
              <div key={i} className={"py-0.5 break-words " + (l.level === "error" ? "text-destructive" : l.level === "warn" ? "text-warning" : l.level === "debug" ? "text-muted-foreground" : "")}>
                <span className="text-muted-foreground">{timeOf(l.time)}</span> {l.actionId && <b>{l.actionId}: </b>}
                {l.message}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

/** Statuses of an action that ran; the server leaves out a duration of 0. */
const ended = new Set(["succeeded", "failed", "cancelled"]);

const actionSymbol: Record<string, string> = { succeeded: "✔", failed: "✖", cancelled: "■", skipped: "–", running: "●", waiting: "⧗", pending: "·" };
const actionColor: Record<string, string> = {
  succeeded: "text-status-success",
  failed: "text-status-failed",
  running: "text-status-running",
  waiting: "text-status-waiting",
};
