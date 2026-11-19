import { useEffect, useState } from "react";
import { APIError, fetchFile, fullPath, isYAML, kindLabel, kindOf, type FileContent } from "../files";
import { CodeView } from "./CodeView";
import { CopyIcon, LockIcon, ReloadIcon } from "./Icons";

type State =
  | { status: "loading" }
  | { status: "loaded"; file: FileContent }
  | { status: "error"; notFound: boolean; message: string };

/** Summary of the shown file for the status bar, e.g. "YAML · UTF-8 · 40 lines". */
export function fileSummary(path: string, file: FileContent): string {
  const lines = file.text.replace(/\r\n?/g, "\n").replace(/\n$/, "").split("\n").length;
  return [isYAML(path) ? "YAML" : "Text", file.encoding, `${lines} ${lines === 1 ? "line" : "lines"}`].join(" · ");
}

/**
 * Shows one file read-only (editing follows with the canvas). It reloads when epoch
 * changes, i.e. after a reconnect.
 */
export function FileView(props: {
  path: string;
  projectDir: string | null;
  epoch: number;
  onSummary: (summary: string | null) => void;
  onReloadExplorer: () => void;
}) {
  const { path, epoch, onSummary } = props;
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    fetchFile(path).then(
      (file) => !cancelled && setState({ status: "loaded", file }),
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
  }, [path, epoch]);

  useEffect(() => {
    onSummary(state.status === "loaded" ? fileSummary(path, state.file) : null);
  }, [state, path, onSummary]);

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
  return (
    <>
      <div className="flex h-10 shrink-0 items-center gap-3 border-b border-border px-4">
        <span className="font-mono text-xs text-muted-foreground" data-testid="crumbs">
          {slash >= 0 && `${path.slice(0, slash).split("/").join(" / ")} / `}
          <b className="font-medium text-foreground">{path.slice(slash + 1)}</b>
        </span>
        <Badge testId="kind">{kindLabel[kindOf(path)]}</Badge>
        <Badge title="Editing follows with the canvas">
          <LockIcon width={12} height={12} />
          Read-only
        </Badge>
        <span className="flex-1" />
        <CopyPath path={props.projectDir ? fullPath(props.projectDir, path) : path} />
      </div>
      {state.status === "loaded" ? (
        <CodeView text={state.file.text} yaml={isYAML(path)} />
      ) : (
        <div className="flex-1" aria-busy="true" />
      )}
    </>
  );
}

function Badge(props: { children: React.ReactNode; title?: string; testId?: string }) {
  return (
    <span
      title={props.title}
      data-testid={props.testId}
      className="inline-flex h-5 items-center gap-1 rounded-full bg-secondary px-2 text-2xs font-semibold text-secondary-foreground"
    >
      {props.children}
    </span>
  );
}

function SmallButton(props: { children: React.ReactNode; onClick: () => void; title?: string }) {
  return (
    <button
      type="button"
      title={props.title}
      onClick={props.onClick}
      className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
    >
      {props.children}
    </button>
  );
}

function CopyPath(props: { path: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <SmallButton
      title="Copy the file path"
      onClick={() => navigator.clipboard.writeText(props.path).then(() => setCopied(true), () => setCopied(false))}
    >
      <CopyIcon width={14} height={14} />
      {copied ? "✔ Copied" : "Copy path"}
    </SmallButton>
  );
}
