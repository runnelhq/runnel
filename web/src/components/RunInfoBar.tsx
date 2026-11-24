import { useEffect, useState } from "react";
import { formatDuration, type RunView } from "../run";
import { dayLabel, fetchRuns, timeOf } from "../runs";
import { RunStatusLabel, TriggerChip } from "./RunsView";
import { CloseIcon, HistoryIcon } from "./Icons";

/** Actions of the file that were not in the run, and actions of the run no longer in the file. */
export interface RunChanges {
  notInRun: string[];
  gone: { id: string; status: string; durationMs?: number }[];
}

/**
 * The line above the canvas naming a run opened from the run history (mockup S26d, states "run"
 * and "changed"): status, start, duration, trigger and environment (parameters in the
 * tooltip); older and newer run of the same file, all its runs, and × to hide it. When the
 * actions of the file differ from those of the run, a warning names them.
 */
export function RunInfoBar(props: {
  run: RunView;
  path: string;
  changes: RunChanges | null;
  onOpenRun: (id: string) => void;
  onRunsOf: () => void;
  onHide: () => void;
}) {
  const { run, path } = props;
  const [near, setNear] = useState<{ older?: string; newer?: string }>({});
  const ended = run.status !== "running";
  useEffect(() => {
    let live = true;
    setNear({});
    void (async () => {
      try {
        const [first, older] = await Promise.all([fetchRuns({ path }), fetchRuns({ path }, run.id, 1)]);
        const i = first.findIndex((r) => r.id === run.id);
        if (live) setNear({ older: older[0]?.id, newer: i > 0 ? first[i - 1].id : undefined });
      } catch {
        // without the list the arrows stay off
      }
    })();
    return () => {
      live = false;
    };
  }, [path, run.id, ended]);

  const started = new Date(run.started).toISOString();
  const params = Object.entries(run.history?.params ?? {});
  const arrow = (label: string, id: string | undefined, d: string) => (
    <button
      type="button"
      title={`${label} of this file`}
      aria-label={label}
      disabled={!id}
      onClick={() => id && props.onOpenRun(id)}
      className="grid size-[26px] cursor-pointer place-items-center rounded-md border border-input bg-background text-foreground disabled:cursor-default disabled:opacity-40"
    >
      <svg viewBox="0 0 24 24" width={14} height={14} fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d={d} />
      </svg>
    </button>
  );
  const c = props.changes;
  return (
    <>
      <div data-testid="run-info" className="flex min-h-9 shrink-0 items-center gap-2.5 border-b border-border bg-muted py-1 pr-2 pl-4 text-xs whitespace-nowrap">
        <HistoryIcon width={14} height={14} className="shrink-0 text-muted-foreground" />
        <RunStatusLabel status={run.status} />
        <span>
          {dayLabel(started).split(" · ")[0]} {timeOf(started)}
          {run.durationMs !== undefined && ` · ${formatDuration(run.durationMs)}`}
        </span>
        <span className="text-muted-foreground">·</span>
        <TriggerChip trigger={run.history?.trigger} />
        {run.environment && (
          <>
            <span className="text-muted-foreground">·</span>
            <span className="font-mono text-2xs text-muted-foreground" title={params.length ? `Parameters: ${params.map(([k, v]) => `${k}=${v}`).join(", ")}` : undefined}>
              {run.environment}
              {params.length > 0 && ` · ${params.length} ${params.length === 1 ? "parameter" : "parameters"}`}
            </span>
          </>
        )}
        <span className="flex-1" />
        {arrow("Older run", near.older, "M15 6l-6 6 6 6")}
        {arrow("Newer run", near.newer, "M9 6l6 6-6 6")}
        <button
          type="button"
          title="All runs of this file"
          aria-label="All runs of this file"
          onClick={props.onRunsOf}
          className="grid size-[26px] cursor-pointer place-items-center rounded-md border border-input bg-background text-foreground"
        >
          <HistoryIcon width={14} height={14} />
        </button>
        <button
          type="button"
          title="Hide the run"
          aria-label="Hide the run"
          onClick={props.onHide}
          className="grid size-[26px] cursor-pointer place-items-center rounded-md border-0 bg-transparent text-muted-foreground"
        >
          <CloseIcon width={12} height={12} />
        </button>
      </div>
      {c && (c.notInRun.length > 0 || c.gone.length > 0) && (
        <div role="note" data-testid="run-changed" className="flex shrink-0 items-start gap-2.5 border-b border-border bg-warning-soft px-4 py-1.5 text-xs text-foreground">
          <span className="font-bold text-warning">⚠</span>
          <span>
            <b>The file was changed after this run.</b> The canvas shows the file as it is now
            {c.notInRun.length > 0 && (
              <>
                {"; not in the run: "}
                <span className="font-mono">{c.notInRun.join(", ")}</span>
              </>
            )}
            {c.gone.length > 0 && (
              <>
                {"; in the run, no longer in the file: "}
                <span className="font-mono">
                  {c.gone.map((g) => `${g.id} (${g.status}${g.durationMs !== undefined ? ` ${formatDuration(g.durationMs)}` : ""})`).join(", ")}
                </span>
              </>
            )}
            .
          </span>
        </div>
      )}
    </>
  );
}
