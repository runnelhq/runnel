import { useState } from "react";
import logo from "../../../design/logo/runnel-logo.svg";
import logoInverse from "../../../design/logo/runnel-logo-inverse.svg";
import { createProject, type ProjectStatus } from "../api";
import { useTheme } from "../theme";

/**
 * StartPage is shown while the server's folder has no project (R10): "Create project"
 * creates one there, like "runnel init" in the folder of the server. The folder is the
 * server's; the page only names it. onCreated gets the paths of the new files.
 */
export function StartPage(props: { status: ProjectStatus; onCreated: (created: string[]) => void }) {
  useTheme();
  const [name, setName] = useState(props.status.defaultName ?? "");
  const [force, setForce] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const blocked = !props.status.empty && !force;
  const create = async () => {
    if (busy || blocked) return;
    setBusy(true);
    setError(null);
    try {
      props.onCreated(await createProject(name.trim(), force));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };
  return (
    <main className="grid h-full place-items-center bg-background p-8 text-foreground">
      <div className="w-full max-w-[460px] rounded-xl border border-border bg-card px-7 py-6 shadow-md" data-testid="start-page">
        <img src={logo} alt="Runnel" className="h-8 [[data-theme=dark]_&]:hidden" />
        <img src={logoInverse} alt="Runnel" className="hidden h-8 [[data-theme=dark]_&]:block" />
        <h1 className="mt-5 mb-1 text-lg font-semibold">No project here</h1>
        <p className="mt-0 mb-1 text-muted-foreground">This folder has no Runnel project yet:</p>
        <p className="mt-0 mb-4 rounded-md bg-muted px-2.5 py-1.5 font-mono text-xs break-all" data-testid="start-folder">
          {props.status.dir}
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <label className="mb-1 block text-xs font-medium" htmlFor="new-project-name">
            Project name
          </label>
          <input
            id="new-project-name"
            value={name}
            autoFocus
            onChange={(e) => {
              setName(e.target.value);
              setError(null);
            }}
            aria-invalid={!!error}
            aria-describedby="new-project-hint"
            className="block h-[30px] w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:border-ring focus:outline-2 focus:outline-ring/25 aria-invalid:border-destructive"
          />
          <div id="new-project-hint" role={error ? "alert" : undefined} className={"mt-1 mb-3 text-2xs " + (error ? "text-destructive" : "text-muted-foreground")}>
            {error ? (
              `✖ ${error}`
            ) : (
              <>
                Creates <span className="font-mono">project.yaml</span>, the project folders, the environment{" "}
                <span className="font-mono">dev</span> and an example workflow – like <span className="font-mono">runnel init</span>.
              </>
            )}
          </div>
          {!props.status.empty && (
            <label className="mb-3 flex items-start gap-2 text-xs">
              <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} className="mt-0.5 size-3.5 accent-primary" />
              <span>
                <span className="font-medium">Add to the existing files.</span>{" "}
                <span className="text-muted-foreground">The folder is not empty; files that are already there are kept, none is overwritten.</span>
              </span>
            </label>
          )}
          <button
            type="submit"
            disabled={busy || blocked}
            className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-primary bg-primary px-2.5 text-xs font-medium text-primary-foreground disabled:cursor-default disabled:opacity-50"
          >
            {busy ? "Creating…" : "Create project"}
          </button>
        </form>
      </div>
    </main>
  );
}
