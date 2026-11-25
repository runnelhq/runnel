import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { APIError, fetchFile, fullPath, isYAML, kindLabel, kindOf, saveFile } from "../files";
import {
  clipKind,
  copySteps,
  editWorkflow,
  fieldsList,
  graphOf,
  problemTarget,
  pasteSteps,
  validate,
  type EntryEdit,
  type GraphKind,
  type KeyValue,
  type Link,
  type Pasted,
  type Problem,
  type Validation,
  type WorkflowEdit,
} from "../workflow";
import { Canvas, type Selection } from "./Canvas";
import { BottomPanel, type BottomTab } from "./BottomPanel";
import { RunDialog } from "./RunDialog";
import { RunInfoBar, type RunChanges } from "./RunInfoBar";
import { fetchRun } from "../runs";
import { DetectFieldsDialog } from "./DetectFieldsDialog";
import { PreviewPanel, type PreviewSelection } from "./PreviewPanel";
import { listItems, type Item } from "../yaml";
import { previewRun, runPreview, type PreviewResult } from "../preview";
import type { ProjectInfo } from "../api";
import { loadRunSettings, saveRunSettings, StartError, structureSignature, useRun, type RunSettings, type RunView } from "../run";
import { CodeView } from "./CodeView";
import { ConnectionForm, TestButton, type TestResult } from "./ConnectionForm";
import { PluginActions, PluginView, TrustDialog } from "./PluginView";
import { pluginFile, pluginOf, reloadPlugin, trustPlugin, untrustPlugin, usePlugins, type PluginInfo } from "../plugins";
import { TriggerForm } from "./TriggerForm";
import { SettingsForm } from "./SettingsForm";
import { runTriggerTarget } from "../forms";
import { MarkdownView } from "./MarkdownView";
import { YamlEditor } from "./YamlEditor";
import { Dialog } from "./Dialog";
import { PropertiesPanel, type FieldValue, type LinkChange } from "./PropertiesPanel";
import type { SettableKind } from "./LinkEdge";
import type { PaletteTarget } from "./Palette";
import type { ReferenceAction, ReferenceTarget } from "./ReferencePanel";
import { insertField, useSchema } from "../reference";
import { useCatalog } from "../catalog";
import type { AssistContext } from "../yamlAssist";
import {
  ChevronIcon,
  CodeIcon,
  CopyIcon,
  EyeIcon,
  FormIcon,
  LinesIcon,
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

/** A message at the bottom of the canvas (E6a, E16); with undo it offers to undo the last change. */
interface Toast {
  text: string;
  detail?: string;
  status?: "ok" | "error";
  undo?: boolean;
}

/** The text copied last in this window (E16), to tell how many links were left out when it is pasted. */
let lastCopy: { text: string; omitted: number } | null = null;

const count = (n: number, noun: string) => `${n} ${noun}${n === 1 ? "" : "s"}`;

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
  return [isYAML(path) ? "YAML" : /\.md$/i.test(path) ? "Markdown" : "Text", encoding, `${lines} ${lines === 1 ? "line" : "lines"}`].join(" · ");
}

const historyLimit = 100;

/** The path of an input as written, if it has variables (shown in "Detect fields"). */
function rawPathOf(v: unknown): string | undefined {
  return typeof v === "string" && v.includes("${") ? v : undefined;
}

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
  /** Files of the project (paths), for the links of a document (E17). */
  files?: Set<string>;
  /** Opens a file of the project in its tab, e.g. from a link of a document. */
  onOpenFile?: (path: string) => void;
  /** Reports the open project.yaml, environment or trigger for the reference (E18b). */
  onReference?: (target: ReferenceTarget | null) => void;
  /** A request of the reference for this file: go to a line, insert a field or a variable. */
  request?: { seq: number; action: ReferenceAction } | null;
  /** Variables used but not set for this environment (E18b): a warning badge. */
  variableWarning?: { count: number; text: string } | null;
  /** A run of the run history to show on the canvas (S26e2); seq changes with every request. */
  showRun?: { id: string; seq: number } | null;
  /** Opens the list of runs filtered by this file. */
  onRunsOf?: (path: string) => void;
}) {
  const { path, active, epoch, onSummary, onPalette, onDirty, onRegister, onRun } = props;
  const [state, setState] = useState<State>({ status: "loading" });
  const fileKind = kindOf(path);
  // Workflows and pipelines are graphs: shown on the canvas and edited there.
  const graphKind: GraphKind | null = fileKind === "workflow" || fileKind === "pipeline" ? fileKind : null;
  const isGraph = graphKind !== null;
  // Files edited as text in the YAML view (E9, E10): graphs and the other files of a project.
  // Documents (E17) show "canvas" as the formatted view ("Preview") and "yaml" as the text.
  const isDoc = fileKind === "document";
  const isPlugin = fileKind === "plugin";
  const editsText = isGraph || isDoc || fileKind === "project" || fileKind === "environment" || fileKind === "trigger" || fileKind === "connection" || isPlugin;
  const isConnection = fileKind === "connection";
  const isTrigger = fileKind === "trigger";
  const settingsKind = fileKind === "environment" || fileKind === "project" ? fileKind : null;
  // Connections, triggers, environments and project.yaml open as a form (mockups S39b,
  // E20), plugin.yaml as the overview of its plugin (S52b1); "canvas" is the form there.
  const isForm = isConnection || isTrigger || settingsKind !== null || isPlugin;
  const [view, setView] = useState<View>(isGraph || isDoc || isForm ? "canvas" : "yaml");
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const [focusLine, setFocusLine] = useState<number | null>(null);
  const [validation, setValidation] = useState<Validation | null>(null);
  const [undo, setUndo] = useState<string[]>([]);
  const [redo, setRedo] = useState<string[]>([]);
  const [toast, setToast] = useState<Toast | null>(null);
  // Changes when something was copied, so that the context menu offers Paste.
  const [copies, setCopies] = useState(0);
  // Position of the mouse on the canvas, where Ctrl+V pastes.
  const pointer = useRef<{ x: number; y: number } | null>(null);
  const [conflict, setConflict] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [selection, setSelection] = useState<Selection>({ actions: [], links: [] });
  const [bottomTab, setBottomTab] = useState<BottomTab | null>(null);
  const [logFocus, setLogFocus] = useState<{ actionId: string; seq: number } | null>(null);
  // A field of the properties panel to show and focus, e.g. after clicking a problem.
  const [focusField, setFocusField] = useState<{ key: string; seq: number } | null>(null);
  const [panel, setPanel] = useState(true);
  // The transform whose fields are being detected (S46).
  const [detecting, setDetecting] = useState<string | null>(null);
  // Editing in the YAML view (E9): typed text waiting for the typing pause, a check on its way,
  // the line to show in the editor, and the function that hands over typed text at once.
  const [typing, setTyping] = useState(false);
  // Typed text the YAML editor has not handed over yet (typing pause): a reload must keep it.
  const typingRef = useRef(false);
  const onPending = useCallback((pending: boolean) => {
    typingRef.current = pending;
    setTyping(pending);
  }, []);
  const [checking, setChecking] = useState(false);
  const [yamlFocus, setYamlFocus] = useState<{ line: number; seq: number; from?: number; to?: number } | null>(null);
  const flushTyping = useRef<(() => void) | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  // The line of the cursor in the YAML view: switching to the canvas selects what is there.
  const cursorLine = useRef<number | null>(null);
  // The canvas reports an empty selection while it goes away; only the shown canvas selects.
  const viewRef = useRef(view);
  viewRef.current = view;
  // The data preview of a pipeline (S44): up to which transform, its result and what is shown.
  const [preview, setPreview] = useState<{
    upTo: string;
    limit: number;
    result: PreviewResult | null;
    error: string | null;
    loading: boolean;
    selected: PreviewSelection;
  } | null>(null);
  const previewSeq = useRef(0);
  // Leaving the YAML view forgets its cursor; entering it, the editor reports where it is
  // (this effect runs after the editor's own, so it must not reset then).
  useEffect(() => {
    if (view !== "yaml") setCursor(null);
  }, [view]);

  const doc = state.status === "loaded" ? state.doc : null;
  // The plugin of a plugin.yaml (S52b1): again after each save and reconnect.
  const plugins = usePlugins(isPlugin ? `${epoch}:${doc?.etag ?? ""}` : null);
  const [pluginOverride, setPluginOverride] = useState<PluginInfo | null>(null);
  const [trusting, setTrusting] = useState<"dialog" | "busy" | null>(null);
  const pluginName = isPlugin ? pluginOf(path) : null;
  const pluginInfo: PluginInfo | null | undefined = !isPlugin
    ? undefined
    : pluginOverride?.name === pluginName
      ? pluginOverride
      : plugins.info
        ? (plugins.info.plugins.find((x) => x.name === pluginName) ?? null)
        : plugins.error
          ? null
          : undefined;
  useEffect(() => setPluginOverride(null), [plugins.info]);
  const { onReloadExplorer } = props;
  const pluginAction = useCallback(
    async (act: () => Promise<PluginInfo>, failure: string) => {
      try {
        setPluginOverride(await act());
        onReloadExplorer(); // the state in the explorer
      } catch (e) {
        setEditError(`${failure}: ${(e as Error).message}`);
        plugins.reload();
      }
    },
    [onReloadExplorer, plugins],
  );
  const trust = useCallback(async () => {
    if (!pluginInfo?.hash) return;
    setTrusting("busy");
    await pluginAction(() => trustPlugin(pluginInfo.name, pluginInfo.hash!), "The plugin was not trusted");
    setTrusting(null);
  }, [pluginInfo, pluginAction]);
  const dirty = doc !== null && doc.text !== doc.saved;

  const dirtyRef = useRef(false);
  dirtyRef.current = dirty;

  const load = useCallback((force = false) => {
    let cancelled = false;
    fetchFile(path).then(
      (f) => {
        // A reload must never replace changes made while it was on its way: neither text
        // typed but not handed over yet, nor text handed over but not rendered yet (the
        // reload after connecting could otherwise undo the last typing before a save).
        if (cancelled) return;
        const pendingChanges = () => typingRef.current || (docRef.current !== null && docRef.current.text !== docRef.current.saved);
        if (!force && (dirtyRef.current || pendingChanges())) return;
        // Also checked against the state itself: an update queued before this one wins.
        setState((s) =>
          !force && s.status === "loaded" && s.doc.text !== s.doc.saved
            ? s
            : { status: "loaded", doc: { text: f.text, saved: f.text, etag: f.etag ?? "*", encoding: f.encoding } },
        );
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
    if (!editsText || isDoc || text === undefined) return;
    let cancelled = false;
    setChecking(true);
    validate(path, text)
      .then(
        (v) => !cancelled && setValidation(v),
        () => !cancelled && setValidation(null),
      )
      .finally(() => !cancelled && setChecking(false));
    return () => {
      cancelled = true;
    };
  }, [editsText, isDoc, path, text]);
  const graph = useMemo(() => graphOf(validation), [validation]);
  const steps = graphKind === "pipeline" ? "transforms" : "actions";

  useEffect(() => {
    if (!active) return;
    if (!doc) return onSummary(null);
    onSummary(
      view === "canvas" && graph
        ? `${graph.kind === "pipeline" ? "Pipeline" : "Workflow"} · ${graph.actions?.length ?? 0} ${steps} · ${graph.links?.length ?? 0} links`
        : fileSummary(path, doc.text, doc.encoding) + (view === "yaml" && editsText && cursor ? ` · ${cursor}` : ""),
    );
  }, [active, doc, path, view, graph, steps, onSummary, editsText, cursor]);

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
  const runEdit = useCallback(
    (change: (text: string) => Promise<string>, field?: string, onError?: (message: string) => void): Promise<string | null> => {
      const run = async () => {
        await pending.current?.catch(() => undefined);
        const current = docRef.current;
        if (!current) return null;
        try {
          const next = await change(current.text);
          const merge = field !== undefined && lastField.current === field;
          setText(next, merge ? "merge" : "push");
          lastField.current = field ?? null;
          setEditError(null);
          return next;
        } catch (err) {
          const message = err instanceof Error ? err.message : String(err);
          if (onError) onError(message);
          else setEditError(message);
          return null;
        }
      };
      const request = run();
      pending.current = request;
      return request;
    },
    [setText],
  );
  const applyEdit = useCallback(
    (edit: WorkflowEdit, field?: string) => runEdit((text) => editWorkflow(path, text, edit, graphKind ?? "workflow"), field),
    [runEdit, path, graphKind],
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
        setToast({ text: `Deleted ${linkText}`, undo: true });
        return;
      }
      const what = ids.length === 1 ? ids[0] : `${ids.length} ${steps}`;
      setToast({ text: `Deleted ${what}${removed > 0 ? ` and ${linkText}` : ""}`, undo: true });
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

  // Copy, cut and paste (mockup E16): the clipboard holds the YAML text of the steps.
  const copy = useCallback(
    async (ids: string[], cut: boolean) => {
      if (ids.length === 0) return;
      await pending.current?.catch(() => undefined);
      const current = docRef.current;
      if (!current) return;
      try {
        const clip = await copySteps(path, current.text, ids);
        await navigator.clipboard.writeText(clip.text);
        lastCopy = { text: clip.text, omitted: clip.omitted };
        setCopies((n) => n + 1);
        if (!cut) return;
        if ((await applyEdit({ delete: ids })) === null) return;
        const links = clip.links + clip.omitted;
        setToast({ text: `Cut ${count(clip.steps, steps.slice(0, -1))}${links > 0 ? ` and ${count(links, "link")}` : ""}`, undo: true });
      } catch (err) {
        setToast({ text: cut ? "Could not cut" : "Could not copy", detail: err instanceof Error ? err.message : String(err), status: "error" });
      }
    },
    [path, applyEdit, steps],
  );
  const paste = useCallback(
    async (text: string, at: { x: number; y: number } | null) => {
      if (!graphKind) return;
      const kind = clipKind(text);
      if (kind === null) return setToast({ text: "The clipboard holds no actions or transforms", status: "error" });
      if (kind !== graphKind) {
        const from = /^# Runnel: (\d+ \w+) and .* from (.+)$/m.exec(text);
        return setToast({
          text: `${kind === "workflow" ? "Actions" : "Transforms"} can't be pasted into a ${graphKind}`,
          detail: from ? `the clipboard holds ${from[1]} from ${from[2].split("/").pop()}` : undefined,
          status: "error",
        });
      }
      const result: { pasted?: Pasted } = {};
      const position = at ? { x: Math.round(at.x / 8) * 8, y: Math.round(at.y / 8) * 8 } : undefined;
      const next = await runEdit(
        async (current) => {
          const r = await pasteSteps(path, current, position ? { text, at: position } : { text }, graphKind);
          result.pasted = r.pasted;
          return r.content;
        },
        undefined,
        (message) => setToast({ text: "Could not paste", detail: message, status: "error" }),
      );
      const pasted = result.pasted;
      if (next === null || !pasted) return;
      setSelection({ actions: pasted.ids, links: [] });
      const noun = graphKind === "pipeline" ? "transform" : "action";
      const renamed = Object.entries(pasted.renamed ?? {});
      const detail: string[] = [];
      if (renamed.length > 0) detail.push(`renamed ${renamed.map(([a]) => a).join(", ")} to ${renamed.map(([, b]) => b).join(", ")}`);
      if (lastCopy?.text === text && lastCopy.omitted > 0) detail.push(`${count(lastCopy.omitted, "link")} to other ${noun}s left out`);
      if (pasted.references > 0) detail.push(`${count(pasted.references, graphKind === "pipeline" ? "target" : "expression")} updated`);
      setToast({
        text: `Pasted ${count(pasted.ids.length, noun)}${pasted.links > 0 ? ` and ${count(pasted.links, "link")}` : ""}`,
        detail: detail.join(" · ") || undefined,
        status: "ok",
        undo: true,
      });
    },
    [graphKind, path, runEdit],
  );
  const pasteFromMenu = useCallback(
    (at: { x: number; y: number } | null) =>
      void navigator.clipboard.readText().then(
        (text) => paste(text, at),
        () => setToast({ text: "This browser does not allow reading the clipboard", detail: "paste with Ctrl+V", status: "error" }),
      ),
    [paste],
  );
  const onConnect = useCallback((link: Link) => void applyEdit({ addLinks: [link] }), [applyEdit]);
  // While a rename from the panel is on its way, the canvas briefly has no selection; keep the new id.
  const renamedTo = useRef<string | null>(null);
  const onSelect = useCallback((next: Selection) => {
    if (viewRef.current !== "canvas") return;
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
  const onSetMap = useCallback(
    (id: string, field: string, map: { key: string; value: string }[]) =>
      void applyEdit({ set: [{ id, field, value: null, ...(map.length > 0 ? { map } : {}) }] }, `${id}:${field}`),
    [applyEdit],
  );
  // Lists of the configuration (E14): one step per change of the list.
  const onSetList = useCallback(
    (id: string, field: string, list: KeyValue[][] | null) =>
      void applyEdit({ set: [{ id, field, value: null, ...(list && list.length > 0 ? { list } : {}) }] }, `${id}:${field}`),
    [applyEdit],
  );
  const onSetTexts = useCallback(
    (id: string, field: string, texts: string[] | null) =>
      void applyEdit({ set: [{ id, field, value: null, ...(texts && texts.length > 0 ? { texts } : {}) }] }, `${id}:${field}`),
    [applyEdit],
  );
  // The data preview (S44) runs the text shown, with the values remembered for Run.
  const startPreview = useCallback(
    async (upTo: string, limit?: number) => {
      flushTyping.current?.();
      await pending.current?.catch(() => undefined);
      const current = docRef.current;
      if (!current) return;
      const seq = ++previewSeq.current;
      const lim = limit ?? 100;
      setPreview((p) => ({
        upTo,
        limit: lim,
        result: p?.upTo === upTo ? p.result : null,
        error: null,
        loading: true,
        selected: p?.upTo === upTo ? p.selected : "output",
      }));
      setBottomTab("preview");
      const settings = loadRunSettings(path);
      try {
        const result = await runPreview({ path, content: current.text, transform: upTo, limit: lim, environment: settings?.environment, params: settings?.params });
        if (seq === previewSeq.current) setPreview((p) => p && { ...p, result, loading: false });
      } catch (err) {
        if (seq === previewSeq.current) setPreview((p) => p && { ...p, result: null, error: err instanceof Error ? err.message : String(err), loading: false });
      }
    },
    [path],
  );
  const endPreview = useCallback(() => {
    previewSeq.current++;
    setPreview(null);
    setBottomTab((t) => (t === "preview" ? null : t));
  }, []);
  // A link selected on the canvas shows its records in the preview.
  useEffect(() => {
    const link = selection.links.length === 1 && selection.actions.length === 0 ? selection.links[0] : undefined;
    if (link !== undefined && preview?.result?.links[link]) setPreview((p) => p && { ...p, selected: link });
  }, [selection]); // eslint-disable-line react-hooks/exhaustive-deps

  // "Detect fields" (S46): the selected fields replace "fields:" as one undo step.
  const onWriteFields = useCallback(
    async (id: string, fields: { name: string; from?: string; type: string; format?: string }[]) => {
      const list = fieldsList(fields);
      const next = await applyEdit({ set: [{ id, field: "with.fields", value: null, ...(list.length > 0 ? { list } : {}) }] });
      if (next !== null) setToast({ text: `Wrote ${fields.length} ${fields.length === 1 ? "field" : "fields"} to ${id}`, undo: true });
    },
    [applyEdit],
  );
  // Parameters and outputs (E7c): typing in one field of an entry is one undo step; a deleted entry can be restored.
  const onEntry = useCallback(
    async (e: EntryEdit) => {
      const name = e.op === "delete" ? (e.list === "parameters" ? graph?.parameters : graph?.outputs)?.[e.index ?? 0]?.name : undefined;
      const key = e.op === "set" ? `${e.list}:${e.index}:${e.field}` : undefined;
      const next = await applyEdit({ entries: [e] }, key);
      if (next !== null && e.op === "delete") setToast({ text: `Deleted ${e.list === "parameters" ? "parameter" : "output"} ${name ?? ""}`.trim(), undo: true });
    },
    [applyEdit, graph],
  );
  const onLinkKind = useCallback(
    (index: number, kind: SettableKind) =>
      void applyEdit({ updateLinks: [kind === "data" || kind === "error" || kind === "info" ? { index, type: kind } : { index, when: kind }] }),
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
      flushTyping.current?.();
      await pending.current?.catch(() => undefined);
      const current = docRef.current;
      if (!current) return false;
      if (current.text === current.saved && !overwrite) return true;
      try {
        const etag = await saveFile(path, current.text, overwrite ? "*" : current.etag, current.encoding);
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
  const { run, start: startRun, cancel: cancelRun, show: showHistoryRun, hide: hideRun } = useRun(path);
  const [runDialog, setRunDialog] = useState(false);
  const [unsavedAsk, setUnsavedAsk] = useState<null | { dialog: boolean }>(null);
  const running = run?.status === "running";
  const wfNow = graph;
  // The status belongs to the actions and links it ran with; it is hidden once they change.
  // A run opened from the run history (S26e2) stays; the line above the canvas names the
  // actions that differ, and the record counts of links are left out then (their order may differ).
  const changes = useMemo<RunChanges | null>(() => {
    if (!run?.history || !wfNow) return null;
    const ids = new Set((wfNow.actions ?? []).map((a) => a.id));
    const notInRun = (wfNow.actions ?? []).filter((a) => !run.actions[a.id]).map((a) => a.id);
    const gone = Object.entries(run.actions)
      .filter(([id]) => !ids.has(id))
      .map(([id, a]) => ({ id, status: a.status, durationMs: a.durationMs }));
    return { notInRun, gone };
  }, [run, wfNow]);
  const shownRun =
    run && wfNow && run.history
      ? changes && (changes.notInRun.length > 0 || changes.gone.length > 0)
        ? { ...run, linkRows: {} }
        : run
      : run && wfNow && run.signature === structureSignature(wfNow)
        ? run
        : null;
  // "Open on canvas" in the list of runs: once the graph is there, the stored run is loaded.
  const shownRequest = useRef(0);
  const openHistoryRun = useCallback(
    async (id: string) => {
      if (!graph) return;
      try {
        showHistoryRun(await fetchRun(id), graph);
        setBottomTab("log");
        setEditError(null);
      } catch (err) {
        setEditError(`Could not open the run: ${err instanceof Error ? err.message : String(err)}`);
      }
    },
    [graph, showHistoryRun],
  );
  useEffect(() => {
    const req = props.showRun;
    if (!req || !graph || shownRequest.current === req.seq) return;
    shownRequest.current = req.seq;
    void openHistoryRun(req.id);
  }, [props.showRun, graph, openHistoryRun]);
  useEffect(() => {
    if (active) onRun(run);
  }, [active, run, onRun]);

  const doRun = useCallback(
    async (settings: RunSettings) => {
      const wf = graph;
      if (!wf) return;
      try {
        await startRun(settings, wf);
        // A run ends the data preview: the canvas shows the counts of the run, not those
        // of the preview with its limit of 100 or 1000 records per input.
        endPreview();
        setBottomTab("log"); // the log opens when a run starts and follows it (mockup E8)
        setEditError(null);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setEditError(`Could not start the run: ${msg}`);
        if (err instanceof StartError && err.problems.length > 0) setBottomTab("problems");
      }
    },
    [graph, startRun, endPreview],
  );
  const proceedRun = useCallback(
    (dialog: boolean) => {
      const remembered = loadRunSettings(path);
      const params = graph?.parameters ?? [];
      const envs = props.project?.environments ?? [];
      if (dialog || (!remembered && (params.length > 0 || envs.length > 1))) setRunDialog(true);
      else void doRun(remembered ?? { environment: "", params: {} });
    },
    [path, graph, props.project, doRun],
  );
  const requestRun = useCallback(
    (dialog: boolean) => {
      if (running) return;
      flushTyping.current?.();
      if (docRef.current && docRef.current.text !== docRef.current.saved) setUnsavedAsk({ dialog });
      else proceedRun(dialog);
    },
    [running, proceedRun],
  );

  // Copy, cut and paste work on an editable canvas (E16).
  const canvasEdit = isGraph && view === "canvas" && !!graph;
  // Keyboard shortcuts of the active tab: Ctrl+S, Ctrl+Z, Ctrl+Y / Ctrl+Shift+Z, F5 (run), Ctrl+. (cancel), Ctrl+C / Ctrl+X on the canvas.
  useEffect(() => {
    if (!active || !editsText) return;
    const onKey = (e: KeyboardEvent) => {
      if (isGraph && e.key === "F5" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        e.preventDefault(); // instead of reloading the page
        requestRun(e.shiftKey);
        return;
      }
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
      if (isGraph && e.key === ".") {
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
      } else if ((k === "c" || k === "x") && canvasEdit && selection.actions.length > 0 && !window.getSelection()?.toString()) {
        e.preventDefault();
        void copy(selection.actions, k === "x");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, isGraph, editsText, save, doUndo, doRedo, requestRun, cancelRun, canvasEdit, selection, copy]);

  // Ctrl+V on the canvas pastes at the mouse (E16); in text fields it pastes text as usual.
  useEffect(() => {
    if (!active || !canvasEdit) return;
    const onPaste = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return;
      e.preventDefault();
      void paste(e.clipboardData?.getData("text/plain") ?? "", pointer.current);
    };
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
  }, [active, canvasEdit, paste]);

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

  const showInYAML = useCallback((line: number, from?: number, to?: number) => {
    setView("yaml");
    setFocusLine(line);
    setYamlFocus((f) => ({ line, seq: (f?.seq ?? 0) + 1, from, to }));
  }, []);

  // The reference (E18b): which file it helps with, and its requests to this file.
  const { onReference } = props;
  const refKind =
    fileKind === "project" || fileKind === "environment" || fileKind === "trigger" || fileKind === "connection" ? fileKind : null;
  // Suggestions and hover in the YAML editor (E19): the schema of the file kind or the catalog.
  const refSchema = useSchema(refKind);
  const catalog = useCatalog(graphKind ?? "workflow");
  const environments = props.project?.environments;
  const assist = useMemo<AssistContext>(
    () => ({
      schema: refKind ? refSchema : null,
      catalog: graphKind ? catalog : new Map(),
      steps: graphKind ? (graphKind === "pipeline" ? "transforms" : "actions") : undefined,
      environments: environments ?? [],
      trigger: refKind === "trigger",
    }),
    [refKind, refSchema, graphKind, catalog, environments],
  );
  useEffect(() => {
    if (active) onReference?.(refKind && doc ? { path, kind: refKind, text: doc.text } : null);
  }, [active, refKind, doc, path, onReference]);
  const done = useRef(0);
  useEffect(() => {
    const req = props.request;
    if (!req || req.seq === done.current || !docRef.current) return;
    done.current = req.seq;
    const a = req.action;
    if (a.op === "goto") return showInYAML(a.line);
    flushTyping.current?.();
    const text = docRef.current.text;
    const ins =
      a.op === "insert" ? insertField(text, a.parent, a.key, a.schema) : insertField(text, "variables", a.name, { type: "string" });
    setText(ins.text, "push");
    showInYAML(ins.line, ins.from, ins.to);
  }, [props.request, doc, setText, showInYAML]);

  // Switching keeps the element (focus fix): the YAML view opens at the selected step or
  // link, the canvas selects the step or link the cursor of the YAML view is in.
  const switchView = useCallback(
    (next: View) => {
      if (next === view) return;
      const text = doc?.text ?? "";
      const steps = graphKind === "pipeline" ? "transforms" : "actions";
      if (next === "yaml") {
        cursorLine.current = null;
        const action = selection.actions.length === 1 && selection.links.length === 0 ? selection.actions[0] : null;
        const link = selection.links.length === 1 && selection.actions.length === 0 ? selection.links[0] : null;
        const item = action ? listItems(text, steps).find((i) => i.id === action) : link !== null ? listItems(text, "links")[link] : undefined;
        if (item) return showInYAML(item.from);
        setView("yaml");
        return;
      }
      const line = cursorLine.current;
      setView(next);
      if (line === null || !graph) return;
      const at = (items: Item[]) => items.findIndex((i) => line >= i.from && line <= i.to);
      const step = listItems(text, steps)[at(listItems(text, steps))];
      const linkIndex = at(listItems(text, "links"));
      if (step?.id && graph.actions?.some((a) => a.id === step.id)) setSelection({ actions: [step.id], links: [] });
      else if (linkIndex >= 0 && graph.links && linkIndex < graph.links.length) setSelection({ actions: [], links: [linkIndex] });
    },
    [view, doc, graphKind, selection, graph, showInYAML],
  );

  // A click on a problem selects what it belongs to and shows the field in the properties panel.
  const openProblem = useCallback(
    (p: Problem) => {
      // In the YAML view a problem shows its line (E9).
      if (view === "yaml") {
        if (p.line) showInYAML(p.line);
        return;
      }
      const target = graph ? problemTarget(graph, p) : null;
      if (!target) {
        if (p.line) showInYAML(p.line);
        return;
      }
      setSelection({ actions: target.action ? [target.action] : [], links: target.link !== undefined ? [target.link] : [] });
      setPanel(true);
      if (target.field) setFocusField((f) => ({ key: target.field!, seq: (f?.seq ?? 0) + 1 }));
    },
    [graph, view, showInYAML],
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
  const openProblems = editsText ? () => setBottomTab("problems") : undefined;
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
        {!editsText && (
          <Badge title="This file can be changed in your text editor">
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
        {props.variableWarning && props.variableWarning.count > 0 && (
          <Badge testId="variable-warning" tone="warning" title={props.variableWarning.text}>
            ⚠ {props.variableWarning.count}
          </Badge>
        )}
        {/* After the problem badges, so that they stay in place when switching views. */}
        {editsText && view === "yaml" && (typing || checking) && (
          <span data-testid="checking" className="inline-flex items-center gap-1.5 text-2xs whitespace-nowrap text-muted-foreground">
            <span className="size-[9px] animate-spin rounded-full border-2 border-current border-r-transparent motion-reduce:animate-none" />
            Checking…
          </span>
        )}
        <span className="flex-1" />
        {isGraph &&
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
        {isConnection && <TestButton name={path.split("/").pop()!.replace(/\.yaml$/, "")} onResult={setTestResult} />}
        {isPlugin && pluginInfo && (
          <PluginActions
            info={pluginInfo}
            canTrust={plugins.info?.canTrust ?? true}
            onTrust={() => setTrusting("dialog")}
            onUntrust={() => void pluginAction(() => untrustPlugin(pluginInfo.name), "The approval was not removed")}
            onRetry={() => void pluginAction(() => reloadPlugin(pluginInfo.name), "The plugin was not checked again")}
          />
        )}
        {isTrigger && (
          <button
            type="button"
            title={dirty ? "Starts the target of the saved trigger, with its environment and parameters" : "Starts the target with the environment and parameters of the trigger"}
            onClick={() =>
              void runTriggerTarget(path).then(
                (r) => setToast({ text: `Started a run of ${r.target.split("/").pop()}`, detail: "open the target to follow it", status: "ok" }),
                (e: Error) => setToast({ text: "Could not start the target", detail: e.message, status: "error" }),
              )
            }
            className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
          >
            <span className="text-status-success">
              <PlayIcon width={14} height={14} />
            </span>
            Run target now
          </button>
        )}
        {editsText && (
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
          </>
        )}
        {isDoc && <ViewSwitch view={view} onView={switchView} document />}
        {isForm && <ViewSwitch view={view} onView={switchView} form overview={isPlugin} />}
        {isGraph && (
          <>
            <ViewSwitch view={view} onView={switchView} pipeline={graphKind === "pipeline"} notAGraph={!!validation && !graph} />
            {/* Always there, so that nothing moves: pressed while the panel is shown. */}
            <IconButton
              title={
                editable && view === "canvas"
                  ? panel
                    ? "Hide properties panel"
                    : "Show properties panel"
                  : "The properties panel belongs to the canvas"
              }
              label="Properties panel"
              pressed={panel && editable && view === "canvas"}
              disabled={!editable || view !== "canvas"}
              onClick={() => setPanel((p) => !p)}
            >
              <PanelIcon width={14} height={14} />
            </IconButton>
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
      {isGraph && run?.history && (
        <RunInfoBar
          run={run}
          path={path}
          changes={changes}
          onOpenRun={(id) => void openHistoryRun(id)}
          onRunsOf={() => props.onRunsOf?.(path)}
          onHide={hideRun}
        />
      )}
      <div className="flex min-h-0 flex-1">
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {!doc ? (
          <div className="flex-1" aria-busy="true" />
        ) : view === "canvas" && isConnection ? (
          <ConnectionForm
            path={path}
            text={doc.text}
            problems={validation?.problems ?? []}
            result={testResult}
            onText={(next) => setText(next, "push")}
            onError={setEditError}
          />
        ) : view === "canvas" && isPlugin ? (
          <PluginView
            info={pluginInfo}
            error={plugins.error}
            serviceApprovals={plugins.info?.serviceApprovals ?? false}
            canTrust={plugins.info?.canTrust ?? true}
            onTrust={() => setTrusting("dialog")}
            onRetry={() => pluginInfo && void pluginAction(() => reloadPlugin(pluginInfo.name), "The plugin was not checked again")}
            onOpenFile={(p) => props.onOpenFile?.(p)}
          />
        ) : view === "canvas" && isTrigger ? (
          <TriggerForm
            path={path}
            text={doc.text}
            saved={!dirty}
            problems={validation?.problems ?? []}
            files={props.files ?? noFiles}
            onText={(next) => setText(next, "push")}
            onError={setEditError}
            onOpenFile={props.onOpenFile}
          />
        ) : view === "canvas" && settingsKind ? (
          <SettingsForm
            kind={settingsKind}
            path={path}
            text={doc.text}
            saved={!dirty}
            projectDir={props.projectDir}
            problems={validation?.problems ?? []}
            onText={(next) => setText(next, "push")}
            onError={setEditError}
          />
        ) : view === "canvas" && isDoc ? (
          <MarkdownView text={doc.text} path={path} files={props.files ?? noFiles} onOpen={(p) => props.onOpenFile?.(p)} />
        ) : view === "yaml" && editsText ? (
          <YamlEditor
            text={doc.text}
            markdown={isDoc}
            problems={validation?.problems ?? []}
            focus={yamlFocus}
            assist={isDoc ? undefined : assist}
            onChange={(next) => setText(next, "push")}
            onPending={onPending}
            onUndo={doUndo}
            onRedo={doRedo}
            registerFlush={(f) => (flushTyping.current = f)}
            onCursor={(line, column) => {
              cursorLine.current = line;
              setCursor(`Ln ${line}, Col ${column}`);
            }}
          />
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
            onClipboard={editable ? (op, ids) => void copy(ids, op === "cut") : undefined}
            onPaste={editable ? pasteFromMenu : undefined}
            canPaste={copies >= 0 && lastCopy !== null}
            pointer={pointer}
            renaming={renaming}
            onRenaming={setRenaming}
            onSelect={onSelect}
            selection={selection}
            problemsInPanel={showPanel}
            run={preview?.result && graph ? previewRun(preview.result, graph, preview.upTo) : shownRun}
            preview={preview ? { upTo: preview.upTo, limit: preview.limit, onClose: endPreview } : null}
          />
        ) : validation ? (
          <NotAGraph path={path} kind={graphKind ?? "workflow"} validation={validation} onShowYAML={showInYAML} />
        ) : (
          <div className="runnel-canvas flex-1" aria-busy="true" />
        )}
        {editsText && doc && (graph || validation) && (
          <BottomPanel
            workflow={graph ?? null}
            pipeline={graphKind === "pipeline"}
            log={isGraph}
            preview={
              preview && graphKind === "pipeline"
                ? {
                    failed: !!preview.error || (!!preview.result && preview.result.status !== "succeeded"),
                    onClose: endPreview,
                    content: (
                      <PreviewPanel
                        upTo={preview.upTo}
                        result={preview.result}
                        loading={preview.loading}
                        error={preview.error}
                        links={graph?.links ?? []}
                        selected={preview.selected}
                        onSelect={(selected) => setPreview((p) => p && { ...p, selected })}
                        limit={preview.limit}
                        onLimit={(limit) => void startPreview(preview.upTo, limit)}
                        onRefresh={() => void startPreview(preview.upTo, preview.limit)}
                      />
                    ),
                  }
                : null
            }
            problems={validation?.problems ?? []}
            tab={bottomTab}
            onTab={setBottomTab}
            onOpen={openProblem}
            onOpenPlugin={(name) => props.onOpenFile?.(pluginFile(name))}
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
            className={
              "absolute bottom-5 left-1/2 inline-flex min-h-9 w-max max-w-[min(560px,calc(100%-2rem))] -translate-x-1/2 items-center gap-3 rounded-lg border border-border bg-card py-1.5 pl-3.5 shadow-md " +
              (toast.undo ? "pr-1.5" : "pr-3.5")
            }
          >
            {toast.status && (
              <span
                aria-hidden
                className={"grid size-[18px] shrink-0 place-items-center rounded-full text-[10px] font-bold " + (toast.status === "ok" ? "bg-success" : "bg-destructive")}
                style={{ color: "var(--rn-tiefe-0)" }}
              >
                {toast.status === "ok" ? "✔" : "✖"}
              </span>
            )}
            <span className="flex-1">
              {toast.text}
              {toast.detail && <span className="text-xs text-muted-foreground"> · {toast.detail}</span>}
            </span>
            {toast.undo && (
              <SmallButton
                onClick={() => {
                  doUndo();
                  setToast(null);
                }}
              >
                <UndoIcon width={14} height={14} />
                Undo
              </SmallButton>
            )}
          </div>
        )}
      </div>
      {showPanel && graph && (
        <PropertiesPanel
          workflow={graph}
          onOpenPlugin={(name) => props.onOpenFile?.(pluginFile(name))}
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
          onSetMap={onSetMap}
          onSetList={onSetList}
          onShowInYAML={(id) => {
            const item = listItems(doc?.text ?? "", graphKind === "pipeline" ? "transforms" : "actions").find((i) => i.id === id);
            if (item) showInYAML(item.from);
          }}
          onSetTexts={onSetTexts}
          onEntry={(e) => void onEntry(e)}
          onLinkChange={onLinkChange}
          onDeleteLink={(index) => void onDelete([], [index])}
          onRename={(from, to) => {
            renamedTo.current = to;
            setSelection({ actions: [to], links: [] });
            onRename(from, to);
          }}
          onHide={() => setPanel(false)}
          onDetectFields={graphKind === "pipeline" ? setDetecting : undefined}
          onPreview={graphKind === "pipeline" ? (id) => void startPreview(id, preview?.limit) : undefined}
        />
      )}
      </div>
      {detecting && doc && (
        <DetectFieldsDialog
          path={path}
          content={doc.text}
          transform={detecting}
          rawPath={rawPathOf(graph?.actions?.find((a) => a.id === detecting)?.with?.path)}
          onCancel={() => setDetecting(null)}
          onWrite={(fields) => {
            const id = detecting;
            setDetecting(null);
            void onWriteFields(id, fields);
          }}
        />
      )}
      {runDialog && (
        <RunDialog
          path={path}
          project={props.project}
          parameters={graph?.parameters ?? []}
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
      {trusting && pluginInfo && (
        <TrustDialog p={pluginInfo} projectDir={props.projectDir ?? undefined} busy={trusting === "busy"} onTrust={() => void trust()} onCancel={() => setTrusting(null)} />
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

const noFiles = new Set<string>();

function ViewSwitch(props: { view: View; onView: (v: View) => void; pipeline?: boolean; notAGraph?: boolean; document?: boolean; form?: boolean; overview?: boolean }) {
  const views: [View, string, typeof CodeIcon][] = props.document
    ? [
        ["canvas", "Preview", EyeIcon],
        ["yaml", "Text", LinesIcon],
      ]
    : props.form
      ? [
          ["canvas", props.overview ? "Overview" : "Form", FormIcon],
          ["yaml", "YAML", CodeIcon],
        ]
      : [
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
          title={v === "canvas" && props.notAGraph ? "The file has errors: the canvas shows where they are" : undefined}
          onClick={() => props.onView(v)}
          className="inline-flex cursor-pointer items-center gap-1.5 rounded-sm px-2 py-[3px] text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm"
        >
          <Icon width={14} height={14} />
          {label}
          {v === "canvas" && props.notAGraph && <span data-testid="not-a-graph" className="size-1.5 rounded-full bg-destructive" />}
        </button>
      ))}
    </div>
  );
}

/** Shown instead of the canvas when the file cannot be read as a workflow. */
function NotAGraph(props: { path: string; kind: GraphKind; validation: Validation; onShowYAML: (line: number) => void }) {
  const first = props.validation.problems.find((p) => p.severity === "error");
  const line = first?.line ?? 1;
  return (
    <div className="runnel-canvas grid flex-1 place-items-center p-8" role="alert">
      <div className="max-w-[440px] rounded-xl border border-border bg-card px-7 py-6 text-center shadow-md">
        <span className="inline-grid size-10 place-items-center rounded-full bg-danger-soft text-lg font-bold text-destructive">
          ✖
        </span>
        <h2 className="mt-3 mb-1 text-md font-semibold">This {props.kind} can’t be shown as a graph</h2>
        <p className="mt-0 mb-3 text-muted-foreground">The file has errors. Fix them in the YAML view; your changes are kept.</p>
        {first && (
          <pre className="mt-0 mb-4 rounded-md bg-muted px-2.5 py-2 text-left text-xs whitespace-pre-wrap">
            {props.path}
            {first.line ? `:${first.line}:${first.column ?? 1}` : ""}
            {"\n"}
            {first.message}
          </pre>
        )}
        <SmallButton onClick={() => props.onShowYAML(line)}>
          <CodeIcon width={14} height={14} />
          Show YAML at line {line}
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

function IconButton(props: {
  children: React.ReactNode;
  title: string;
  /** Accessible name if it differs from the title, e.g. for a toggle whose title changes. */
  label?: string;
  /** Makes the button a toggle (aria-pressed). */
  pressed?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      title={props.title}
      aria-label={props.label ?? props.title.replace(/ \(.*\)$/, "")}
      aria-pressed={props.pressed}
      disabled={props.disabled}
      onClick={props.onClick}
      className="grid size-7 cursor-pointer place-items-center rounded-md border border-input bg-background text-foreground hover:bg-muted aria-pressed:border-ring aria-pressed:bg-accent aria-pressed:text-accent-foreground disabled:cursor-default disabled:text-muted-foreground disabled:opacity-50 disabled:hover:bg-background"
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
