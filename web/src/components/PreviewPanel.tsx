import { useState } from "react";
import type { PreviewRecords, PreviewResult } from "../preview";
import type { Link } from "../workflow";
import { ReloadIcon } from "./Icons";

/** What the preview shows: the output of the previewed transform or the records of a link (by index). */
export type PreviewSelection = "output" | number;

const numericTypes = new Set(["int", "decimal", "float"]);
const errorFields = new Set(["_error", "_errorField", "_errorTransform"]);
const viewKey = "runnel.preview.view";

/**
 * The tab "Preview" below the canvas (mockup design/mockups/s44-preview.html): the records
 * of the previewed transform or of a link as a table or as JSON, the number of records
 * per input, and Refresh.
 */
export function PreviewPanel(props: {
  upTo: string;
  result: PreviewResult | null;
  loading: boolean;
  /** A problem before the preview ran (e.g. an error in the file). */
  error: string | null;
  links: Link[];
  selected: PreviewSelection;
  onSelect: (s: PreviewSelection) => void;
  limit: number;
  onLimit: (limit: number) => void;
  onRefresh: () => void;
}) {
  const { result, upTo, selected } = props;
  const [view, setView] = useState<"table" | "json">(() => {
    try {
      return localStorage.getItem(viewKey) === "json" ? "json" : "table";
    } catch {
      return "table";
    }
  });
  const choose = (v: "table" | "json") => {
    setView(v);
    try {
      localStorage.setItem(viewKey, v);
    } catch {
      // only a convenience
    }
  };
  const label = (s: PreviewSelection) => {
    if (s === "output") return `${upTo} (output)`;
    const l = props.links[s];
    return l ? `${l.from} → ${l.to}${l.type === "error" ? " (error link)" : ""}` : `link ${s + 1}`;
  };
  const choices: PreviewSelection[] = ["output", ...Object.keys(result?.links ?? {}).map(Number).sort((a, b) => a - b)];
  const shown: PreviewRecords | undefined = result ? (selected === "output" ? result.output : result.links[selected]) : undefined;
  const failed = props.error ?? (result && result.status !== "succeeded" ? (result.error ?? "the preview did not finish") : null);
  const select = "h-6 rounded-md border border-input bg-background px-1.5 text-xs text-foreground";

  return (
    <div data-testid="preview">
      <div className="flex h-[34px] items-center gap-2.5 border-t border-border px-3 text-xs whitespace-nowrap text-muted-foreground">
        <select aria-label="Records of" value={String(selected)} onChange={(e) => props.onSelect(e.target.value === "output" ? "output" : Number(e.target.value))} className={select + " font-mono"}>
          {choices.map((c) => (
            <option key={String(c)} value={String(c)}>
              {label(c)}
            </option>
          ))}
        </select>
        {shown && (
          <span data-testid="preview-count">
            <b className="font-medium text-foreground">{shown.rows}</b> {shown.rows === 1 ? "record" : "records"}
            {shown.records.length < shown.rows ? ` (first ${shown.records.length} shown)` : ""}
          </span>
        )}
        {props.loading && <span>Running the preview…</span>}
        <span className="flex-1" />
        <label className="inline-flex items-center gap-1.5">
          First
          <select aria-label="Records per input" value={props.limit} onChange={(e) => props.onLimit(Number(e.target.value))} className={select}>
            {[100, 1000].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          per input
        </label>
        <div role="group" aria-label="Show as" className="inline-flex gap-0.5 rounded-md border border-border bg-muted p-0.5">
          {(["table", "json"] as const).map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={view === v}
              onClick={() => choose(v)}
              className="cursor-pointer rounded-sm border-0 bg-transparent px-2 py-px text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm"
            >
              {v === "table" ? "Table" : "JSON"}
            </button>
          ))}
        </div>
        <button
          type="button"
          title="Run the preview again (also after changes)"
          onClick={props.onRefresh}
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-muted"
        >
          <ReloadIcon width={14} height={14} />
          Refresh
        </button>
      </div>
      {failed && (
        <div role="alert" className="mx-3 my-2.5 grid grid-cols-[18px_1fr] gap-2 rounded-md bg-danger-soft px-3 py-2.5 text-xs text-destructive">
          <span>✖</span>
          <div>
            <b>{result?.failedAt ? `The preview stopped at ${result.failedAt}` : "The preview did not run"}</b>
            <p className="mt-1 mb-0 text-foreground">{failed}</p>
          </div>
        </div>
      )}
      {!failed && shown && (view === "table" ? <RecordTable records={shown} /> : <RecordJSON records={shown} />)}
    </div>
  );
}

function cellText(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

function RecordTable(props: { records: PreviewRecords }) {
  const { fields, records } = props.records;
  if (records.length === 0) return <p className="m-0 border-t border-border px-3 py-2 text-xs text-muted-foreground">No records.</p>;
  const cell = "h-[26px] max-w-[280px] truncate border-r border-b border-border px-3 font-mono text-2xs whitespace-nowrap";
  return (
    <div className="h-[196px] overflow-auto border-t border-border text-xs">
      <table role="table" aria-label="Records" className="min-w-full border-collapse">
        <thead>
          <tr>
            <th className="sticky top-0 z-[1] h-[30px] w-11 border-r border-b border-border bg-muted px-3 text-right font-medium text-muted-foreground">#</th>
            {fields.map((f) => (
              <th
                key={f.name}
                className={"sticky top-0 z-[1] h-[30px] border-r border-b border-border bg-muted px-3 text-left font-medium whitespace-nowrap" + (errorFields.has(f.name) ? " text-muted-foreground" : "")}
              >
                {f.name}
                <span className="block font-mono text-[10px] leading-none font-normal text-muted-foreground">{f.type || "null"}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {records.map((row, i) => (
            <tr key={i} className="hover:bg-muted">
              <td className={cell + " text-right text-muted-foreground"}>{i + 1}</td>
              {fields.map((f, j) => {
                const v = row[j];
                const meta = errorFields.has(f.name);
                const cls =
                  v === null || v === undefined
                    ? " font-sans text-muted-foreground italic"
                    : numericTypes.has(f.type)
                      ? " text-right tabular-nums"
                      : meta
                        ? f.name === "_error"
                          ? " text-destructive"
                          : " text-muted-foreground"
                        : "";
                return (
                  <td key={f.name} title={cellText(v)} className={cell + cls}>
                    {cellText(v)}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RecordJSON(props: { records: PreviewRecords }) {
  const { fields, records } = props.records;
  const objects = records.map((row) => Object.fromEntries(fields.map((f, j) => [f.name, row[j] ?? null])));
  return (
    <pre data-testid="preview-json" className="m-0 h-[196px] overflow-auto border-t border-border px-3 py-2 font-mono text-2xs leading-relaxed text-foreground">
      {JSON.stringify(objects, null, 2)}
    </pre>
  );
}
