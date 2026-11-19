import { Handle, NodeToolbar, Position, type Node, type NodeProps } from "@xyflow/react";
import type { Problem } from "../workflow";
import { ActionIcon } from "./Icons";

export interface ActionNodeData extends Record<string, unknown> {
  id: string;
  type: string;
  join?: string;
  /** Tile colour (category of the action type); unknown types have none. */
  color?: string;
  problems: Problem[];
  onShowInYAML: (line: number) => void;
}

export type ActionNodeType = Node<ActionNodeData, "action">;

const severityRank = (ps: Problem[]) =>
  ps.some((p) => p.severity === "error") ? "error" : ps.length > 0 ? "warning" : null;

/** An action on the canvas: a card with category tile, id and type (mockup E5). */
export function ActionNode({ data, selected }: NodeProps<ActionNodeType>) {
  const severity = severityRank(data.problems);
  const known = data.color !== undefined;
  const hasInput = data.type !== "core.start";
  const hasOutput = data.type !== "core.success" && data.type !== "core.abort";
  const border =
    severity === "error" ? "border-destructive" : severity === "warning" ? "border-warning" : "border-node-border";

  return (
    <div
      data-testid={`node-${data.id}`}
      data-severity={severity ?? undefined}
      className={
        `relative flex h-[52px] w-[176px] items-center gap-2.5 rounded-lg border bg-node pr-3 pl-2.5 shadow-sm ${border}` +
        (selected ? " outline-2 outline-offset-1 outline-node-selected" : "")
      }
    >
      {data.join && data.join !== "all" && (
        <span
          title={`join: ${data.join}`}
          className="absolute -top-[22px] -left-0.5 inline-flex h-[18px] items-center rounded-sm border border-border bg-card px-1.5 font-mono text-[10px] font-medium text-muted-foreground"
        >
          join: {data.join}
        </span>
      )}
      {hasInput && <Handle type="target" position={Position.Left} isConnectable={false} className="runnel-port" />}
      <span
        className="grid size-7 shrink-0 place-items-center rounded-md"
        style={known ? { background: data.color, color: "var(--rn-tiefe-0)" } : { background: "var(--rn-tiefe-300)", color: "var(--rn-tiefe-800)" }}
      >
        <ActionIcon type={data.type} known={known} />
      </span>
      <span className="min-w-0 leading-tight">
        <span className="block truncate font-semibold">{data.id}</span>
        <span className="block truncate font-mono text-2xs text-muted-foreground">{data.type}</span>
      </span>
      {hasOutput && <Handle type="source" position={Position.Right} isConnectable={false} className="runnel-port" />}
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
      <NodeToolbar isVisible={selected && data.problems.length > 0} position={Position.Bottom} offset={12}>
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
