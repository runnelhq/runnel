import { useCallback, useEffect, useRef, useState } from "react";
import mark from "../../design/logo/runnel-mark.svg";
import { fetchProject, fetchVersion, useConnection, type ProjectInfo } from "./api";
import { Explorer } from "./components/Explorer";
import { Dialog } from "./components/Dialog";
import { FileView, type FileHandle } from "./components/FileView";
import { Header } from "./components/Header";
import { Palette, type PaletteTarget } from "./components/Palette";
import { Splitter, useExplorerHeight } from "./components/Splitter";
import { StatusBar } from "./components/StatusBar";
import { Tabs } from "./components/Tabs";
import { fetchTree, type Entry } from "./files";
import type { RunView } from "./run";
import { useTheme } from "./theme";

// Editor shell after the approved mockups E4–E6b (design/mockups/): header, explorer and
// palette, tabs with file views (workflows editable on the canvas), status bar.
export function App() {
  const [theme, setTheme] = useTheme();
  const { state, epoch } = useConnection();
  const [project, setProject] = useState<ProjectInfo | null>(null);
  const [version, setVersion] = useState<string | null>(null);
  const [tree, setTree] = useState<Entry | null>(null);
  const [treeError, setTreeError] = useState<string | null>(null);
  const [tabs, setTabs] = useState<string[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [summary, setSummary] = useState<string | null>(null);
  const [paletteTarget, setPaletteTarget] = useState<PaletteTarget | null>(null);
  const [run, setRun] = useState<RunView | null>(null);
  const [dirty, setDirty] = useState<Set<string>>(new Set());
  const [closing, setClosing] = useState<string | null>(null);
  const handles = useRef(new Map<string, FileHandle>());
  // The height of the explorer can be changed; the palette takes the rest.
  const [explorerHeight, setExplorerHeight] = useExplorerHeight();
  const sidebar = useRef<HTMLElement>(null);
  const explorer = useRef<HTMLDivElement>(null);

  const onDirty = useCallback((path: string, isDirty: boolean) => {
    setDirty((d) => {
      if (d.has(path) === isDirty) return d;
      const next = new Set(d);
      if (isDirty) next.add(path);
      else next.delete(path);
      return next;
    });
  }, []);
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
    fetchTree().then(
      (t) => {
        setTree(t);
        setTreeError(null);
      },
      (err: unknown) => setTreeError(`Could not load the project files: ${err instanceof Error ? err.message : err}`),
    );
  }, []);

  // Load at start and again on every reconnect: the server may have been restarted and
  // files may have changed. Loading does not wait for the event stream.
  useEffect(() => {
    fetchProject().then(setProject, () => setProject(null));
    fetchVersion().then((v) => setVersion(v.version), () => setVersion(null));
    loadTree();
  }, [epoch, loadTree]);

  const open = (path: string) => {
    setTabs((t) => (t.includes(path) ? t : [...t, path]));
    setActive(path);
  };
  const closeNow = (path: string) => {
    const i = tabs.indexOf(path);
    const rest = tabs.filter((p) => p !== path);
    setTabs(rest);
    onDirty(path, false);
    if (active === path) setActive(rest[Math.min(i, rest.length - 1)] ?? null);
  };
  // Closing a tab with unsaved changes asks first (decision for mockup E6a).
  const close = (path: string) => (dirty.has(path) ? setClosing(path) : closeNow(path));

  return (
    <div className="grid h-full grid-rows-[48px_1fr_24px]">
      <Header project={project} theme={theme} onTheme={setTheme} />
      <div className="grid min-h-0 grid-cols-[248px_1fr]">
        <aside ref={sidebar} className="flex min-h-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground">
          <div
            ref={explorer}
            data-testid="explorer-pane"
            className="flex min-h-[120px] flex-col"
            style={{ flex: "0 1 auto", height: explorerHeight ?? undefined }}
          >
            <Explorer
              tree={tree}
              error={treeError}
              selected={active}
              onOpen={open}
              onReload={loadTree}
              changes={{
                projectDir: project?.dir ?? null,
                isDirty: (path) => dirty.has(path),
                onCreated: open,
                // Tabs of a renamed file or of the files of a renamed folder follow (E11).
                onRenamed: (from, to) => {
                  const moved = (p: string) => (p === from ? to : p.startsWith(from + "/") ? to + p.slice(from.length) : p);
                  setTabs((t) => t.map(moved));
                  setActive((a) => (a ? moved(a) : a));
                },
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
          <Splitter container={sidebar} explorer={explorer} height={explorerHeight} onHeight={setExplorerHeight} />
          <Palette target={active ? paletteTarget : null} />
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
              <Tabs paths={tabs} active={active} dirty={dirty} onSelect={setActive} onClose={close} />
              {tabs.map((path) => (
                <FileView
                  key={path}
                  path={path}
                  active={path === active}
                  projectDir={project?.dir ?? null}
                  project={project}
                  onRun={setRun}
                  epoch={epoch}
                  onSummary={setSummary}
                  onPalette={setPaletteTarget}
                  onDirty={onDirty}
                  onRegister={onRegister}
                  onReloadExplorer={loadTree}
                />
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
        run={active ? run : null}
      />
      {closing && (
        <Dialog
          title="Save changes?"
          onCancel={() => setClosing(null)}
          actions={[
            { label: "Cancel", onClick: () => setClosing(null) },
            {
              label: "Don’t save",
              onClick: () => {
                closeNow(closing);
                setClosing(null);
              },
            },
            {
              label: "Save",
              tone: "primary",
              onClick: async () => {
                const path = closing;
                setClosing(null);
                if (await handles.current.get(path)?.save()) closeNow(path);
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
