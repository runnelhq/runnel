import { useMemo, useState } from "react";
import {
  Background,
  BackgroundVariant,
  Panel,
  ReactFlow,
  ReactFlowProvider,
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

/** Read-only canvas of a workflow (mockup design/mockups/e5-canvas.html). Editing follows with E6. */
export function Canvas(props: { workflow: Workflow; problems: Problem[]; onShowInYAML: (line: number) => void }) {
  return (
    <ReactFlowProvider>
      <Graph {...props} />
    </ReactFlowProvider>
  );
}

function Graph({ workflow, problems, onShowInYAML }: { workflow: Workflow; problems: Problem[]; onShowInYAML: (line: number) => void }) {
  const catalog = useCatalog();
  // Selection is kept here (controlled flow): clicking a node selects it, clicking the canvas clears it.
  const [selected, setSelected] = useState<string | null>(null);
  const { nodes, edges, auto } = useMemo(() => {
    const { positions, auto } = place(workflow);
    const byAction = problemsByAction(workflow, problems);
    const nodes: ActionNodeType[] = (workflow.actions ?? []).map((a) => {
      const d = catalog.get(a.type);
      return {
        id: a.id,
        type: "action",
        position: positions.get(a.id) ?? { x: 0, y: 0 },
        draggable: false,
        connectable: false,
        selected: a.id === selected,
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
    const ids = new Set(nodes.map((n) => n.id));
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
    return { nodes, edges, auto };
  }, [workflow, problems, catalog, onShowInYAML, selected]);

  return (
    <div className="runnel-canvas relative min-h-0 flex-1" data-testid="canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        fitView
        fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
        minZoom={0.2}
        maxZoom={2}
        deleteKeyCode={null}
        onNodeClick={(_, node) => setSelected(node.id)}
        onPaneClick={() => setSelected(null)}
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
