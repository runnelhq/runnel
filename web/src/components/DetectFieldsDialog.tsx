import { useEffect, useRef, useState } from "react";
import { DetectError, detectFields, type Detection, type DetectedField } from "../workflow";
import { DetectIcon } from "./Icons";

/** Types of fields (ADR 0007) as offered in the dialog. */
const fieldTypes = ["string", "int", "decimal", "float", "bool", "date", "datetime", "json"];
/** Types that take a number or date format. */
const formatTypes = new Set(["int", "decimal", "float", "date", "datetime"]);

type State =
  | { status: "loading" }
  | { status: "error"; message: string; baseDir?: string }
  | { status: "loaded"; detection: Detection; fields: DetectedField[] };

/**
 * "Detect fields" (mockup design/mockups/s46-fields.html): the server reads the first
 * rows of the input of a transform and proposes its fields; here they are checked and
 * changed, and "Write n fields" writes the selected ones to "fields:".
 */
export function DetectFieldsDialog(props: {
  path: string;
  /** The text shown in the editor, saved or not. */
  content: string;
  transform: string;
  /** The path as written in the configuration, shown when it has variables. */
  rawPath?: string;
  onCancel: () => void;
  onWrite: (fields: DetectedField[]) => void;
}) {
  const { path, content, transform } = props;
  const [state, setState] = useState<State>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const ref = useRef<HTMLDivElement>(null);
  const cancel = useRef(props.onCancel);
  cancel.current = props.onCancel;

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    detectFields(path, content, transform).then(
      (detection) => !cancelled && setState({ status: "loaded", detection, fields: detection.fields }),
      (err: unknown) =>
        !cancelled &&
        setState({
          status: "error",
          message: err instanceof Error ? err.message : String(err),
          baseDir: err instanceof DetectError ? err.baseDir : undefined,
        }),
    );
    return () => {
      cancelled = true;
    };
    // The text at opening counts; the dialog is modal.
  }, [path, transform, attempt]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && cancel.current();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    ref.current?.querySelector<HTMLButtonElement>("[data-primary]")?.focus();
  }, [state.status]);

  const change = (i: number, f: Partial<DetectedField>) =>
    setState((s) => (s.status === "loaded" ? { ...s, fields: s.fields.map((x, j) => (j === i ? { ...x, ...f } : x)) } : s));

  const fields = state.status === "loaded" ? state.fields : [];
  const selected = fields.filter((f) => f.selected);
  const listed = fields.some((f) => f.state);
  const button = "inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium disabled:cursor-default disabled:opacity-50";

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-tiefe-950/45" onMouseDown={(e) => e.target === e.currentTarget && props.onCancel()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label="Detect fields"
        className="flex max-h-[calc(100vh-64px)] w-[860px] max-w-[calc(100vw-48px)] flex-col rounded-xl border border-border bg-popover text-popover-foreground shadow-lg"
      >
        <div className="px-6 pt-[18px] pb-2.5">
          <h2 className="m-0 mb-2 flex items-center gap-2.5 text-md font-semibold">
            <span className="inline-grid size-7 place-items-center rounded-full bg-accent text-accent-foreground">
              <DetectIcon width={15} height={15} />
            </span>
            Detect fields of <span className="font-mono">{transform}</span>
          </h2>
          {state.status === "loaded" && <Source detection={state.detection} rawPath={props.rawPath} />}
        </div>
        {state.status === "loading" && (
          <p role="status" className="mx-6 my-4 text-xs text-muted-foreground">
            Reading the first rows…
          </p>
        )}
        {state.status === "error" && (
          <div role="alert" className="mx-6 my-4 grid grid-cols-[18px_1fr] gap-2 rounded-md bg-danger-soft px-3.5 py-3 text-xs text-destructive">
            <span>✖</span>
            <div>
              <b>The input cannot be read</b>
              <p className="mt-1 mb-0 text-foreground">
                {state.message}
                {state.baseDir && (
                  <>
                    {" "}
                    Relative paths refer to the project folder: <span className="font-mono text-2xs">{state.baseDir}</span>
                  </>
                )}
              </p>
            </div>
          </div>
        )}
        {state.status === "loaded" && (
          <div className="overflow-auto border-y border-border">
            <table className="w-full border-collapse text-xs">
              <thead>
                <tr className="text-left text-2xs text-muted-foreground">
                  {["", "Column", "Type", "Format", "Values read", "Note"].map((h, i) => (
                    <th
                      key={i}
                      className={"sticky top-0 h-7 border-b border-border bg-muted px-2 font-medium " + (i === 0 ? "w-[30px] pl-6" : i === 5 ? "pr-6" : "")}
                    >
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {fields.map((f, i) => (
                  <FieldRow key={i} field={f} onChange={(c) => change(i, c)} />
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="flex items-center gap-2 px-6 pt-3 pb-4">
          <p className="m-0 flex-1 text-2xs text-muted-foreground">
            {state.status === "error"
              ? "Correct the path in the panel and detect again, or try again once the input is there."
              : listed
                ? "Listed fields keep their type and format; new columns are unchecked. Listed fields missing in the input are marked and can be removed."
                : "A type is proposed only if every value read fits it; you can change it here. Unchecked columns are not read."}
          </p>
          <button type="button" onClick={props.onCancel} className={`${button} border-input bg-background hover:bg-muted`}>
            Cancel
          </button>
          {state.status === "error" ? (
            <button type="button" data-primary onClick={() => setAttempt((a) => a + 1)} className={`${button} border-primary bg-primary text-primary-foreground`}>
              Try again
            </button>
          ) : (
            <button
              type="button"
              data-primary
              disabled={state.status !== "loaded" || selected.length === 0}
              onClick={() => props.onWrite(selected)}
              className={`${button} border-primary bg-primary text-primary-foreground`}
            >
              Write {selected.length} {selected.length === 1 ? "field" : "fields"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

/** What was read: the file (with its variables), the settings and the number of rows. */
function Source(props: { detection: Detection; rawPath?: string }) {
  const { detection: d, rawPath } = props;
  return (
    <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-xs text-muted-foreground">
      {d.source.map((f) => (
        <span key={f.name}>
          {f.name}{" "}
          {f.name === "File" ? (
            <span className="font-mono text-2xs text-foreground">
              {d.variables && rawPath && rawPath !== f.value && (
                <>
                  <span className="text-expression">{rawPath}</span> →{" "}
                </>
              )}
              {f.value}
            </span>
          ) : (
            <b className="font-medium text-foreground">{f.value}</b>
          )}
        </span>
      ))}
      {d.variables && d.environment && (
        <span>
          Environment <b className="font-medium text-foreground">{d.environment}</b>
        </span>
      )}
      <span>
        Read <b className="font-medium text-foreground">{d.rows === 1 ? "1 row" : `${d.rows} rows`}</b> {d.complete ? "(all)" : "(the first)"}
      </span>
    </div>
  );
}

const stateTag: Record<string, string> = { listed: "listed", new: "new", missing: "not in input" };

function FieldRow(props: { field: DetectedField; onChange: (f: Partial<DetectedField>) => void }) {
  const { field: f, onChange } = props;
  const off = !f.selected;
  const cell = "border-b border-border px-2 py-[5px] align-top";
  const input = "h-[26px] w-full rounded-md border border-input bg-background px-2 text-xs text-foreground";
  return (
    <tr data-testid="detected-field" className="last:[&>td]:border-b-0">
      <td className={`${cell} pl-6`}>
        <input
          type="checkbox"
          aria-label={`Write ${f.name}`}
          checked={f.selected}
          onChange={(e) => onChange({ selected: e.target.checked })}
          className="mt-[5px] size-4 accent-primary"
        />
      </td>
      <td className={`${cell} pt-[10px] font-mono font-medium ${off ? "opacity-50" : ""}`}>
        {f.name}
        {f.state && (
          <span
            className={
              "ml-1.5 inline-flex h-4 items-center rounded-full px-1.5 align-[1px] font-sans text-[10px] font-semibold " +
              (f.state === "new" ? "bg-accent text-accent-foreground" : f.state === "missing" ? "bg-warning-soft text-warning" : "bg-secondary text-secondary-foreground")
            }
          >
            {stateTag[f.state]}
          </span>
        )}
      </td>
      <td className={`${cell} w-[118px] ${off ? "opacity-50" : ""}`}>
        <select
          aria-label={`Type of ${f.name}`}
          value={f.type}
          onChange={(e) => onChange({ type: e.target.value, ...(formatTypes.has(e.target.value) ? {} : { format: "" }) })}
          className={input}
        >
          {fieldTypes.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </td>
      <td className={`${cell} w-[124px] ${off ? "opacity-50" : ""}`}>
        <input
          aria-label={`Format of ${f.name}`}
          value={f.format ?? ""}
          placeholder="format"
          disabled={!formatTypes.has(f.type)}
          onChange={(e) => onChange({ format: e.target.value })}
          className={`${input} font-mono placeholder:text-muted-foreground disabled:bg-muted`}
        />
      </td>
      <td
        className={`${cell} max-w-[230px] truncate pt-[9px] font-mono text-2xs text-muted-foreground ${off ? "opacity-50" : ""}`}
        title={f.values.join(", ")}
      >
        {f.values.map((v, i) => (
          <span key={i}>
            {i > 0 && ", "}
            {v === "" ? <i className="font-sans">empty</i> : v}
          </span>
        ))}
      </td>
      <td className={`${cell} max-w-[220px] pt-[9px] pr-6 text-2xs ${f.warning ? "text-warning" : "text-muted-foreground"} ${off ? "opacity-50" : ""}`}>
        {f.warning && "⚠ "}
        {f.note}
      </td>
    </tr>
  );
}
