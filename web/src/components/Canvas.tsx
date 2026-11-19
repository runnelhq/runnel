import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";
import {
  Background,
  BackgroundVariant,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  ViewportPortal,
  useEdgesState,
  useNodesState,
  useReactFlow,
  useViewport,
  type Connection,
  type ConnectionLineComponentProps,
  type EdgeTypes,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/base.css";
import { categoryColor, paletteDrag, paletteMime, useCatalog } from "../catalog";
import {
  closesCycle,
  linkKind,
  newActionId,
  nodeHeight,
  nodeWidth,
  place,
  problemsByAction,
  type Link,
  type Problem,
  type Workflow,
} from "../workflow";
import { ActionNode, type ActionNodeType } from "./ActionNode";
import { ActionIcon, FitIcon, LayoutIcon, MinusIcon, PlusIcon } from "./Icons";
import { LinkEdge, linkPath, type LinkEdgeType, type SettableKind } from "./LinkEdge";

const nodeTypes: NodeTypes = { action: ActionNode };
const edgeTypes: EdgeTypes = { link: LinkEdge };

/** Grid the nodes snap to when moved (decision for mockup E6a). */
export const snapGrid: [number, number] = [8, 8];
// Stable key lists: React Flow re-registers its key handlers whenever these change.
const deleteKeys = ["Delete", "Backspace"];
const multiSelectionKeys = ["Control", "Meta"];
const snap = (v: number) => Math.round(v / snapGrid[0]) * snapGrid[0];

type Positions = Record<string, { x: number; y: number }>;

export interface CanvasProps {
  workflow: Workflow;
  problems: Problem[];
  onShowInYAML: (line: number) => void;
  /** Called with the new positions after nodes were moved (all positions if the file has no complete layout). */
  onMove?: (positions: Positions) => void;
  /** Called with the ids of the actions and the indexes of the links to delete (Delete key, link menu). */
  onDelete?: (ids: string[], links: number[]) => void;
  /** Called when an action was dropped from the palette, with its position (and all others if the layout is incomplete). */
  onAdd?: (action: { id: string; type: string }, positions: Positions) => void;
  /** Called when an action id was changed. */
  onRename?: (from: string, to: string) => void;
  /** Called with a new link drawn from an output to an input. */
  onConnect?: (link: Link) => void;
  /** Called when the kind of a link was chosen in the link menu. */
  onLinkKind?: (index: number, when: SettableKind) => void;
  /** Called with the ids of the selected actions. */
  onSelect?: (ids: string[]) => void;
  /** Selects these actions, e.g. after renaming one in the properties panel. */
  selection?: string[];
  /** The properties panel is shown and lists the problems of the selected action. */
  problemsInPanel?: boolean;
  /** Id of the action whose id is being edited. */
  renaming?: string | null;
  onRenaming?: (id: string | null) => void;
}

/**
 * Canvas of a workflow (mockups E5, E6a, E6b). When editable, nodes can be moved
 * (snapping to 8 px), selected (Ctrl+click, Shift+drag for a selection box), deleted,
 * renamed (double-click on the id) and connected (drag from an output to an input);
 * actions are added by dropping them from the palette, and a click on a link opens
 * its menu (kind, delete).
 */
export function Canvas(props: CanvasProps) {
  return (
    <ReactFlowProvider>
      <Graph {...props} />
    </ReactFlowProvider>
  );
}

function Graph(props: CanvasProps) {
  const { workflow, problems, onShowInYAML, onMove, onDelete, onAdd, onRename, onConnect, onLinkKind, renaming, onRenaming, onSelect, selection, problemsInPanel } =
    props;
  const catalog = useCatalog();
  const editable = onMove !== undefined;
  const { screenToFlowPosition } = useReactFlow();
  const [menu, setMenu] = useState<string | null>(null);
  const [ghost, setGhost] = useState<{ type: string; x: number; y: number } | null>(null);
  // Starts with the selection of the file view, e.g. when switching back from the YAML view.
  const selectedRef = useRef(new Set<string>(props.selection ?? []));

  const finishRename = useCallback(
    (from: string, to: string | null) => {
      onRenaming?.(null);
      if (to && to !== from) {
        selectedRef.current = new Set([to]);
        onRename?.(from, to);
      }
    },
    [onRenaming, onRename],
  );

  const { computed, computedEdges, auto } = useMemo(() => {
    const { positions, auto } = place(workflow);
    const byAction = problemsByAction(workflow, problems);
    const ids = new Set((workflow.actions ?? []).map((a) => a.id));
    // A file with duplicate ids is shown with the first of them only (the problem is reported).
    const unique = (workflow.actions ?? []).filter((a, i, all) => a.id && all.findIndex((b) => b.id === a.id) === i);
    const computed: ActionNodeType[] = unique.map((a) => {
      const d = catalog.get(a.type);
      return {
        id: a.id,
        type: "action",
        position: positions.get(a.id) ?? { x: 0, y: 0 },
        data: {
          id: a.id,
          type: a.type,
          join: a.join,
          color: d ? (categoryColor[d.category] ?? categoryColor.general) : catalog.size === 0 ? categoryColor.general : undefined,
          problems: byAction.get(a.id) ?? [],
          onShowInYAML,
          problemsInPanel,
          connectable: editable,
          onStartRename: editable ? () => onRenaming?.(a.id) : undefined,
          rename: renaming === a.id ? { taken: ids, onDone: (to: string | null) => finishRename(a.id, to) } : undefined,
        },
      };
    });
    const computedEdges: LinkEdgeType[] = (workflow.links ?? [])
      .map((l, i) => ({ l, i }))
      .filter(({ l }) => ids.has(l.from) && ids.has(l.to))
      .map(({ l, i }) => ({
        id: `${i}`,
        source: l.from,
        target: l.to,
        type: "link",
        selectable: editable,
        data: { kind: linkKind(l.when), when: l.when, loop: l.loop, disabled: l.disabled },
      }));
    return { computed, computedEdges, auto };
  }, [workflow, problems, catalog, onShowInYAML, problemsInPanel, editable, renaming, onRenaming, finishRename]);

  // Local node and edge state for dragging and selection; it follows the workflow and keeps the selection.
  // The nodes start with the current selection, so that React Flow does not report an empty one first.
  const [nodes, setNodes, onNodesChange] = useNodesState<ActionNodeType>(
    computed.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })),
  );
  const [edges, setEdges, onEdgesChange] = useEdgesState<LinkEdgeType>(computedEdges);
  useEffect(() => {
    if (renaming) selectedRef.current = new Set([renaming]);
    setNodes(computed.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })));
  }, [computed, renaming, setNodes]);
  useEffect(() => {
    if (!selection) return;
    const same = selection.length === selectedRef.current.size && selection.every((id) => selectedRef.current.has(id));
    // A renamed action is selected once the workflow with its new id has arrived.
    if (same || !selection.every((id) => computed.some((n) => n.id === id))) return;
    selectedRef.current = new Set(selection);
    setNodes((ns) => ns.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })));
  }, [selection, computed, setNodes]);
  useEffect(() => {
    setEdges(computedEdges);
    setMenu(null);
  }, [computedEdges, setEdges]);

  const onSelectionChange = useCallback(
    ({ nodes }: { nodes: { id: string }[] }) => {
      const ids = nodes.map((n) => n.id);
      const same = ids.length === selectedRef.current.size && ids.every((id) => selectedRef.current.has(id));
      selectedRef.current = new Set(ids);
      if (!same) onSelect?.(ids);
    },
    [onSelect],
  );

  /** Positions to write with a change: only the given ones, or all if the file has no complete layout. */
  const withLayout = useCallback(
    (changed: Positions): Positions => {
      if (auto === "none") return changed;
      const all: Positions = {};
      for (const n of nodes) all[n.id] = { x: Math.round(n.position.x), y: Math.round(n.position.y) };
      return { ...all, ...changed };
    },
    [auto, nodes],
  );

  const onNodeDragStop = useCallback(
    (_: unknown, __: unknown, dragged: ActionNodeType[]) => {
      if (!onMove) return;
      const moved: Positions = {};
      for (const n of dragged) moved[n.id] = { x: Math.round(n.position.x), y: Math.round(n.position.y) };
      const changed = Object.entries(moved).some(([id, p]) => {
        const before = computed.find((c) => c.id === id)?.position;
        return auto !== "none" || !before || before.x !== p.x || before.y !== p.y;
      });
      if (changed) onMove(withLayout(moved));
    },
    [onMove, auto, computed, withLayout],
  );

  // Dropping an action from the palette; a dashed card shows where it will be placed.
  const dropPosition = (e: DragEvent) => {
    const p = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    return { x: snap(p.x - nodeWidth / 2), y: snap(p.y - nodeHeight / 2) };
  };
  const onDragOver = (e: DragEvent) => {
    if (!onAdd || !e.dataTransfer.types.includes(paletteMime)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
    const type = paletteDrag.type;
    if (type) setGhost({ type, ...dropPosition(e) });
  };
  const onDrop = (e: DragEvent) => {
    const type = e.dataTransfer.getData(paletteMime);
    setGhost(null);
    if (!onAdd || !type) return;
    e.preventDefault();
    const id = newActionId(workflow, type);
    onAdd({ id, type }, withLayout({ [id]: dropPosition(e) }));
  };

  const isValidConnection = useCallback(
    (c: Connection | LinkEdgeType) =>
      c.source !== c.target &&
      !(workflow.links ?? []).some((l) => l.from === c.source && l.to === c.target && linkKind(l.when) === "success"),
    [workflow],
  );
  const connect = useCallback(
    (c: Connection) => {
      if (!onConnect || !c.source || !c.target) return;
      // A link back to an earlier action is a loop link (decision for mockup E6b).
      onConnect({ from: c.source, to: c.target, ...(closesCycle(workflow, c.source, c.target) ? { loop: true } : {}) });
    },
    [onConnect, workflow],
  );

  const openMenu = useCallback(
    (id: string) => {
      setEdges((es) => es.map((e) => ({ ...e, selected: e.id === id })));
      setMenu(id);
    },
    [setEdges],
  );
  useEffect(() => {
    if (!menu) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setMenu(null);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menu]);

  const shownEdges = useMemo(
    () =>
      edges.map((e) => ({
        ...e,
        data: {
          ...e.data!,
          menu: editable && menu === e.id,
          onOpenMenu: editable ? () => openMenu(e.id) : undefined,
          onKind: (when: SettableKind) => {
            setMenu(null);
            if (when !== e.data?.kind) onLinkKind?.(Number(e.id), when);
          },
          onDelete: () => {
            setMenu(null);
            onDelete?.([], [Number(e.id)]);
          },
        },
      })),
    [edges, editable, menu, openMenu, onLinkKind, onDelete],
  );

  return (
    <div
      className="runnel-canvas relative min-h-0 flex-1"
      data-testid="canvas"
      onDragOver={onDragOver}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setGhost(null);
      }}
      onDrop={onDrop}
    >
      <ReactFlow
        nodes={nodes}
        edges={shownEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onSelectionChange={onSelectionChange}
        onNodeDragStop={onNodeDragStop}
        onDelete={({ nodes: ns, edges: es }) => {
          const ids = new Set(ns.map((n) => n.id));
          // Links of deleted actions are removed with them.
          const links = es.filter((e) => !ids.has(e.source) && !ids.has(e.target)).map((e) => Number(e.id));
          if (ids.size > 0 || links.length > 0) onDelete?.([...ids], links);
        }}
        onConnect={connect}
        isValidConnection={isValidConnection}
        connectionLineComponent={ConnectionLine}
        onEdgeClick={(_, e) => editable && openMenu(e.id)}
        onPaneClick={() => setMenu(null)}
        onNodeClick={() => setMenu(null)}
        nodesDraggable={editable}
        nodesConnectable={editable}
        elementsSelectable
        snapToGrid
        snapGrid={snapGrid}
        deleteKeyCode={editable ? deleteKeys : null}
        multiSelectionKeyCode={multiSelectionKeys}
        selectionKeyCode="Shift"
        zoomOnDoubleClick={false}
        fitView
        fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
        minZoom={0.2}
        maxZoom={2}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={2} />
        <Controls />
        {ghost && (
          <ViewportPortal>
            <Ghost {...ghost} color={categoryColor[catalog.get(ghost.type)?.category ?? "general"]} id={newActionId(workflow, ghost.type)} />
          </ViewportPortal>
        )}
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

/** Preview of an action dropped from the palette: a dashed card on the 8-px grid. */
function Ghost(props: { type: string; id: string; x: number; y: number; color?: string }) {
  return (
    <div
      data-testid="drop-preview"
      className="pointer-events-none absolute flex h-[52px] w-[176px] items-center gap-2.5 rounded-lg border border-dashed border-node-selected bg-node pr-3 pl-2.5 opacity-70 shadow-md"
      style={{ transform: `translate(${props.x}px, ${props.y}px)` }}
    >
      <span className="grid size-7 shrink-0 place-items-center rounded-md" style={{ background: props.color, color: "var(--rn-tiefe-0)" }}>
        <ActionIcon type={props.type} known />
      </span>
      <span className="min-w-0 leading-tight">
        <span className="block truncate font-semibold">{props.id}</span>
        <span className="block truncate font-mono text-2xs text-muted-foreground">{props.type}</span>
      </span>
    </div>
  );
}

/** Dashed preview of a new link while it is drawn; new links are "On success". */
function ConnectionLine({ fromX, fromY, toX, toY }: ConnectionLineComponentProps) {
  return (
    <path
      d={linkPath(fromX, fromY, toX, toY).path}
      fill="none"
      stroke="var(--link-success)"
      strokeWidth={2}
      strokeDasharray="6 4"
    />
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
