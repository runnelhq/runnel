import { useCallback, useEffect, useMemo, useRef } from "react";
import {
  Background,
  BackgroundVariant,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useReactFlow,
  useViewport,
  type EdgeTypes,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/base.css";
import { categoryColor, useCatalog } from "../catalog";
import { linkKind, place, problemsByAction, type Problem, type Workflow } from "../workflow";
import { ActionNode, type ActionNodeType } from "./ActionNode";
import { FitIcon, LayoutIcon, MinusIcon, PlusIcon } from "./Icons";
import { LinkEdge, type LinkEdgeType } from "./LinkEdge";

const nodeTypes: NodeTypes = { action: ActionNode };
const edgeTypes: EdgeTypes = { link: LinkEdge };

/** Grid the nodes snap to when moved (decision for mockup E6a). */
export const snapGrid: [number, number] = [8, 8];

export interface CanvasProps {
  workflow: Workflow;
  problems: Problem[];
  onShowInYAML: (line: number) => void;
  /** Called with the new positions after nodes were moved (all positions if the file has no complete layout). */
  onMove?: (positions: Record<string, { x: number; y: number }>) => void;
  /** Called with the ids of the actions to delete (Delete key). */
  onDelete?: (ids: string[]) => void;
}

/**
 * Canvas of a workflow (mockups E5 and E6a). With onMove/onDelete, nodes can be moved
 * (snapping to 8 px), selected (Ctrl+click, Shift+drag for a selection box) and deleted.
 */
export function Canvas(props: CanvasProps) {
  return (
    <ReactFlowProvider>
      <Graph {...props} />
    </ReactFlowProvider>
  );
}

function Graph({ workflow, problems, onShowInYAML, onMove, onDelete }: CanvasProps) {
  const catalog = useCatalog();
  const editable = onMove !== undefined;

  const { computed, edges, auto } = useMemo(() => {
    const { positions, auto } = place(workflow);
    const byAction = problemsByAction(workflow, problems);
    const computed: ActionNodeType[] = (workflow.actions ?? []).map((a) => {
      const d = catalog.get(a.type);
      return {
        id: a.id,
        type: "action",
        position: positions.get(a.id) ?? { x: 0, y: 0 },
        connectable: false,
        data: {
          id: a.id,
          type: a.type,
          join: a.join,
          color: d ? (categoryColor[d.category] ?? categoryColor.general) : catalog.size === 0 ? categoryColor.general : undefined,
          problems: byAction.get(a.id) ?? [],
          onShowInYAML,
        },
      };
    });
    const ids = new Set(computed.map((n) => n.id));
    const edges: LinkEdgeType[] = (workflow.links ?? [])
      .filter((l) => ids.has(l.from) && ids.has(l.to))
      .map((l, i) => ({
        id: `${i}`,
        source: l.from,
        target: l.to,
        type: "link",
        selectable: false,
        data: { kind: linkKind(l.when), when: l.when, loop: l.loop, disabled: l.disabled },
      }));
    return { computed, edges, auto };
  }, [workflow, problems, catalog, onShowInYAML]);

  // Local node state for dragging and selection; it follows the workflow and keeps the selection.
  const [nodes, setNodes, onNodesChange] = useNodesState<ActionNodeType>(computed);
  const selectedRef = useRef(new Set<string>());
  useEffect(() => {
    setNodes(computed.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })));
  }, [computed, setNodes]);

  const onSelectionChange = useCallback(({ nodes }: { nodes: { id: string }[] }) => {
    selectedRef.current = new Set(nodes.map((n) => n.id));
  }, []);

  const onNodeDragStop = useCallback(
    (_: unknown, __: unknown, dragged: ActionNodeType[]) => {
      if (!onMove) return;
      const moved: Record<string, { x: number; y: number }> = {};
      // Without a complete layout, the first move writes the positions of all actions.
      const source = auto === "none" ? dragged : nodes.map((n) => dragged.find((d) => d.id === n.id) ?? n);
      for (const n of source) moved[n.id] = { x: Math.round(n.position.x), y: Math.round(n.position.y) };
      const changed = Object.entries(moved).some(([id, p]) => {
        const before = computed.find((c) => c.id === id)?.position;
        return auto !== "none" || !before || before.x !== p.x || before.y !== p.y;
      });
      if (changed) onMove(moved);
    },
    [onMove, auto, nodes, computed],
  );

  return (
    <div className="runnel-canvas relative min-h-0 flex-1" data-testid="canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onSelectionChange={onSelectionChange}
        onNodeDragStop={onNodeDragStop}
        onNodesDelete={(deleted) => onDelete?.(deleted.map((n) => n.id))}
        nodesDraggable={editable}
        nodesConnectable={false}
        elementsSelectable
        snapToGrid
        snapGrid={snapGrid}
        deleteKeyCode={editable ? ["Delete", "Backspace"] : null}
        multiSelectionKeyCode={["Control", "Meta"]}
        selectionKeyCode="Shift"
        fitView
        fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
        minZoom={0.2}
        maxZoom={2}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={2} />
        <Controls />
        {auto !== "none" && (
          <Panel position="top-right">
            <span
              data-testid="auto-layout"
              className="inline-flex h-[26px] items-center gap-1.5 rounded-full border border-border bg-card px-2.5 text-xs text-muted-foreground shadow-sm"
            >
              <LayoutIcon width={14} height={14} />
              {auto === "all"
                ? "Arranged automatically – the file has no layout yet"
                : "Some actions have no position and were arranged automatically"}
            </span>
          </Panel>
        )}
      </ReactFlow>
    </div>
  );
}

/** Zoom buttons and zoom level, bottom left. */
function Controls() {
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const { zoom } = useViewport();
  const button =
    "grid size-[30px] cursor-pointer place-items-center border-0 border-b border-border bg-transparent text-muted-foreground last:border-b-0 hover:bg-muted hover:text-foreground";
  return (
    <Panel position="bottom-left" className="flex items-end gap-2">
      <div className="flex flex-col overflow-hidden rounded-md border border-border bg-card shadow-sm">
        <button type="button" title="Zoom in" aria-label="Zoom in" className={button} onClick={() => zoomIn()}>
          <PlusIcon width={14} height={14} />
        </button>
        <button type="button" title="Zoom out" aria-label="Zoom out" className={button} onClick={() => zoomOut()}>
          <MinusIcon width={14} height={14} />
        </button>
        <button
          type="button"
          title="Fit to view"
          aria-label="Fit to view"
          className={button}
          onClick={() => fitView({ padding: 0.15, maxZoom: 1 })}
        >
          <FitIcon width={14} height={14} />
        </button>
      </div>
      <span
        data-testid="zoom"
        className="inline-flex h-[22px] items-center rounded-sm border border-border bg-card px-2 font-mono text-[11px] text-muted-foreground"
      >
        {Math.round(zoom * 100)} %
      </span>
    </Panel>
  );
}
