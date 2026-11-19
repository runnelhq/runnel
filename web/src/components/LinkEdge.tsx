import { BaseEdge, EdgeLabelRenderer, type Edge, type EdgeProps } from "@xyflow/react";
import type { LinkKind } from "../workflow";
import { TrashIcon } from "./Icons";

/** Kinds that can be chosen in the link menu; expressions follow with the properties dialog (E7). */
export type SettableKind = "success" | "failure" | "always";

export interface LinkEdgeData extends Record<string, unknown> {
  kind: LinkKind;
  when?: string;
  loop?: boolean;
  disabled?: boolean;
  /** The link menu is open (mockup E6b). */
  menu?: boolean;
  onOpenMenu?: () => void;
  onKind?: (when: SettableKind) => void;
  onDelete?: () => void;
}

export type LinkEdgeType = Edge<LinkEdgeData, "link">;

const symbols: Record<LinkKind, string> = { success: "✔", failure: "✖", always: "∞", expression: "ƒ" };
const colors: Record<LinkKind, string> = {
  success: "var(--link-success)",
  failure: "var(--link-failure)",
  always: "var(--link-always)",
  expression: "var(--link-expression)",
};
const menuKinds: [SettableKind, string][] = [
  ["success", "On success"],
  ["failure", "On failure"],
  ["always", "Always"],
];

/** Horizontal Bézier curve between two ports and the point at 35 % of the way from the source. */
export function linkPath(sourceX: number, sourceY: number, targetX: number, targetY: number) {
  const dx = Math.max(40, Math.abs(targetX - sourceX) / 2);
  const c1x = sourceX + dx;
  const c2x = targetX - dx;
  const path = `M${sourceX},${sourceY} C${c1x},${sourceY} ${c2x},${targetY} ${targetX},${targetY}`;
  const t = 0.35;
  const u = 1 - t;
  const x = u * u * u * sourceX + 3 * u * u * t * c1x + 3 * u * t * t * c2x + t * t * t * targetX;
  const y = u * u * u * sourceY + 3 * u * u * t * sourceY + 3 * u * t * t * targetY + t * t * t * targetY;
  return { path, x, y };
}

/**
 * A link as a horizontal Bézier curve in the colour of its kind, with its symbol at
 * 35 % of the way from the source, so that crossing links don't hide each other's
 * symbols (decision for mockup E5). Disabled links are dashed and grey. When editable,
 * a click on the symbol opens the link menu (mockup E6b).
 */
export function LinkEdge({ id, sourceX, sourceY, targetX, targetY, data, selected }: EdgeProps<LinkEdgeType>) {
  const kind = data?.kind ?? "success";
  const { path, x, y } = linkPath(sourceX, sourceY, targetX, targetY);
  const color = data?.disabled ? "var(--link-disabled)" : colors[kind];
  const title = [data?.when ? `when: ${data.when}` : "when: success", data?.loop && "loop", data?.disabled && "disabled"]
    .filter(Boolean)
    .join(", ");

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        interactionWidth={16}
        style={{ stroke: color, strokeWidth: selected ? 3.5 : 2, strokeDasharray: data?.disabled ? "5 4" : undefined }}
      />
      <EdgeLabelRenderer>
        <span
          title={title}
          data-testid={`link-${id}`}
          data-kind={kind}
          role={data?.onOpenMenu ? "button" : undefined}
          aria-haspopup={data?.onOpenMenu ? "menu" : undefined}
          aria-expanded={data?.onOpenMenu ? !!data.menu : undefined}
          onClick={data?.onOpenMenu}
          className={
            "nodrag nopan pointer-events-auto absolute grid h-5 min-w-5 place-items-center rounded-full border-[1.5px] border-current bg-card px-[5px] text-[11px] leading-none font-bold" +
            (data?.onOpenMenu ? " cursor-pointer" : "") +
            (selected ? " outline-2 outline-offset-2 outline-node-selected" : "")
          }
          style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)`, color }}
        >
          {symbols[kind]}
          {data?.loop ? " ↻" : ""}
        </span>
        {data?.menu && (
          <div
            role="menu"
            aria-label="Link"
            className="nodrag nopan pointer-events-auto absolute z-10 w-[196px] rounded-lg border border-border bg-popover p-1 text-sm text-popover-foreground shadow-md"
            style={{ transform: `translate(-50%, 0) translate(${x}px, ${y + 16}px)` }}
          >
            {menuKinds.map(([k, label]) => (
              <button
                key={k}
                type="button"
                role="menuitemradio"
                aria-checked={kind === k}
                onClick={() => data.onKind?.(k)}
                className="flex h-[30px] w-full cursor-pointer items-center gap-2.5 rounded-sm border-0 bg-transparent px-2 text-left hover:bg-muted aria-checked:bg-accent aria-checked:text-accent-foreground"
              >
                <span
                  className="grid size-[18px] place-items-center rounded-full border-[1.5px] border-current bg-card text-[10px] font-bold"
                  style={{ color: colors[k] }}
                >
                  {symbols[k]}
                </span>
                {label}
                {kind === k && <span className="ml-auto font-mono text-[10px] text-muted-foreground">✓</span>}
              </button>
            ))}
            <hr className="mx-0 my-1 border-0 border-t border-border" />
            <button
              type="button"
              role="menuitem"
              onClick={data.onDelete}
              className="flex h-[30px] w-full cursor-pointer items-center gap-2.5 rounded-sm border-0 bg-transparent px-2 text-left text-destructive hover:bg-muted"
            >
              <TrashIcon width={14} height={14} />
              Delete link
              <span className="ml-auto font-mono text-[10px] text-muted-foreground">Del</span>
            </button>
          </div>
        )}
      </EdgeLabelRenderer>
    </>
  );
}
