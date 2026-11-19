import { useCallback, useEffect, useState } from "react";
import mark from "../../design/logo/runnel-mark.svg";
import { fetchProject, fetchVersion, useConnection, type ProjectInfo } from "./api";
import { Explorer } from "./components/Explorer";
import { FileView } from "./components/FileView";
import { Header } from "./components/Header";
import { StatusBar } from "./components/StatusBar";
import { Tabs } from "./components/Tabs";
import { fetchTree, type Entry } from "./files";
import { useTheme } from "./theme";

// Editor shell after the approved mockup design/mockups/e4-editor-shell.html:
// header, explorer, tabs with read-only file views, status bar.
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
  const close = (path: string) => {
    const i = tabs.indexOf(path);
    const rest = tabs.filter((p) => p !== path);
    setTabs(rest);
    if (active === path) setActive(rest[Math.min(i, rest.length - 1)] ?? null);
  };

  return (
    <div className="grid h-full grid-rows-[48px_1fr_24px]">
      <Header project={project} theme={theme} onTheme={setTheme} />
      <div className="grid min-h-0 grid-cols-[248px_1fr]">
        <Explorer tree={tree} error={treeError} selected={active} onOpen={open} onReload={loadTree} />
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
              <Tabs paths={tabs} active={active} onSelect={setActive} onClose={close} />
              <FileView
                key={active}
                path={active}
                projectDir={project?.dir ?? null}
                epoch={epoch}
                onSummary={setSummary}
                onReloadExplorer={loadTree}
              />
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
      <StatusBar connection={state} version={version} summary={active ? summary : null} />
    </div>
  );
}
