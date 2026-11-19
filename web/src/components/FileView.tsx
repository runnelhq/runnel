import { useCallback, useEffect, useState } from "react";
import { APIError, fetchFile, fullPath, isYAML, kindLabel, kindOf, type FileContent } from "../files";
import { validate, type Validation } from "../workflow";
import { Canvas } from "./Canvas";
import { CodeView } from "./CodeView";
import { CodeIcon, CopyIcon, LockIcon, ReloadIcon, WorkflowIcon } from "./Icons";

type View = "canvas" | "yaml";

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
  const isWorkflow = kindOf(path) === "workflow";
  // Workflows open as canvas (mockup E5); other files as YAML.
  const [view, setView] = useState<View>(isWorkflow ? "canvas" : "yaml");
  const [focusLine, setFocusLine] = useState<number | null>(null);
  const [validation, setValidation] = useState<Validation | null>(null);

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

  // Workflows are checked by the server, which also returns the parsed workflow for the canvas.
  useEffect(() => {
    if (!isWorkflow || state.status !== "loaded") return;
    let cancelled = false;
    validate(path, state.file.text).then(
      (v) => !cancelled && setValidation(v),
      () => !cancelled && setValidation(null),
    );
    return () => {
      cancelled = true;
    };
  }, [isWorkflow, path, state]);

  useEffect(() => {
    if (state.status !== "loaded") return onSummary(null);
    const wf = validation?.workflow;
    onSummary(
      view === "canvas" && wf
        ? `Workflow · ${wf.actions?.length ?? 0} actions · ${wf.links?.length ?? 0} links`
        : fileSummary(path, state.file),
    );
  }, [state, path, view, validation, onSummary]);

  const showInYAML = useCallback((line: number) => {
    setView("yaml");
    setFocusLine(line);
  }, []);

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
        <Badge title="Editing follows in a later version">
          <LockIcon width={12} height={12} />
          Read-only
        </Badge>
        {validation && validation.errors > 0 && (
          <Badge testId="error-count" tone="error" title={problemList(validation, "error")}>
            ✖ {validation.errors}
          </Badge>
        )}
        {validation && validation.warnings > 0 && (
          <Badge testId="warning-count" tone="warning" title={problemList(validation, "warning")}>
            ⚠ {validation.warnings}
          </Badge>
        )}
        <span className="flex-1" />
        {isWorkflow && <ViewSwitch view={view} onView={setView} />}
        <CopyPath path={props.projectDir ? fullPath(props.projectDir, path) : path} />
      </div>
      {state.status !== "loaded" ? (
        <div className="flex-1" aria-busy="true" />
      ) : view === "yaml" ? (
        <CodeView text={state.file.text} yaml={isYAML(path)} focusLine={focusLine} />
      ) : validation?.workflow ? (
        <Canvas workflow={validation.workflow} problems={validation.problems} onShowInYAML={showInYAML} />
      ) : validation ? (
        <NotAGraph path={path} validation={validation} onShowYAML={() => showInYAML(validation.problems[0]?.line ?? 1)} />
      ) : (
        <div className="runnel-canvas flex-1" aria-busy="true" />
      )}
    </>
  );
}

const problemList = (v: Validation, severity: "error" | "warning") =>
  v.problems
    .filter((p) => p.severity === severity)
    .map((p) => (p.line ? `${p.line}:${p.column ?? 1} ${p.message}` : p.message))
    .join("\n");

function ViewSwitch(props: { view: View; onView: (v: View) => void }) {
  const views: [View, string, typeof CodeIcon][] = [
    ["canvas", "Canvas", WorkflowIcon],
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
function NotAGraph(props: { path: string; validation: Validation; onShowYAML: () => void }) {
  const first = props.validation.problems.find((p) => p.severity === "error");
  return (
    <div className="runnel-canvas grid flex-1 place-items-center p-8" role="alert">
      <div className="max-w-[440px] rounded-xl border border-border bg-card px-7 py-6 text-center shadow-md">
        <span className="inline-grid size-10 place-items-center rounded-full bg-danger-soft text-lg font-bold text-destructive">
          ✖
        </span>
        <h2 className="mt-3 mb-1 text-md font-semibold">This workflow can’t be shown as a graph</h2>
        <p className="mt-0 mb-3 text-muted-foreground">
          The file has errors. Fix them in the YAML view or in your text editor.
        </p>
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

function Badge(props: { children: React.ReactNode; title?: string; testId?: string; tone?: "error" | "warning" }) {
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
      className={`inline-flex h-5 items-center gap-1 rounded-full px-2 text-2xs font-semibold whitespace-pre-line ${tone}`}
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
