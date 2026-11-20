import { Handle, NodeToolbar, Position, type Node, type NodeProps } from "@xyflow/react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { formatDuration, type ActionRun } from "../run";
import { idPattern, type Problem } from "../workflow";
import { ActionIcon } from "./Icons";

export interface ActionNodeData extends Record<string, unknown> {
  id: string;
  type: string;
  join?: string;
  /** Workflows: the action repeats for each entry of a list (forEach), parallel at a time. */
  forEach?: boolean;
  parallel?: number;
  /** Pipelines: instances of the transform (shown as a chip and a stack of cards when more than 1). */
  copies?: number;
  /** The type takes no incoming links: core.start, transforms of the category input. */
  noInput?: boolean;
  /** Tile colour (category of the action type); unknown types have none. */
  color?: string;
  problems: Problem[];
  onShowInYAML: (line: number) => void;
  /** The properties panel shows the problems; the popover at the node is then not needed. */
  problemsInPanel?: boolean;
  /** Ports accept new links (editable canvas). */
  connectable?: boolean;
  /** Starts editing the id (double-click on it). */
  onStartRename?: () => void;
  /** Status in the current or last run (mockup E8). */
  run?: ActionRun;
  /** Set while the id is edited: ids in use and the callback with the new id (null: keep). */
  rename?: { taken: Set<string>; onDone: (id: string | null) => void };
}

export type ActionNodeType = Node<ActionNodeData, "action">;

const severityRank = (ps: Problem[]) =>
  ps.some((p) => p.severity === "error") ? "error" : ps.length > 0 ? "warning" : null;

/** Run status: bar colour, symbol (design/README.md 3.2, 6.1). */
const runLook: Record<string, { bar: string; marker: string; symbol: string; label: string }> = {
  running: { bar: "bg-status-running", marker: "bg-status-running", symbol: "", label: "Running" },
  waiting: { bar: "bg-status-waiting", marker: "bg-status-waiting", symbol: "⧗", label: "Waiting for further incoming links" },
  succeeded: { bar: "bg-status-success", marker: "bg-status-success", symbol: "✔", label: "Succeeded" },
  failed: { bar: "bg-status-failed", marker: "bg-status-failed", symbol: "✖", label: "Failed" },
  cancelled: { bar: "bg-status-skipped", marker: "bg-status-skipped", symbol: "■", label: "Cancelled" },
  skipped: { bar: "", marker: "bg-status-skipped", symbol: "–", label: "Skipped" },
};

/** Seconds since the start, updated while the action runs. */
function useElapsed(started: number | undefined, running: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => setNow(Date.now()), 200);
    return () => clearInterval(t);
  }, [running]);
  return started && running ? Math.max(0, now - started) : undefined;
}

/** An action on the canvas: a card with category tile, id and type (mockup E5), and its run status (E8). */
export function ActionNode({ data, selected }: NodeProps<ActionNodeType>) {
  const run = data.run;
  const look = run && runLook[run.status];
  const elapsed = useElapsed(run?.started, run?.status === "running");
  const duration = run?.durationMs ?? elapsed;
  // During a run, its status has precedence over problems (decision for mockup E5).
  const severity = look ? null : severityRank(data.problems);
  const known = data.color !== undefined;
  const hasInput = data.type !== "core.start" && !data.noInput;
  const copies = data.copies && data.copies > 1 ? data.copies : 0;
  const hasOutput = data.type !== "core.success" && data.type !== "core.abort";
  const border =
    severity === "error" ? "border-destructive" : severity === "warning" ? "border-warning" : "border-node-border";

  return (
    <div
      data-testid={`node-${data.id}`}
      data-severity={severity ?? undefined}
      data-run={run?.status}
      className={
        `relative flex h-[52px] w-[176px] items-center gap-2.5 rounded-lg border bg-node pr-3 pl-2.5 shadow-sm ${border}` +
        (selected ? " outline-2 outline-offset-1 outline-node-selected" : "") +
        (run && (run.status === "pending" || run.status === "skipped") ? " opacity-55" : "")
      }
      // Copies: a second card edge behind the node (mockup S25a).
      style={copies ? { boxShadow: "5px 5px 0 -1px var(--node), 5px 5px 0 0 var(--node-border), var(--rn-shadow-sm)" } : undefined}
    >
      {((data.join && data.join !== "all") || copies > 0 || data.forEach) && (
        <span className="absolute -top-[22px] -left-0.5 flex gap-1">
          {data.join && data.join !== "all" && <Chip title={`join: ${data.join}`}>join: {data.join}</Chip>}
          {data.forEach && (
            <Chip title="Runs once for each entry of a list (forEach)" testId="for-each">
              ↻ for each{data.parallel && data.parallel > 1 ? ` · ${data.parallel} at a time` : ""}
            </Chip>
          )}
          {copies > 0 && (
            <Chip title={`copies: ${copies}`} testId="copies">
              ×{copies} copies
            </Chip>
          )}
        </span>
      )}
      {hasInput && <Handle type="target" position={Position.Left} isConnectable={!!data.connectable} className="runnel-port" />}
      <span
        className="grid size-7 shrink-0 place-items-center rounded-md"
        style={known ? { background: data.color, color: "var(--rn-tiefe-0)" } : { background: "var(--rn-tiefe-300)", color: "var(--rn-tiefe-800)" }}
      >
        <ActionIcon type={data.type} known={known} />
      </span>
      <span className="min-w-0 leading-tight">
        {data.rename ? (
          <RenameField id={data.id} {...data.rename} />
        ) : (
          <span
            className="block truncate font-semibold"
            title={data.onStartRename ? "Double-click to rename" : undefined}
            onDoubleClick={data.onStartRename}
          >
            {data.id}
          </span>
        )}
        {run?.counts ? (
          // Records of the last run instead of the type (mockup S25a, state "run"): read → written, errors with ✖.
          <span
            data-testid="counts"
            title={`read ${run.counts.read}, written ${run.counts.written}, errors ${run.counts.errors ?? 0}`}
            className="block truncate font-mono text-2xs text-foreground tabular-nums"
          >
            {data.noInput ? "" : `${run.counts.read} → `}
            {run.counts.written}
            {run.counts.errors ? <span className="text-destructive"> · ✖ {run.counts.errors}</span> : null}
          </span>
        ) : (
        <span className="block truncate font-mono text-2xs text-muted-foreground">
          {data.type}
          {duration !== undefined && (
            <>
              {" · "}
              <span className="text-foreground" data-testid="duration">
                {formatDuration(duration)}
              </span>
            </>
          )}
        </span>
        )}
      </span>
      {hasOutput && <Handle type="source" position={Position.Right} isConnectable={!!data.connectable} className="runnel-port" />}
      {look && (
        <>
          <span
            className={
              "absolute right-1.5 -bottom-px left-1.5 h-[3px] rounded-t-[3px] " +
              (run.status === "skipped" ? "border-t-[3px] border-dashed border-status-skipped" : look.bar)
            }
          />
          <span
            role="img"
            aria-label={look.label}
            title={run.error ? `${look.label}: ${run.error}` : look.label}
            className={`absolute -top-[9px] -right-[9px] grid size-5 place-items-center rounded-full text-[11px] font-bold text-tiefe-0 shadow-sm ${look.marker}`}
          >
            {run.status === "running" ? (
              <span className="runnel-spin block size-2.5 rounded-full border-2 border-current border-r-transparent" />
            ) : (
              look.symbol
            )}
          </span>
        </>
      )}
      {severity && (
        <span
          aria-label={severity === "error" ? "Error" : "Warning"}
          className={
            "absolute -top-[9px] -right-[9px] grid size-5 place-items-center rounded-full text-[11px] font-bold text-tiefe-0 shadow-sm " +
            (severity === "error" ? "bg-destructive" : "bg-warnung-600")
          }
        >
          {severity === "error" ? "✖" : "⚠"}
        </span>
      )}
      <NodeToolbar isVisible={selected && data.problems.length > 0 && !data.rename && !data.problemsInPanel} position={Position.Bottom} offset={12}>
        <div
          role="dialog"
          aria-label={`Problems of ${data.id}`}
          className="w-[288px] rounded-lg border border-border bg-popover px-3.5 py-3 text-xs text-popover-foreground shadow-md"
        >
          <h4 className="m-0 mb-1.5 text-xs font-semibold">{data.id}</h4>
          {data.problems.map((p, i) => (
            <div key={i} className="mt-1.5 grid grid-cols-[16px_1fr] gap-1.5">
              <span className={p.severity === "error" ? "text-destructive" : "text-warning"}>
                {p.severity === "error" ? "✖" : "⚠"}
              </span>
              <span>
                {p.message}
                {p.line && (
                  <span className="block font-mono text-muted-foreground">
                    line {p.line}
                    {p.column ? `, column ${p.column}` : ""}
                  </span>
                )}
              </span>
            </div>
          ))}
          {data.problems[0]?.line && (
            <button
              type="button"
              onClick={() => data.onShowInYAML(data.problems[0].line!)}
              className="mt-2 ml-[22px] cursor-pointer border-0 bg-transparent p-0 text-xs text-muted-foreground hover:text-foreground"
            >
              Show in YAML ›
            </button>
          )}
        </div>
      </NodeToolbar>
    </div>
  );
}

/** A chip above a node for a setting that changes how it runs (join, copies). */
function Chip(props: { title: string; testId?: string; children: ReactNode }) {
  return (
    <span
      title={props.title}
      data-testid={props.testId}
      className="inline-flex h-[18px] items-center rounded-sm border border-border bg-card px-1.5 font-mono text-[10px] font-medium whitespace-nowrap text-muted-foreground"
    >
      {props.children}
    </span>
  );
}

/**
 * Edits the id of an action (mockup E6b): Enter renames, Esc keeps the id; leaving the
 * field renames if the new id is valid.
 */
function RenameField(props: { id: string; taken: Set<string>; onDone: (id: string | null) => void }) {
  const [value, setValue] = useState(props.id);
  const done = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  // React Flow shows a new node only once it has been measured; focus it as soon as it is visible.
  useEffect(() => {
    let frame = 0;
    let tries = 0;
    const focus = () => {
      const el = input.current;
      if (!el || done.current) return;
      el.focus();
      if (document.activeElement === el) el.select();
      else if (tries++ < 30) frame = requestAnimationFrame(focus);
    };
    focus();
    return () => cancelAnimationFrame(frame);
  }, []);
  const error = !idPattern.test(value)
    ? "Use letters, digits, - and _, starting with a letter"
    : value !== props.id && props.taken.has(value)
      ? `“${value}” is already used`
      : null;
  const finish = (id: string | null) => {
    if (done.current) return;
    done.current = true;
    props.onDone(id === props.id ? null : id);
  };
  return (
    <>
      <input
        ref={input}
        aria-label="Action id"
        aria-invalid={error !== null}
        value={value}
        onChange={(e) => setValue(e.target.value.trim())}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter" && !error) finish(value);
          else if (e.key === "Escape") finish(null);
        }}
        onBlur={() => finish(error ? null : value)}
        className={
          "nodrag nopan -mt-0.5 -ml-1 block h-[22px] w-[104px] rounded-sm border bg-background px-1 text-sm font-semibold text-foreground outline-2 " +
          (error ? "border-destructive outline-danger-soft" : "border-ring outline-ring/25")
        }
      />
      <span
        role={error ? "alert" : undefined}
        className="absolute top-[60px] left-1/2 -translate-x-1/2 rounded-sm border border-border bg-card px-1.5 py-0.5 text-2xs whitespace-nowrap text-muted-foreground shadow-sm"
      >
        {error ? <span className="text-destructive">✖ {error}</span> : <>Enter: rename · Esc: keep “{props.id}” · letters, digits, - and _</>}
      </span>
    </>
  );
}
