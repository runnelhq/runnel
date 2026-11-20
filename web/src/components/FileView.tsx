import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { APIError, fetchFile, fullPath, isYAML, kindLabel, kindOf, saveFile } from "../files";
import { editWorkflow, graphOf, problemTarget, validate, type GraphKind, type Link, type Problem, type Validation, type WorkflowEdit } from "../workflow";
import { Canvas, type Selection } from "./Canvas";
import { BottomPanel, type BottomTab } from "./BottomPanel";
import { RunDialog } from "./RunDialog";
import type { ProjectInfo } from "../api";
import { loadRunSettings, saveRunSettings, StartError, structureSignature, useRun, type RunSettings, type RunView } from "../run";
import { CodeView } from "./CodeView";
import { Dialog } from "./Dialog";
import { PropertiesPanel, type FieldValue, type LinkChange } from "./PropertiesPanel";
import type { SettableKind } from "./LinkEdge";
import type { PaletteTarget } from "./Palette";
import {
  ChevronIcon,
  CodeIcon,
  CopyIcon,
  LockIcon,
  PanelIcon,
  PlayIcon,
  RedoIcon,
  ReloadIcon,
  SaveIcon,
  StopIcon,
  PipelineIcon,
  UndoIcon,
  WorkflowIcon,
} from "./Icons";

type View = "canvas" | "yaml";

/** A loaded file: the text shown (possibly changed), the saved text and its ETag. */
interface Doc {
  text: string;
  saved: string;
  etag: string;
  encoding: string;
}

type State = { status: "loading" } | { status: "loaded"; doc: Doc } | { status: "error"; notFound: boolean; message: string };

/** Functions of a file view that the app calls, e.g. to save before closing a tab. */
export interface FileHandle {
  save: () => Promise<boolean>;
}

/** Summary of the shown file for the status bar, e.g. "YAML · UTF-8 · 40 lines". */
export function fileSummary(path: string, text: string, encoding: string): string {
  const lines = text.replace(/\r\n?/g, "\n").replace(/\n$/, "").split("\n").length;
  return [isYAML(path) ? "YAML" : "Text", encoding, `${lines} ${lines === 1 ? "line" : "lines"}`].join(" · ");
}

const historyLimit = 100;

/**
 * Shows one file. Workflows and pipelines (S25a) open as canvas and can be edited there (E6a, E6b): every change
 * is applied to the text by the server; undo/redo keep text versions; saving uses the
 * ETag and asks what to do when the file was changed elsewhere. Other files are read-only.
 * The view stays mounted while its tab is open, so that unsaved changes survive tab switches.
 */
export function FileView(props: {
  path: string;
  active: boolean;
  projectDir: string | null;
  project: ProjectInfo | null;
  /** The run of this file to show in the status bar (only called while the tab is active). */
  onRun: (run: RunView | null) => void;
  epoch: number;
  onSummary: (summary: string | null) => void;
  onPalette: (target: PaletteTarget | null) => void;
  onDirty: (path: string, dirty: boolean) => void;
  onRegister: (path: string, handle: FileHandle | null) => void;
  onReloadExplorer: () => void;
}) {
  const { path, active, epoch, onSummary, onPalette, onDirty, onRegister, onRun } = props;
  const [state, setState] = useState<State>({ status: "loading" });
  const fileKind = kindOf(path);
  const isWorkflow = fileKind === "workflow";
  // Workflows and pipelines are graphs: shown on the canvas and edited there.
  const graphKind: GraphKind | null = fileKind === "workflow" || fileKind === "pipeline" ? fileKind : null;
  const isGraph = graphKind !== null;
  const [view, setView] = useState<View>(isGraph ? "canvas" : "yaml");
  const [focusLine, setFocusLine] = useState<number | null>(null);
  const [validation, setValidation] = useState<Validation | null>(null);
  const [undo, setUndo] = useState<string[]>([]);
  const [redo, setRedo] = useState<string[]>([]);
  const [toast, setToast] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [selection, setSelection] = useState<Selection>({ actions: [], links: [] });
  const [bottomTab, setBottomTab] = useState<BottomTab | null>(null);
  const [logFocus, setLogFocus] = useState<{ actionId: string; seq: number } | null>(null);
  // A field of the properties panel to show and focus, e.g. after clicking a problem.
  const [focusField, setFocusField] = useState<{ key: string; seq: number } | null>(null);
  const [panel, setPanel] = useState(true);

  const doc = state.status === "loaded" ? state.doc : null;
  const dirty = doc !== null && doc.text !== doc.saved;

  const dirtyRef = useRef(false);
  dirtyRef.current = dirty;

  const load = useCallback((force = false) => {
    let cancelled = false;
    fetchFile(path).then(
      (f) => {
        // A reload must never replace changes made while it was on its way.
        if (cancelled || (dirtyRef.current && !force)) return;
        setState({ status: "loaded", doc: { text: f.text, saved: f.text, etag: f.etag ?? "*", encoding: f.encoding } });
        setUndo([]);
        setRedo([]);
      },
      (err: unknown) =>
        !cancelled &&
        setState({
          status: "error",
          notFound: err instanceof APIError && err.status === 404,
          message: err instanceof Error ? err.message : String(err),
        }),
    );
    return () => {
      cancelled = true;
    };
  }, [path]);

  // Load at start and after a reconnect – unless there are unsaved changes, which are kept.
  useEffect(() => {
    if (dirtyRef.current) return;
    return load();
  }, [load, epoch]);

  useEffect(() => onDirty(path, dirty), [path, dirty, onDirty]);

  // The server checks the text shown and returns the parsed workflow or pipeline for the canvas.
  const text = doc?.text;
  useEffect(() => {
    if (!isGraph || text === undefined) return;
    let cancelled = false;
    validate(path, text).then(
      (v) => !cancelled && setValidation(v),
      () => !cancelled && setValidation(null),
    );
    return () => {
      cancelled = true;
    };
  }, [isGraph, path, text]);
  const graph = useMemo(() => graphOf(validation), [validation]);
  const steps = graphKind === "pipeline" ? "transforms" : "actions";

  useEffect(() => {
    if (!active) return;
    if (!doc) return onSummary(null);
    onSummary(
      view === "canvas" && graph
        ? `${graph.kind === "pipeline" ? "Pipeline" : "Workflow"} · ${graph.actions?.length ?? 0} ${steps} · ${graph.links?.length ?? 0} links`
        : fileSummary(path, doc.text, doc.encoding),
    );
  }, [active, doc, path, view, graph, steps, onSummary]);

  const docRef = useRef<Doc | null>(null);
  docRef.current = doc;
  // Key of the field changed last: further changes of it are one undo step (decision for mockup E7).
  const lastField = useRef<string | null>(null);
  const setText = useCallback((next: string, history: "push" | "undo" | "redo" | "merge") => {
    const current = docRef.current;
    if (!current || current.text === next) return;
    const prev = current.text;
    if (history !== "merge") lastField.current = null;
    if (history === "merge") {
      setRedo([]);
    } else if (history === "push") {
      setUndo((u) => [...u.slice(-historyLimit + 1), prev]);
      setRedo([]);
    } else if (history === "undo") {
      setUndo((u) => u.slice(0, -1));
      setRedo((r) => [...r, prev]);
    } else {
      setRedo((r) => r.slice(0, -1));
      setUndo((u) => [...u, prev]);
    }
    docRef.current = { ...current, text: next }; // visible to a save right away, before the next render
    setState((s) => (s.status === "loaded" ? { status: "loaded", doc: { ...s.doc, text: next } } : s));
  }, []);

  // The edits on their way to the server, one after the other; saving waits for them.
  const pending = useRef<Promise<unknown> | null>(null);
  const applyEdit = useCallback(
    (edit: WorkflowEdit, field?: string): Promise<string | null> => {
      const run = async () => {
        await pending.current?.catch(() => undefined);
        const current = docRef.current;
        if (!current) return null;
        try {
          const next = await editWorkflow(path, current.text, edit, graphKind ?? "workflow");
          const merge = field !== undefined && lastField.current === field;
          setText(next, merge ? "merge" : "push");
          lastField.current = field ?? null;
          setEditError(null);
          return next;
        } catch (err) {
          setEditError(err instanceof Error ? err.message : String(err));
          return null;
        }
      };
      const request = run();
      pending.current = request;
      return request;
    },
    [path, setText, graphKind],
  );

  type Positions = Record<string, { x: number; y: number }>;
  const onMove = useCallback((positions: Positions) => void applyEdit({ move: positions }), [applyEdit]);
  const onDelete = useCallback(
    async (ids: string[], links: number[]) => {
      const before = graph?.links?.length ?? 0;
      const next = await applyEdit({ delete: ids, deleteLinks: links });
      if (next === null) return;
      const after = graphOf(await validate(path, next).catch(() => null));
      const removed = after ? before - (after.links?.length ?? 0) : links.length;
      const linkText = `${removed} ${removed === 1 ? "link" : "links"}`;
      if (ids.length === 0) {
        setToast(`Deleted ${linkText}`);
        return;
      }
      const what = ids.length === 1 ? ids[0] : `${ids.length} ${steps}`;
      setToast(`Deleted ${what}${removed > 0 ? ` and ${linkText}` : ""}`);
    },
    [applyEdit, graph, path, steps],
  );
  // A new action from the palette; its id can be changed right away (mockup E6b).
  const onAdd = useCallback(
    async (action: { id: string; type: string }, positions: Positions) => {
      if ((await applyEdit({ add: [action], move: positions })) !== null) setRenaming(action.id);
    },
    [applyEdit],
  );
  const onRename = useCallback((from: string, to: string) => void applyEdit({ rename: { [from]: to } }), [applyEdit]);
  const onConnect = useCallback((link: Link) => void applyEdit({ addLinks: [link] }), [applyEdit]);
  // While a rename from the panel is on its way, the canvas briefly has no selection; keep the new id.
  const renamedTo = useRef<string | null>(null);
  const onSelect = useCallback((next: Selection) => {
    const ids = next.actions;
    const to = renamedTo.current;
    if (to && !ids.includes(to)) {
      if (ids.length === 0) return;
      renamedTo.current = null;
    }
    if (to && ids.includes(to)) renamedTo.current = null;
    setSelection((s) =>
      s.actions.length === ids.length && s.actions.every((id, i) => id === ids[i]) &&
      s.links.length === next.links.length && s.links.every((l, i) => l === next.links[i])
        ? s
        : next,
    );
  }, []);
  const onLinkChange = useCallback(
    (index: number, change: LinkChange) => void applyEdit({ updateLinks: [{ index, ...change }] }),
    [applyEdit],
  );
  const onSetWorkflow = useCallback(
    (field: string, value: FieldValue) => void applyEdit({ setWorkflow: [{ field, value }] }, `workflow:${field}`),
    [applyEdit],
  );
  const onSet = useCallback(
    (id: string, field: string, value: FieldValue) => void applyEdit({ set: [{ id, field, value }] }, `${id}:${field}`),
    [applyEdit],
  );
  const onLinkKind = useCallback(
    (index: number, kind: SettableKind) =>
      void applyEdit({ updateLinks: [kind === "data" || kind === "error" ? { index, type: kind } : { index, when: kind }] }),
    [applyEdit],
  );

  const doUndo = useCallback(() => {
    if (undo.length > 0) setText(undo[undo.length - 1], "undo");
  }, [undo, setText]);
  const doRedo = useCallback(() => {
    if (redo.length > 0) setText(redo[redo.length - 1], "redo");
  }, [redo, setText]);

  // Saving: with the ETag of the loaded text; a 412 means the file was changed elsewhere.
  const save = useCallback(
    async (overwrite = false): Promise<boolean> => {
      await pending.current?.catch(() => undefined);
      const current = docRef.current;
      if (!current) return false;
      if (current.text === current.saved && !overwrite) return true;
      try {
        const etag = await saveFile(path, current.text, overwrite ? "*" : current.etag);
        setState((s) => (s.status === "loaded" ? { status: "loaded", doc: { ...s.doc, saved: current.text, etag } } : s));
        setConflict(false);
        return true;
      } catch (err) {
        if (err instanceof APIError && err.status === 412) setConflict(true);
        else setEditError(`Could not save: ${err instanceof Error ? err.message : String(err)}`);
        return false;
      }
    },
    [path],
  );

  useEffect(() => {
    onRegister(path, { save: () => save() });
    return () => onRegister(path, null);
  }, [path, save, onRegister]);

  // Running (mockup E8): Run uses the remembered values; the dialog opens on the first run of a
  // workflow with parameters or several environments, or via the arrow. Unsaved changes are asked about.
  const { run, start: startRun, cancel: cancelRun } = useRun(path);
  const [runDialog, setRunDialog] = useState(false);
  const [unsavedAsk, setUnsavedAsk] = useState<null | { dialog: boolean }>(null);
  const running = run?.status === "running";
  const wfNow = validation?.workflow;
  // The status belongs to the actions and links it ran with; it is hidden once they change.
  const shownRun = run && wfNow && run.signature === structureSignature(wfNow) ? run : null;
  useEffect(() => {
    if (active) onRun(run);
  }, [active, run, onRun]);

  const doRun = useCallback(
    async (settings: RunSettings) => {
      const wf = validation?.workflow;
      if (!wf) return;
      try {
        await startRun(settings, wf);
        setBottomTab("log"); // the log opens when a run starts and follows it (mockup E8)
        setEditError(null);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setEditError(`Could not start the run: ${msg}`);
        if (err instanceof StartError && err.problems.length > 0) setBottomTab("problems");
      }
    },
    [validation, startRun],
  );
  const proceedRun = useCallback(
    (dialog: boolean) => {
      const remembered = loadRunSettings(path);
      const params = validation?.workflow?.parameters ?? [];
      const envs = props.project?.environments ?? [];
      if (dialog || (!remembered && (params.length > 0 || envs.length > 1))) setRunDialog(true);
      else void doRun(remembered ?? { environment: "", params: {} });
    },
    [path, validation, props.project, doRun],
  );
  const requestRun = useCallback(
    (dialog: boolean) => {
      if (running) return;
      if (docRef.current && docRef.current.text !== docRef.current.saved) setUnsavedAsk({ dialog });
      else proceedRun(dialog);
    },
    [running, proceedRun],
  );

  // Keyboard shortcuts of the active tab: Ctrl+S, Ctrl+Z, Ctrl+Y / Ctrl+Shift+Z, F5 (run), Ctrl+. (cancel).
  useEffect(() => {
    if (!active || !isGraph) return;
    const onKey = (e: KeyboardEvent) => {
      if (isWorkflow && e.key === "F5" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        e.preventDefault(); // instead of reloading the page
        requestRun(e.shiftKey);
        return;
      }
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
      if (isWorkflow && e.key === ".") {
        e.preventDefault();
        void cancelRun();
        return;
      }
      const k = e.key.toLowerCase();
      // In a text field (id, search), undo and redo belong to the field.
      const t = e.target as HTMLElement | null;
      if (k !== "s" && t && (t.isContentEditable || ["INPUT", "TEXTAREA"].includes(t.tagName))) return;
      if (k === "s") {
        e.preventDefault();
        void save();
      } else if (k === "z" && !e.shiftKey) {
        e.preventDefault();
        doUndo();
      } else if (k === "y" || (k === "z" && e.shiftKey)) {
        e.preventDefault();
        doRedo();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, isGraph, isWorkflow, save, doUndo, doRedo, requestRun, cancelRun]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 6000);
    return () => clearTimeout(t);
  }, [toast]);

  // The palette can add actions (transforms) while an editable canvas is shown.
  const canEdit = isGraph && view === "canvas" && !!graph;
  const hasStart = !!graph?.actions?.some((a) => a.type === "core.start");
  useEffect(() => {
    if (active) onPalette(graphKind ? { kind: graphKind, editable: canEdit, hasStart } : null);
  }, [active, graphKind, canEdit, hasStart, onPalette]);

  const showInYAML = useCallback((line: number) => {
    setView("yaml");
    setFocusLine(line);
  }, []);

  // A click on a problem selects what it belongs to and shows the field in the properties panel.
  const openProblem = useCallback(
    (p: Problem) => {
      const target = graph ? problemTarget(graph, p) : null;
      if (!target) {
        if (p.line) showInYAML(p.line);
        return;
      }
      setSelection({ actions: target.action ? [target.action] : [], links: target.link !== undefined ? [target.link] : [] });
      setPanel(true);
      if (target.field) setFocusField((f) => ({ key: target.field!, seq: (f?.seq ?? 0) + 1 }));
    },
    [graph, showInYAML],
  );

  if (!active) return null;

  if (state.status === "error") {
    return (
      <div className="grid flex-1 place-items-center p-8" role="alert">
        <div className="max-w-[420px] text-center">
          <span className="inline-grid size-10 place-items-center rounded-full bg-danger-soft text-lg font-bold text-destructive">
            ✖
          </span>
          <h2 className="mt-4 mb-1 text-md font-semibold">{state.notFound ? "File not found" : "File could not be loaded"}</h2>
          <p className="mt-0 mb-4 text-muted-foreground">
            <code className="text-xs">{path}</code>{" "}
            {state.notFound
              ? "no longer exists. It may have been renamed or deleted outside the editor."
              : `could not be loaded: ${state.message}`}
          </p>
          <SmallButton onClick={props.onReloadExplorer}>
            <ReloadIcon width={14} height={14} />
            Reload explorer
          </SmallButton>
        </div>
      </div>
    );
  }

  const slash = path.lastIndexOf("/");
  const editable = isGraph && !!graph;
  const showPanel = editable && view === "canvas" && panel;
  // Workflows have the compact file bar of mockups E7/E8 in every view, with or without the
  // properties panel, so that nothing moves when switching between canvas and YAML: the
  // name with the path in its tooltip, "Copy path" as a symbol next to it, no kind badge.
  const compact = isGraph;
  // The badges open the list of problems below the canvas, also from the YAML view.
  const openProblems = editable
    ? () => {
        setView("canvas");
        setBottomTab("problems");
      }
    : undefined;
  return (
    <>
      <div className="flex h-10 shrink-0 items-center gap-3 border-b border-border px-4">
        <span className="font-mono text-xs whitespace-nowrap text-muted-foreground" data-testid="crumbs" title={path}>
          {slash >= 0 && !compact && `${path.slice(0, slash).split("/").join(" / ")} / `}
          <b className="font-medium text-foreground">{path.slice(slash + 1)}</b>
        </span>
        {compact ? (
          <CopyPath path={props.projectDir ? fullPath(props.projectDir, path) : path} compact />
        ) : (
          <Badge testId="kind">{kindLabel[kindOf(path)]}</Badge>
        )}
        {!isGraph && (
          <Badge title="Only workflows and pipelines can be edited in this version">
            <LockIcon width={12} height={12} />
            Read-only
          </Badge>
        )}
        {validation && validation.errors > 0 && (
          <Badge
            testId="error-count"
            tone="error"
            title={problemList(validation, "error")}
            onClick={openProblems}
          >
            ✖ {validation.errors}
          </Badge>
        )}
        {validation && validation.warnings > 0 && (
          <Badge
            testId="warning-count"
            tone="warning"
            title={problemList(validation, "warning")}
            onClick={openProblems}
          >
            ⚠ {validation.warnings}
          </Badge>
        )}
        <span className="flex-1" />
        {isWorkflow &&
          (running ? (
            <button
              type="button"
              title="Cancel the run (Ctrl+.)"
              onClick={() => void cancelRun()}
              className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-destructive bg-background px-2.5 text-xs font-medium text-destructive hover:bg-danger-soft"
            >
              <StopIcon width={14} height={14} />
              Cancel
            </button>
          ) : (
            <span className="inline-flex">
              <button
                type="button"
                title="Run (F5)"
                onClick={() => requestRun(false)}
                className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-l-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
              >
                <span className="text-status-success">
                  <PlayIcon width={14} height={14} />
                </span>
                Run
              </button>
              <button
                type="button"
                title="Run with other values… (Shift+F5)"
                aria-label="Run with other values"
                onClick={() => requestRun(true)}
                className="inline-grid h-7 w-7 cursor-pointer place-items-center rounded-r-md border border-l-0 border-input bg-background text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <ChevronIcon open width={14} height={14} />
              </button>
            </span>
          ))}
        {isGraph && (
          <>
            <div className="inline-flex gap-1">
              <IconButton title="Undo (Ctrl+Z)" disabled={undo.length === 0} onClick={doUndo}>
                <UndoIcon width={14} height={14} />
              </IconButton>
              <IconButton title="Redo (Ctrl+Y)" disabled={redo.length === 0} onClick={doRedo}>
                <RedoIcon width={14} height={14} />
              </IconButton>
            </div>
            <button
              type="button"
              title="Save (Ctrl+S)"
              disabled={!dirty}
              onClick={() => void save()}
              className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-primary bg-primary px-2.5 text-xs font-medium text-primary-foreground disabled:cursor-default disabled:opacity-50"
            >
              <SaveIcon width={14} height={14} />
              Save
            </button>
            <ViewSwitch view={view} onView={setView} pipeline={graphKind === "pipeline"} />
            {editable && view === "canvas" && !panel && (
              <IconButton title="Show properties" onClick={() => setPanel(true)}>
                <PanelIcon width={14} height={14} />
              </IconButton>
            )}
          </>
        )}
        {!compact && <CopyPath path={props.projectDir ? fullPath(props.projectDir, path) : path} />}
      </div>
      {editError && (
        <div role="alert" className="flex shrink-0 items-center gap-3 border-b border-border bg-danger-soft px-4 py-1.5 text-xs text-destructive">
          ✖ {editError}
          <button type="button" className="ml-auto cursor-pointer border-0 bg-transparent text-xs text-muted-foreground" onClick={() => setEditError(null)}>
            Dismiss
          </button>
        </div>
      )}
      <div className="flex min-h-0 flex-1">
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {!doc ? (
          <div className="flex-1" aria-busy="true" />
        ) : view === "yaml" ? (
          <CodeView text={doc.text} yaml={isYAML(path)} focusLine={focusLine} />
        ) : graph ? (
          <Canvas
            workflow={graph}
            problems={validation?.problems ?? []}
            onShowInYAML={showInYAML}
            onMove={editable ? onMove : undefined}
            onDelete={editable ? onDelete : undefined}
            onAdd={editable ? onAdd : undefined}
            onRename={editable ? onRename : undefined}
            onConnect={editable ? onConnect : undefined}
            onLinkKind={editable ? onLinkKind : undefined}
            renaming={renaming}
            onRenaming={setRenaming}
            onSelect={onSelect}
            selection={selection}
            problemsInPanel={showPanel}
            run={shownRun}
          />
        ) : validation ? (
          <NotAGraph path={path} kind={graphKind ?? "workflow"} validation={validation} onShowYAML={() => showInYAML(validation.problems[0]?.line ?? 1)} />
        ) : (
          <div className="runnel-canvas flex-1" aria-busy="true" />
        )}
        {editable && view === "canvas" && graph && (
          <BottomPanel
            workflow={graph}
            problems={validation?.problems ?? []}
            tab={bottomTab}
            onTab={setBottomTab}
            onOpen={openProblem}
            run={run}
            logFocus={logFocus}
            onSelectAction={(id) => {
              setSelection({ actions: [id], links: [] });
              setPanel(true);
            }}
          />
        )}
        {toast && (
          <div
            role="status"
            className="absolute bottom-5 left-1/2 inline-flex h-9 -translate-x-1/2 items-center gap-3 rounded-lg border border-border bg-card py-0 pr-1.5 pl-3.5 shadow-md"
          >
            <span>{toast}</span>
            <SmallButton
              onClick={() => {
                doUndo();
                setToast(null);
              }}
            >
              <UndoIcon width={14} height={14} />
              Undo
            </SmallButton>
          </div>
        )}
      </div>
      {showPanel && graph && (
        <PropertiesPanel
          workflow={graph}
          schemas={validation?.schemas}
          selected={selection}
          problems={validation?.problems ?? []}
          focus={focusField}
          run={shownRun}
          onShowInLog={(actionId) => {
            setBottomTab("log");
            setLogFocus((f) => ({ actionId, seq: (f?.seq ?? 0) + 1 }));
          }}
          onSet={onSet}
          onSetWorkflow={onSetWorkflow}
          onLinkChange={onLinkChange}
          onDeleteLink={(index) => void onDelete([], [index])}
          onRename={(from, to) => {
            renamedTo.current = to;
            setSelection({ actions: [to], links: [] });
            onRename(from, to);
          }}
          onHide={() => setPanel(false)}
        />
      )}
      </div>
      {runDialog && (
        <RunDialog
          path={path}
          project={props.project}
          parameters={validation?.workflow?.parameters ?? []}
          initial={loadRunSettings(path)}
          onCancel={() => setRunDialog(false)}
          onRun={(settings) => {
            setRunDialog(false);
            saveRunSettings(path, settings);
            void doRun(settings);
          }}
        />
      )}
      {unsavedAsk && (
        <Dialog
          title="Save before running?"
          onCancel={() => setUnsavedAsk(null)}
          actions={[
            { label: "Cancel", onClick: () => setUnsavedAsk(null) },
            {
              label: "Run saved version",
              onClick: () => {
                const { dialog } = unsavedAsk;
                setUnsavedAsk(null);
                proceedRun(dialog);
              },
            },
            {
              label: "Save and run",
              tone: "primary",
              onClick: async () => {
                const { dialog } = unsavedAsk;
                setUnsavedAsk(null);
                if (await save()) proceedRun(dialog);
              },
            },
          ]}
        >
          <p>
            <code className="text-xs">{path}</code> has unsaved changes. A run always uses the saved file.
          </p>
        </Dialog>
      )}
      {conflict && (
        <Dialog
          title="File changed outside the editor"
          onCancel={() => setConflict(false)}
          actions={[
            { label: "Cancel", onClick: () => setConflict(false) },
            {
              label: (
                <>
                  <ReloadIcon width={14} height={14} />
                  Reload
                </>
              ),
              onClick: () => {
                setConflict(false);
                load(true); // discards the unsaved changes on purpose
              },
            },
            { label: "Overwrite", tone: "danger", onClick: () => void save(true) },
          ]}
        >
          <p>
            <code className="text-xs">{path}</code> was changed after you opened it – for example in another browser, a text
            editor or by <code className="text-xs">git pull</code>. Your changes have not been saved yet.
          </p>
          <ul>
            <li>
              <b>Reload</b> shows the file as it is now; your unsaved changes are discarded.
            </li>
            <li>
              <b>Overwrite</b> saves your version; the other changes are lost.
            </li>
          </ul>
        </Dialog>
      )}
    </>
  );
}

const problemList = (v: Validation, severity: "error" | "warning") =>
  v.problems
    .filter((p) => p.severity === severity)
    .map((p) => (p.line ? `${p.line}:${p.column ?? 1} ${p.message}` : p.message))
    .join("\n");

function ViewSwitch(props: { view: View; onView: (v: View) => void; pipeline?: boolean }) {
  const views: [View, string, typeof CodeIcon][] = [
    ["canvas", "Canvas", props.pipeline ? PipelineIcon : WorkflowIcon],
    ["yaml", "YAML", CodeIcon],
  ];
  return (
    <div role="group" aria-label="View" className="inline-flex gap-0.5 rounded-md border border-border bg-muted p-0.5">
      {views.map(([v, label, Icon]) => (
        <button
          key={v}
          type="button"
          aria-pressed={props.view === v}
          onClick={() => props.onView(v)}
          className="inline-flex cursor-pointer items-center gap-1.5 rounded-sm px-2 py-[3px] text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm"
        >
          <Icon width={14} height={14} />
          {label}
        </button>
      ))}
    </div>
  );
}

/** Shown instead of the canvas when the file cannot be read as a workflow. */
function NotAGraph(props: { path: string; kind: GraphKind; validation: Validation; onShowYAML: () => void }) {
  const first = props.validation.problems.find((p) => p.severity === "error");
  return (
    <div className="runnel-canvas grid flex-1 place-items-center p-8" role="alert">
      <div className="max-w-[440px] rounded-xl border border-border bg-card px-7 py-6 text-center shadow-md">
        <span className="inline-grid size-10 place-items-center rounded-full bg-danger-soft text-lg font-bold text-destructive">
          ✖
        </span>
        <h2 className="mt-3 mb-1 text-md font-semibold">This {props.kind} can’t be shown as a graph</h2>
        <p className="mt-0 mb-3 text-muted-foreground">The file has errors. Fix them in the YAML view or in your text editor.</p>
        {first && (
          <pre className="mt-0 mb-4 rounded-md bg-muted px-2.5 py-2 text-left text-xs whitespace-pre-wrap">
            {props.path}
            {first.line ? `:${first.line}:${first.column ?? 1}` : ""}
            {"\n"}
            {first.message}
          </pre>
        )}
        <SmallButton onClick={props.onShowYAML}>
          <CodeIcon width={14} height={14} />
          Show YAML
        </SmallButton>
      </div>
    </div>
  );
}

function Badge(props: {
  children: React.ReactNode;
  title?: string;
  testId?: string;
  tone?: "error" | "warning";
  onClick?: () => void;
}) {
  const tone =
    props.tone === "error"
      ? "bg-danger-soft text-destructive"
      : props.tone === "warning"
        ? "bg-warning-soft text-warning"
        : "bg-secondary text-secondary-foreground";
  return (
    <span
      title={props.title}
      data-testid={props.testId}
      role={props.onClick ? "button" : undefined}
      tabIndex={props.onClick ? 0 : undefined}
      onClick={props.onClick}
      onKeyDown={(e) => props.onClick && (e.key === "Enter" || e.key === " ") && props.onClick()}
      className={`inline-flex h-5 items-center gap-1 rounded-full px-2 text-2xs font-semibold whitespace-pre-line ${tone}${props.onClick ? " cursor-pointer" : ""}`}
    >
      {props.children}
    </span>
  );
}

function IconButton(props: { children: React.ReactNode; title: string; disabled?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      title={props.title}
      aria-label={props.title.replace(/ \(.*\)$/, "")}
      disabled={props.disabled}
      onClick={props.onClick}
      className="grid size-7 cursor-pointer place-items-center rounded-md border border-input bg-background text-foreground hover:bg-muted disabled:cursor-default disabled:text-muted-foreground disabled:opacity-50 disabled:hover:bg-background"
    >
      {props.children}
    </button>
  );
}

function SmallButton(props: { children: React.ReactNode; onClick: () => void; title?: string; ariaLabel?: string }) {
  return (
    <button
      type="button"
      title={props.title}
      aria-label={props.ariaLabel}
      onClick={props.onClick}
      className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
    >
      {props.children}
    </button>
  );
}

function CopyPath(props: { path: string; compact?: boolean }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <SmallButton
      title="Copy the file path"
      ariaLabel={props.compact ? (copied ? "Copied" : "Copy path") : undefined}
      onClick={() => navigator.clipboard.writeText(props.path).then(() => setCopied(true), () => setCopied(false))}
    >
      {copied && props.compact ? "✔" : <CopyIcon width={14} height={14} />}
      {!props.compact && (copied ? "✔ Copied" : "Copy path")}
    </SmallButton>
  );
}
