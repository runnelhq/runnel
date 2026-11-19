import { useEffect, useState } from "react";
import mark from "../../design/logo/runnel-mark.svg";
import { fetchProject, fetchVersion, useConnection, type ProjectInfo } from "./api";
import { Header } from "./components/Header";
import { StatusBar } from "./components/StatusBar";
import { useTheme } from "./theme";

// Editor shell (E4a, mockup design/mockups/e4-editor-shell.html): header, main area and
// status bar. Explorer, tabs and the YAML view follow with E4b.
export function App() {
  const [theme, setTheme] = useTheme();
  const { state, epoch } = useConnection();
  const [project, setProject] = useState<ProjectInfo | null>(null);
  const [version, setVersion] = useState<string | null>(null);

  // (Re)load on every (re)connect: the server may have been restarted with another project.
  useEffect(() => {
    if (epoch === 0) return;
    fetchProject().then(setProject, () => setProject(null));
    fetchVersion().then((v) => setVersion(v.version), () => setVersion(null));
  }, [epoch]);

  return (
    <div className="grid h-full grid-rows-[48px_1fr_24px]">
      <Header project={project} theme={theme} onTheme={setTheme} />
      <main className="flex min-h-0 flex-col bg-background">
        {state === "disconnected" && (
          <div role="alert" className="flex items-center gap-3 border-b border-border bg-warning-soft px-4 py-2 text-warning">
            <strong>⚠ Connection to the Runnel server lost.</strong>
            <span className="text-muted-foreground">
              Reconnecting… Is <code>runnel serve</code> still running?
            </span>
          </div>
        )}
        <div className="grid flex-1 place-items-center p-8">
          <div className="max-w-[420px] text-center">
            <img src={mark} alt="" width={48} height={48} className="mx-auto" />
            <h2 className="mt-4 mb-1 text-md font-semibold">No file open</h2>
            <p className="m-0 text-muted-foreground">The project explorer and the file view follow in the next step.</p>
          </div>
        </div>
      </main>
      <StatusBar connection={state} version={version} />
    </div>
  );
}
