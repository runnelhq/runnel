import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type MouseEvent as ReactMouseEvent, type ReactNode, type RefObject } from "react";
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
import { categoryColor, paletteDrag, paletteMime, tileColor, useCatalog } from "../catalog";
import {
  closesCycle,
  kindOfLink,
  newActionId,
  nodeHeight,
  nodeWidth,
  place,
  problemsByAction,
  type Link,
  type Problem,
  type Workflow,
} from "../workflow";
import type { RunView } from "../run";
import { ActionNode, type ActionNodeType } from "./ActionNode";
import { ActionIcon, CopyIcon, CutIcon, EyeIcon, FitIcon, LayoutIcon, MinusIcon, PasteIcon, PlusIcon, TrashIcon } from "./Icons";
import { LinkEdge, linkPath, pipelineKinds, type LinkEdgeType, type SettableKind } from "./LinkEdge";

const nodeTypes: NodeTypes = { action: ActionNode };
const edgeTypes: EdgeTypes = { link: LinkEdge };

/** Grid the nodes snap to when moved (decision for mockup E6a). */
export const snapGrid: [number, number] = [8, 8];
// Stable key lists: React Flow re-registers its key handlers whenever these change.
const deleteKeys = ["Delete", "Backspace"];
const multiSelectionKeys = ["Control", "Meta"];
const snap = (v: number) => Math.round(v / snapGrid[0]) * snapGrid[0];

type Positions = Record<string, { x: number; y: number }>;

/** Selected actions (ids) and links (index in the list of links). */
export interface Selection {
  actions: string[];
  links: number[];
}

const sameSet = (a: Set<string>, b: string[]) => a.size === b.length && b.every((x) => a.has(x));

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
  /** Called with the selected actions and links. */
  onSelect?: (selection: Selection) => void;
  /** Selects these actions and links, e.g. after renaming in the properties panel or from the problems. */
  selection?: Selection;
  /** The properties panel is shown and lists the problems of the selected action. */
  problemsInPanel?: boolean;
  /** The current or last run, shown as status at the actions and links (mockup E8). */
  run?: RunView | null;
  /** The run shown is a data preview (S44): record counts are drawn dashed, and a hint says up to which transform it ran. */
  preview?: { upTo: string; limit: number; onClose: () => void } | null;
  /** Copy or cut the given actions (context menu, E16). */
  onClipboard?: (op: "copy" | "cut", ids: string[]) => void;
  /** Paste at the given position (context menu); null without a position. */
  onPaste?: (at: { x: number; y: number } | null) => void;
  /** Something was copied, so the context menu offers Paste. */
  canPaste?: boolean;
  /** Is kept at the position of the mouse on the canvas (null outside), for pasting with Ctrl+V. */
  pointer?: RefObject<{ x: number; y: number } | null>;
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
  const { workflow, problems, onShowInYAML, onMove, onDelete, onAdd, onRename, onConnect, onLinkKind, renaming, onRenaming, onSelect, selection, problemsInPanel, run, preview } =
    props;
  const { onClipboard, onPaste, canPaste, pointer } = props;
  const pipeline = workflow.kind === "pipeline";
  const catalog = useCatalog(workflow.kind);
  const editable = onMove !== undefined;
  const { screenToFlowPosition } = useReactFlow();
  const [menu, setMenu] = useState<string | null>(null);
  // Context menu of the selection or the free canvas (E16): position in the container and on the canvas.
  const [context, setContext] = useState<{ x: number; y: number; at: { x: number; y: number }; ids: string[] } | null>(null);
  const container = useRef<HTMLDivElement>(null);
  const [ghost, setGhost] = useState<{ type: string; x: number; y: number } | null>(null);
  // Starts with the selection of the file view, e.g. when switching back from the YAML view.
  const selectedRef = useRef(new Set<string>(props.selection?.actions ?? []));
  const selectedLinksRef = useRef(new Set<string>((props.selection?.links ?? []).map(String)));

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
          copies: a.copies,
          forEach: !!a.forEach,
          parallel: a.parallel,
          noInput: pipeline && d?.category === "input",
          color: d ? tileColor(d) : catalog.size === 0 ? categoryColor.general : undefined,
          problems: byAction.get(a.id) ?? [],
          onShowInYAML,
          problemsInPanel,
          run: run?.actions[a.id],
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
        data: {
          kind: kindOfLink(l),
          when: l.when,
          loop: l.loop,
          disabled: l.disabled,
          run: pipeline ? undefined : linkRun(run, l),
          rows: pipeline ? run?.linkRows[i] : undefined,
          preview: !!preview,
        },
      }));
    return { computed, computedEdges, auto };
  }, [workflow, pipeline, problems, catalog, onShowInYAML, problemsInPanel, run, preview, editable, renaming, onRenaming, finishRename]);

  // Local node and edge state for dragging and selection; it follows the workflow and keeps the selection.
  // The nodes start with the current selection, so that React Flow does not report an empty one first.
  const [nodes, setNodes, onNodesChange] = useNodesState<ActionNodeType>(
    computed.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })),
  );
  const [edges, setEdges, onEdgesChange] = useEdgesState<LinkEdgeType>(
    computedEdges.map((e) => ({ ...e, selected: selectedLinksRef.current.has(e.id) })),
  );
  useEffect(() => {
    if (renaming) selectedRef.current = new Set([renaming]);
    setNodes(computed.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })));
  }, [computed, renaming, setNodes]);
  useEffect(() => {
    if (!selection) return;
    const links = selection.links.map(String);
    if (!sameSet(selectedLinksRef.current, links) && links.every((id) => computedEdges.some((e) => e.id === id))) {
      selectedLinksRef.current = new Set(links);
      setEdges((es) => es.map((e) => ({ ...e, selected: selectedLinksRef.current.has(e.id) })));
    }
    // A renamed action is selected once the workflow with its new id has arrived.
    if (sameSet(selectedRef.current, selection.actions) || !selection.actions.every((id) => computed.some((n) => n.id === id))) return;
    selectedRef.current = new Set(selection.actions);
    setNodes((ns) => ns.map((n) => ({ ...n, selected: selectedRef.current.has(n.id) })));
  }, [selection, computed, computedEdges, setNodes, setEdges]);
  // Links are identified by their index; a selected link stays selected while it connects the same actions.
  const prevEdges = useRef(computedEdges);
  useEffect(() => {
    const before = prevEdges.current;
    prevEdges.current = computedEdges;
    for (const id of [...selectedLinksRef.current]) {
      const a = before.find((e) => e.id === id);
      const b = computedEdges.find((e) => e.id === id);
      if (!a || !b || a.source !== b.source || a.target !== b.target) selectedLinksRef.current.delete(id);
    }
    setEdges(computedEdges.map((e) => ({ ...e, selected: selectedLinksRef.current.has(e.id) })));
    setMenu(null);
  }, [computedEdges, setEdges]);

  // React Flow reports an empty selection once when it starts, before it shows the nodes
  // selected above; that report would drop the selection kept while the YAML view was shown.
  const started = useRef(false);
  const onSelectionChange = useCallback(
    ({ nodes, edges }: { nodes: { id: string }[]; edges: { id: string }[] }) => {
      const ids = nodes.map((n) => n.id);
      const links = edges.map((e) => e.id);
      if (!started.current) {
        started.current = true;
        if (ids.length === 0 && links.length === 0) return;
      }
      const same = sameSet(selectedRef.current, ids) && sameSet(selectedLinksRef.current, links);
      selectedRef.current = new Set(ids);
      selectedLinksRef.current = new Set(links);
      if (!same) onSelect?.({ actions: ids, links: links.map(Number) });
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

  const openContext = (e: ReactMouseEvent | MouseEvent, ids: string[]) => {
    e.preventDefault();
    if (!editable || !onClipboard) return;
    const box = container.current?.getBoundingClientRect();
    const p = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    setMenu(null);
    setContext({ x: e.clientX - (box?.left ?? 0), y: e.clientY - (box?.top ?? 0), at: { x: snap(p.x), y: snap(p.y) }, ids });
  };
  // A right click on an action that is not selected selects it alone.
  const onNodeContextMenu = (e: ReactMouseEvent, node: { id: string }) => {
    let ids = [...selectedRef.current];
    if (!selectedRef.current.has(node.id)) {
      ids = [node.id];
      selectedRef.current = new Set(ids);
      selectedLinksRef.current = new Set();
      setNodes((ns) => ns.map((n) => ({ ...n, selected: n.id === node.id })));
      setEdges((es) => es.map((x) => ({ ...x, selected: false })));
      onSelect?.({ actions: ids, links: [] });
    }
    openContext(e, ids);
  };
  useEffect(() => {
    if (!context) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setContext(null);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [context]);

  const isValidConnection = useCallback(
    (c: Connection | LinkEdgeType) => {
      if (c.source === c.target) return false;
      const links = workflow.links ?? [];
      // Records only flow forward: a pipeline takes one link per pair and no link that closes a cycle.
      if (pipeline) return !links.some((l) => l.from === c.source && l.to === c.target) && !closesCycle(workflow, c.source, c.target);
      return !links.some((l) => l.from === c.source && l.to === c.target && kindOfLink(l) === "success");
    },
    [workflow, pipeline],
  );
  const connect = useCallback(
    (c: Connection) => {
      if (!onConnect || !c.source || !c.target) return;
      // New pipeline links are data links; their type is changed in the link menu.
      if (pipeline) return onConnect({ from: c.source, to: c.target, type: "data" });
      // A link back to an earlier action is a loop link (decision for mockup E6b).
      onConnect({ from: c.source, to: c.target, ...(closesCycle(workflow, c.source, c.target) ? { loop: true } : {}) });
    },
    [onConnect, workflow, pipeline],
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
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") return setMenu(null);
      // In a pipeline, D and E choose the type of the link (mockup S25a, state "link").
      const kind = pipeline && !e.ctrlKey && !e.metaKey && !e.altKey && pipelineKinds.find(([, , key]) => key?.toLowerCase() === e.key.toLowerCase())?.[0];
      if (!kind) return;
      e.preventDefault();
      setMenu(null);
      const edge = edges.find((x) => x.id === menu);
      if (edge && edge.data?.kind !== kind) onLinkKind?.(Number(menu), kind);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menu, pipeline, edges, onLinkKind]);

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
      ref={container}
      className="runnel-canvas relative min-h-0 flex-1"
      data-testid="canvas"
      onMouseMove={(e) => {
        if (pointer) pointer.current = screenToFlowPosition({ x: e.clientX, y: e.clientY });
      }}
      onMouseLeave={() => {
        if (pointer) pointer.current = null;
      }}
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
        connectionLineComponent={pipeline ? DataConnectionLine : ConnectionLine}
        onEdgeClick={(_, e) => editable && openMenu(e.id)}
        onPaneClick={() => {
          setMenu(null);
          setContext(null);
        }}
        onNodeClick={() => {
          setMenu(null);
          setContext(null);
        }}
        onNodeContextMenu={onNodeContextMenu}
        onSelectionContextMenu={(e, ns) => openContext(e, ns.map((n) => n.id))}
        onPaneContextMenu={(e) => openContext(e, [])}
        onMoveStart={() => setContext(null)}
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
            <Ghost {...ghost} color={categoryColor[catalog.get(ghost.type)?.category ?? "general"] ?? categoryColor.general} id={newActionId(workflow, ghost.type)} />
          </ViewportPortal>
        )}
        {(preview || auto !== "none") && (
          // The hints at the top right, one below the other (mockups E5, S44).
          <Panel position="top-right" className="flex flex-col items-end gap-1.5">
            {preview && (
              <span
                data-testid="preview-hint"
                className="inline-flex h-[26px] items-center gap-1.5 rounded-full border border-border bg-card px-2.5 text-xs text-foreground shadow-sm"
              >
                <EyeIcon width={14} height={14} />
                Preview up to <b>{preview.upTo}</b> · first {preview.limit} records · nothing is written
                <button
                  type="button"
                  title="End the preview"
                  aria-label="End the preview"
                  onClick={preview.onClose}
                  className="cursor-pointer border-0 bg-transparent p-0 text-muted-foreground hover:text-foreground"
                >
                  ×
                </button>
              </span>
            )}
            {auto !== "none" && (
              <span
                data-testid="auto-layout"
                className="inline-flex h-[26px] items-center gap-1.5 rounded-full border border-border bg-card px-2.5 text-xs text-muted-foreground shadow-sm"
              >
                <LayoutIcon width={14} height={14} />
                {auto === "all"
                  ? "Arranged automatically – the file has no layout yet"
                  : `Some ${pipeline ? "transforms" : "actions"} have no position and were arranged automatically`}
              </span>
            )}
          </Panel>
        )}
      </ReactFlow>
      {context && (
        <ContextMenu
          {...context}
          pipeline={pipeline}
          canPaste={!!canPaste}
          onClose={() => setContext(null)}
          onClipboard={(op) => onClipboard?.(op, context.ids)}
          onPaste={() => onPaste?.(context.at)}
          onDelete={() => onDelete?.(context.ids, [])}
        />
      )}
    </div>
  );
}

/** Context menu of the selected actions or of the free canvas (mockup E16, state "copy"). */
function ContextMenu(props: {
  x: number;
  y: number;
  ids: string[];
  pipeline: boolean;
  canPaste: boolean;
  onClose: () => void;
  onClipboard: (op: "copy" | "cut") => void;
  onPaste: () => void;
  onDelete: () => void;
}) {
  const { ids, pipeline } = props;
  const noun = pipeline ? "transform" : "action";
  const item = (label: string, key: string, icon: ReactNode, run: () => void, disabled = false, danger = false) => (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onClick={() => {
        props.onClose();
        run();
      }}
      className={
        "flex h-[30px] w-full cursor-pointer items-center gap-2.5 rounded-sm border-0 bg-transparent px-2 text-left hover:bg-muted disabled:cursor-default disabled:opacity-55 disabled:hover:bg-transparent " +
        (danger ? "text-destructive" : "")
      }
    >
      <span className={danger ? "" : "text-muted-foreground"}>{icon}</span>
      {label}
      <span className="ml-auto font-mono text-[10px] text-muted-foreground">{key}</span>
    </button>
  );
  return (
    <div
      role="menu"
      aria-label={ids.length > 0 ? `${ids.length} ${noun}${ids.length === 1 ? "" : "s"}` : "Canvas"}
      className="absolute z-10 w-[196px] rounded-lg border border-border bg-popover p-1 text-sm text-popover-foreground shadow-md"
      style={{ left: props.x, top: props.y }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {ids.length > 0 && item("Cut", "Ctrl+X", <CutIcon width={15} height={15} />, () => props.onClipboard("cut"))}
      {ids.length > 0 && item("Copy", "Ctrl+C", <CopyIcon width={15} height={15} />, () => props.onClipboard("copy"))}
      {item("Paste", "Ctrl+V", <PasteIcon width={15} height={15} />, props.onPaste, !props.canPaste)}
      {ids.length > 0 && (
        <>
          <hr className="mx-0 my-1 border-0 border-t border-border" />
          {item("Delete", "Del", <TrashIcon width={15} height={15} />, props.onDelete, false, true)}
        </>
      )}
    </div>
  );
}

/**
 * How a link looks in a run: it "flows" while it leads to the running action, and is
 * faded once its source has finished without the link applying (or never ran).
 */
function linkRun(run: RunView | null | undefined, l: Link): "flow" | "dead" | undefined {
  if (!run || l.disabled) return undefined;
  const from = run.actions[l.from]?.status;
  const to = run.actions[l.to]?.status;
  if (!from || from === "pending" || from === "running") return undefined;
  const kind = kindOfLink(l);
  if (kind === "expression") return undefined;
  const applies =
    (kind === "success" && from === "succeeded") ||
    (kind === "failure" && from === "failed") ||
    (kind === "always" && (from === "succeeded" || from === "failed"));
  if (!applies) return "dead";
  return to === "running" ? "flow" : undefined;
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

/** Preview of a new pipeline link: a data link (grey). */
function DataConnectionLine({ fromX, fromY, toX, toY }: ConnectionLineComponentProps) {
  return <path d={linkPath(fromX, fromY, toX, toY).path} fill="none" stroke="var(--link-always)" strokeWidth={2} strokeDasharray="6 4" />;
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
