import { BaseEdge, EdgeLabelRenderer, type Edge, type EdgeProps } from "@xyflow/react";
import type { LinkKind } from "../workflow";

export interface LinkEdgeData extends Record<string, unknown> {
  kind: LinkKind;
  when?: string;
  loop?: boolean;
  disabled?: boolean;
}

export type LinkEdgeType = Edge<LinkEdgeData, "link">;

const symbols: Record<LinkKind, string> = { success: "✔", failure: "✖", always: "∞", expression: "ƒ" };
const colors: Record<LinkKind, string> = {
  success: "var(--link-success)",
  failure: "var(--link-failure)",
  always: "var(--link-always)",
  expression: "var(--link-expression)",
};

/**
 * A link as a horizontal Bézier curve in the colour of its kind, with its symbol at
 * 35 % of the way from the source, so that crossing links don't hide each other's
 * symbols (decision for mockup E5). Disabled links are dashed and grey.
 */
export function LinkEdge({ id, sourceX, sourceY, targetX, targetY, data }: EdgeProps<LinkEdgeType>) {
  const kind = data?.kind ?? "success";
  const dx = Math.max(40, Math.abs(targetX - sourceX) / 2);
  const c1x = sourceX + dx;
  const c2x = targetX - dx;
  const path = `M${sourceX},${sourceY} C${c1x},${sourceY} ${c2x},${targetY} ${targetX},${targetY}`;
  const t = 0.35;
  const u = 1 - t;
  const x = u * u * u * sourceX + 3 * u * u * t * c1x + 3 * u * t * t * c2x + t * t * t * targetX;
  const y = u * u * u * sourceY + 3 * u * u * t * sourceY + 3 * u * t * t * targetY + t * t * t * targetY;
  const color = data?.disabled ? "var(--link-disabled)" : colors[kind];
  const title = [data?.when ? `when: ${data.when}` : "when: success", data?.loop && "loop", data?.disabled && "disabled"]
    .filter(Boolean)
    .join(", ");

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        style={{ stroke: color, strokeWidth: 2, strokeDasharray: data?.disabled ? "5 4" : undefined }}
      />
      <EdgeLabelRenderer>
        <span
          title={title}
          data-testid={`link-${id}`}
          data-kind={kind}
          className="nodrag nopan absolute grid h-5 min-w-5 place-items-center rounded-full border-[1.5px] border-current bg-card px-[5px] text-[11px] leading-none font-bold"
          style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)`, color }}
        >
          {symbols[kind]}
          {data?.loop ? " ↻" : ""}
        </span>
      </EdgeLabelRenderer>
    </>
  );
}
