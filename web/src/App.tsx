import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import mark from "../../design/logo/runnel-mark.svg";
import { fetchProject, fetchVersion, useConnection, type ProjectInfo } from "./api";
import { Explorer } from "./components/Explorer";
import { Dialog } from "./components/Dialog";
import { ErrorBoundary, FileError, PanelError } from "./components/ErrorBoundary";
import { FileView, type FileHandle } from "./components/FileView";
import { Header } from "./components/Header";
import { RunsView } from "./components/RunsView";
import { ImageView } from "./components/ImageView";
import { Palette, type PaletteTarget } from "./components/Palette";
import { ReferencePanel, type ReferenceAction, type ReferenceTarget } from "./components/ReferencePanel";
import { missingIn, useVariables } from "./reference";
import { Splitter, useExplorerHeight } from "./components/Splitter";
import { StatusBar } from "./components/StatusBar";
import { Tabs } from "./components/Tabs";
import { fetchTree, fullPath, kindOf, movedPath, type Entry } from "./files";
import type { RunView } from "./run";
import { runsTab, useRunningCount } from "./runs";
import { useTriggers } from "./forms";
import { useTheme } from "./theme";

// Editor shell after the approved mockups E4–E6b (design/mockups/): header, explorer and
// palette, tabs with file views (workflows editable on the canvas), status bar.
// openFirst is the file to open at the start instead of the README.md (a new project, R10).
export function App(props: { openFirst?: string }) {
  const [theme, setTheme] = useTheme();
  const { state, epoch, runsTick } = useConnection();
  // The list of runs (S26e1) follows runs that start and end; after a reconnect it loads again.
  const runsKey = epoch * 1_000_000 + runsTick;
  const running = useRunningCount(runsKey);
  const [project, setProject] = useState<ProjectInfo | null>(null);
  const [version, setVersion] = useState<string | null>(null);
  const [tree, setTree] = useState<Entry | null>(null);
  const [treeError, setTreeError] = useState<string | null>(null);
  const [tabs, setTabs] = useState<string[]>([]);
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;
  const [active, setActive] = useState<string | null>(null);
  const [summary, setSummary] = useState<string | null>(null);
  const [paletteTarget, setPaletteTarget] = useState<PaletteTarget | null>(null);
  // The lower part of the sidebar follows the open file (mockup E18): the palette for
  // workflows and pipelines, the reference for project.yaml, environments and triggers.
  const [reference, setReference] = useState<ReferenceTarget | null>(null);
  const [request, setRequest] = useState<{ path: string; seq: number; action: ReferenceAction } | null>(null);
  // The variables of the project (E18a), loaded again after every save and file change.
  const [varsKey, setVarsKey] = useState(0);
  const variables = useVariables(varsKey);
  const [run, setRun] = useState<RunView | null>(null);
  const triggers = useTriggers(`${epoch}:${varsKey}`);
  // A run of the history to open on the canvas, and a filter for the list of runs (S26e2).
  const [showRun, setShowRun] = useState<{ path: string; id: string; seq: number } | null>(null);
  const [runsRequest, setRunsRequest] = useState<{ filter: { path?: string }; seq: number } | null>(null);
  const [dirty, setDirty] = useState<Set<string>>(new Set());
  // Files with unsaved changes waiting for "Save changes?", one after the other (E21).
  const [closingQueue, setClosingQueue] = useState<string[]>([]);
  const closing = closingQueue[0] ?? null;
  const nextClosing = () => setClosingQueue((q) => q.slice(1));
  // "Reveal in explorer" (E21): the explorer opens the folders of the file and shows its row.
  const [reveal, setReveal] = useState<{ path: string; seq: number } | null>(null);
  // Files changed outside their tab (references updated when moving, E12) load again.
  const [reloads, setReloads] = useState<Record<string, number>>({});
  const handles = useRef(new Map<string, FileHandle>());
  // The height of the explorer can be changed; the palette takes the rest.
  const [explorerHeight, setExplorerHeight] = useExplorerHeight();
  const sidebar = useRef<HTMLElement>(null);
  const explorer = useRef<HTMLDivElement>(null);

  // The project (its environments, the default environment) is loaded again whenever the
  // files of the project are: after creating, renaming or deleting files in the explorer and
  // after saving project.yaml or an environment. Otherwise the run dialog keeps the
  // environments of the start.
  const loadProject = useCallback(() => {
    fetchProject().then(setProject, () => setProject(null));
    setVarsKey((k) => k + 1);
  }, []);
  const onDirty = useCallback((path: string, isDirty: boolean) => {
    if (!isDirty && (path === "project.yaml" || path.startsWith("environments/"))) loadProject();
    else if (!isDirty) setVarsKey((k) => k + 1);
    setDirty((d) => {
      if (d.has(path) === isDirty) return d;
      const next = new Set(d);
      if (isDirty) next.add(path);
      else next.delete(path);
      return next;
    });
  }, [loadProject]);
  const onRegister = useCallback((path: string, handle: FileHandle | null) => {
    if (handle) handles.current.set(path, handle);
    else handles.current.delete(path);
  }, []);

  // The browser asks before leaving the page while changes are unsaved.
  useEffect(() => {
    if (dirty.size === 0) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  const loadTree = useCallback(() => {
    loadProject();
    fetchTree().then(
      (t) => {
        setTree(t);
        setTreeError(null);
      },
      (err: unknown) => setTreeError(`Could not load the project files: ${err instanceof Error ? err.message : err}`),
    );
  }, [loadProject]);

  // Load at start and again on every reconnect: the server may have been restarted and
  // files may have changed. Loading does not wait for the event stream.
  useEffect(() => {
    fetchVersion().then((v) => setVersion(v.version), () => setVersion(null));
    loadTree();
  }, [epoch, loadTree]);

  const open = useCallback((path: string) => {
    setTabs((t) => (t.includes(path) ? t : [...t, path]));
    setActive(path);
  }, []);
  // Requests of the reference go to their file, which opens if needed.
  const onReferenceAction = useCallback(
    (action: ReferenceAction) => {
      open(action.path);
      setRequest((r) => ({ path: action.path, seq: (r?.seq ?? 0) + 1, action }));
    },
    [open],
  );
  // Variables used but not set: a badge in the file bar of environments and project.yaml.
  const variableWarning = (path: string) => {
    if (!variables) return null;
    const envs = path === "project.yaml" ? (variables.environments ?? []) : path.startsWith("environments/") ? [path.slice(13).replace(/\.ya?ml$/, "")] : [];
    const missing = variables.variables.filter((v) => envs.some((e) => missingIn(v, e)));
    if (missing.length === 0) return null;
    const text = missing
      .map((v) => `⚠ ${v.name} is used, but not set for ${envs.filter((e) => missingIn(v, e)).join(", ")} (${v.uses.filter((u) => !u.parameter).map((u) => `${u.file}:${u.line}`).join(", ")})`)
      .join("\n");
    return { count: missing.length, text };
  };
  // Without an open file the palette stays, with the hint to open a workflow or pipeline.
  const onRuns = active === runsTab;
  const sidePanel = !active || onRuns || paletteTarget ? "palette" : reference ? "reference" : null;
  // The files of the project, for the links of documents (E17).
  const files = useMemo(() => {
    const out = new Set<string>();
    const walk = (e: Entry) => {
      if (e.type === "file") out.add(e.path);
      e.children?.forEach(walk);
    };
    if (tree) walk(tree);
    return out;
  }, [tree]);
  const runFiles = useMemo(() => [...files].filter((p) => p.endsWith(".workflow.yaml") || p.endsWith(".pipeline.yaml")).sort(), [files]);
  // Without an open file, the editor starts with the README.md of the project (E17).
  const started = useRef(false);
  useEffect(() => {
    if (!tree || started.current) return;
    started.current = true;
    const readme = tree.children?.find((e) => e.type === "file" && e.name.toLowerCase() === "readme.md");
    const first = props.openFirst ?? readme?.path;
    if (first && tabs.length === 0) open(first);
  }, [tree, tabs.length, open, props.openFirst]);
  // Closes tabs at once; the active one passes to its next neighbour that stays open.
  const closeNow = (paths: string[]) => {
    if (paths.length === 0) return;
    // The latest tabs, also after a save that finished later (Save in "Save changes?").
    const t = tabsRef.current;
    const gone = new Set(paths);
    setTabs((cur) => cur.filter((p) => !gone.has(p)));
    setActive((a) => {
      if (!a || !gone.has(a)) return a;
      const i = t.indexOf(a);
      return t.slice(i + 1).find((p) => !gone.has(p)) ?? t.slice(0, i).reverse().find((p) => !gone.has(p)) ?? null;
    });
    paths.forEach((p) => onDirty(p, false));
  };
  // Closing a tab with unsaved changes asks first (decision for mockup E6a); with several
  // files, the saved ones close at once and the others ask one after the other (E21).
  const closeMany = (paths: string[]) => {
    closeNow(paths.filter((p) => !dirty.has(p)));
    const ask = paths.filter((p) => dirty.has(p));
    if (ask.length > 0) setClosingQueue((q) => [...q, ...ask.filter((p) => !q.includes(p))]);
  };
  const close = (path: string) => closeMany([path]);

  return (
    <div className="grid h-full grid-rows-[48px_1fr_24px]">
      <Header project={project} theme={theme} onTheme={setTheme} running={running} runsOpen={onRuns} onRuns={() => open(runsTab)} />
      <div className="grid min-h-0 grid-cols-[280px_1fr]">
        <aside ref={sidebar} className="flex min-h-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground">
          <div
            ref={explorer}
            data-testid="explorer-pane"
            className="flex min-h-[120px] flex-col"
            style={sidePanel ? { flex: "0 1 auto", height: explorerHeight ?? undefined } : { flex: "1 1 auto" }}
          >
            <Explorer
              tree={tree}
              error={treeError}
              selected={onRuns ? null : active}
              onOpen={open}
              onReload={loadTree}
              reveal={reveal}
              changes={{
                projectDir: project?.dir ?? null,
                isDirty: (path) => dirty.has(path),
                onCreated: open,
                // Tabs of a renamed file or of the files of a renamed folder follow (E11).
                onRenamed: (from, to) => {
                  const moved = (p: string) => movedPath(p, from, to);
                  setTabs((t) => t.map(moved));
                  setActive((a) => (a ? moved(a) : a));
                },
                onChanged: (paths) => setReloads((r) => Object.fromEntries([...Object.entries(r), ...paths.map((p) => [p, (r[p] ?? 0) + 1])])),
                onDeleted: (path) => {
                  const gone = tabs.filter((p) => p === path || p.startsWith(path + "/"));
                  const rest = tabs.filter((p) => !gone.includes(p));
                  gone.forEach((p) => onDirty(p, false));
                  setTabs(rest);
                  if (active && gone.includes(active)) setActive(rest[0] ?? null);
                },
              }}
            />
          </div>
          {sidePanel && <Splitter container={sidebar} explorer={explorer} height={explorerHeight} onHeight={setExplorerHeight} />}
          {sidePanel && (
            <ErrorBoundary key={sidePanel} fallback={(error, retry) => <PanelError error={error} onRetry={retry} />}>
              {sidePanel === "palette" && <Palette target={active && !onRuns ? paletteTarget : null} />}
              {sidePanel === "reference" && reference && <ReferencePanel target={reference} variables={variables} onAction={onReferenceAction} />}
            </ErrorBoundary>
          )}
        </aside>
        <main className="flex min-h-0 min-w-0 flex-col bg-background">
          {state === "disconnected" && (
            <div
              role="alert"
              className="flex shrink-0 items-center gap-3 border-b border-border bg-warning-soft px-4 py-2 text-warning"
            >
              <strong>⚠ Connection to the Runnel server lost.</strong>
              <span className="text-muted-foreground">
                Reconnecting… Is <code>runnel serve</code> still running?
              </span>
            </div>
          )}
          {active ? (
            <>
              <Tabs
                paths={tabs}
                active={active}
                dirty={dirty}
                onSelect={setActive}
                onClose={close}
                onCloseMany={closeMany}
                onCopyPath={(path) => void navigator.clipboard?.writeText(project?.dir ? fullPath(project.dir, path) : path)}
                onReveal={(path) => setReveal((r) => ({ path, seq: (r?.seq ?? 0) + 1 }))}
              />
              {tabs.includes(runsTab) && (
                <div className={onRuns ? "flex min-h-0 flex-1" : "hidden"}>
                  <ErrorBoundary fallback={(error, retry) => <PanelError error={error} onRetry={retry} />}>
                    <RunsView
                      active={onRuns}
                      tick={runsKey}
                      files={runFiles}
                      triggers={triggers?.triggers ?? []}
                      keepDays={project?.history?.keepDays}
                      maxRuns={project?.history?.maxRuns}
                      onOpenFile={open}
                      onOpenRun={(path, id) => {
                        open(path);
                        setShowRun((r) => ({ path, id, seq: (r?.seq ?? 0) + 1 }));
                      }}
                      request={runsRequest}
                      onSummary={setSummary}
                    />
                  </ErrorBoundary>
                </div>
              )}
              {tabs.filter((path) => path !== runsTab).map((path) => (
                // An error in a file stays in its tab; the message shows while the tab is active.
                <ErrorBoundary
                  key={`${path}#${reloads[path] ?? 0}`}
                  fallback={(error, retry) => (path === active ? <FileError path={path} error={error} onRetry={retry} /> : null)}
                >
                  {kindOf(path) === "image" ? (
                    <ImageView
                      path={path}
                      active={path === active}
                      epoch={epoch}
                      onSummary={setSummary}
                      onRun={setRun}
                      onPalette={setPaletteTarget}
                      onReference={setReference}
                      onReloadExplorer={loadTree}
                    />
                  ) : (
                    <FileView
                      path={path}
                      active={path === active}
                      projectDir={project?.dir ?? null}
                      project={project}
                      onRun={setRun}
                      epoch={epoch}
                      onSummary={setSummary}
                      onPalette={setPaletteTarget}
                      onReference={setReference}
                      request={request?.path === path ? request : null}
                      variableWarning={variableWarning(path)}
                      onDirty={onDirty}
                      onRegister={onRegister}
                      onReloadExplorer={loadTree}
                      files={files}
                      onOpenFile={open}
                      showRun={showRun?.path === path ? showRun : null}
                      onRunsOf={(p) => {
                        open(runsTab);
                        setRunsRequest((r) => ({ filter: { path: p }, seq: (r?.seq ?? 0) + 1 }));
                      }}
                    />
                  )}
                </ErrorBoundary>
              ))}
            </>
          ) : (
            <div className="grid flex-1 place-items-center p-8">
              <div className="max-w-[420px] text-center">
                <img src={mark} alt="" width={48} height={48} className="mx-auto" />
                <h2 className="mt-4 mb-1 text-md font-semibold">No file open</h2>
                <p className="m-0 text-muted-foreground">Select a workflow or another file in the explorer to view it.</p>
              </div>
            </div>
          )}
        </main>
      </div>
      <StatusBar
        connection={state}
        version={version}
        summary={active ? summary : null}
        dirty={active !== null && dirty.has(active)}
        run={active && !onRuns ? run : null}
      />
      {closing && (
        <Dialog
          title="Save changes?"
          onCancel={() => setClosingQueue([])}
          actions={[
            // Cancel keeps this file and the ones still waiting open.
            { label: "Cancel", onClick: () => setClosingQueue([]) },
            {
              label: "Don’t save",
              onClick: () => {
                closeNow([closing]);
                nextClosing();
              },
            },
            {
              label: "Save",
              tone: "primary",
              onClick: async () => {
                const path = closing;
                nextClosing();
                if (await handles.current.get(path)?.save()) closeNow([path]);
              },
            },
          ]}
        >
          <p>
            <code className="text-xs">{closing}</code> has unsaved changes. They are lost if you close the file without
            saving.
          </p>
        </Dialog>
      )}
    </div>
  );
}
