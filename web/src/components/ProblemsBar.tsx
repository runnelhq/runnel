import { problemTarget, type Problem, type Workflow } from "../workflow";
import { ChevronIcon } from "./Icons";

/** Names of fields for "Where", as labelled in the properties panel. */
const fieldLabels: Record<string, string> = {
  id: "ID",
  description: "Description",
  timeout: "Timeout",
  retry: "Retry",
  "retry.strategy": "Backoff strategy",
  continueOnError: "Continue on error",
  join: "Join",
  joinTimeout: "Join timeout",
  when: "Condition",
  loop: "Loop link",
  disabled: "Disabled",
  name: "Name",
  "settings.timeout": "Timeout",
  "settings.maxConcurrentRuns": "Max. concurrent runs",
  parameters: "Parameters",
  outputs: "Outputs",
};

export function fieldLabel(key: string): string {
  if (fieldLabels[key]) return fieldLabels[key];
  const name = key.replace(/^with\./, "");
  return name.charAt(0).toUpperCase() + name.slice(1).replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase();
}

/** Where a problem is, e.g. "notify · Message", "Link #3 · Condition" or "Workflow · Name". */
function where(wf: Workflow, p: Problem): string {
  const t = problemTarget(wf, p);
  if (!t) return "—";
  const field = t.field ? ` · ${fieldLabel(t.field)}` : "";
  if (t.action) return t.action + field;
  if (t.link !== undefined) return `Link #${t.link + 1}${field}`;
  return `Workflow${field}`;
}

/**
 * The problems of the workflow below the canvas (mockup E7): collapsed to a tab with
 * the number of problems; open, a table of message, place and line. A click on a row
 * selects the action or link and shows the field in the properties panel.
 */
export function ProblemsBar(props: {
  workflow: Workflow;
  problems: Problem[];
  open: boolean;
  onToggle: () => void;
  onOpen: (p: Problem) => void;
}) {
  const { problems } = props;
  const errors = problems.filter((p) => p.severity === "error").length;
  return (
    <section aria-label="Problems" className="shrink-0 border-t border-border bg-background">
      <div className="flex h-8 items-center gap-0.5 px-2">
        <button
          type="button"
          aria-expanded={props.open}
          onClick={props.onToggle}
          className={
            "inline-flex h-[26px] cursor-pointer items-center gap-1.5 rounded-sm border-0 px-2.5 text-xs " +
            (props.open ? "bg-muted font-medium text-foreground" : "bg-transparent text-muted-foreground hover:text-foreground")
          }
        >
          <ChevronIcon open={props.open} width={12} height={12} />
          Problems
          {problems.length > 0 && (
            <span
              data-testid="problem-count"
              className={
                "inline-grid h-4 min-w-[18px] place-items-center rounded-full px-[5px] text-[10px] font-semibold " +
                (errors > 0 ? "bg-danger-soft text-destructive" : "bg-warning-soft text-warning")
              }
            >
              {problems.length}
            </span>
          )}
        </button>
      </div>
      {props.open && (
        <div role="table" aria-label="Problems" className="h-[132px] overflow-auto border-t border-border text-xs">
          <div role="row" className="grid h-6 grid-cols-[20px_1fr_180px_90px] items-center gap-2 bg-muted px-3 text-2xs text-muted-foreground">
            <span role="columnheader" />
            <span role="columnheader">Message</span>
            <span role="columnheader">Where</span>
            <span role="columnheader">Line</span>
          </div>
          {problems.length === 0 && <p className="m-0 px-3 py-2 text-muted-foreground">No problems.</p>}
          {problems.map((p, i) => (
            <div
              key={i}
              role="row"
              tabIndex={0}
              onClick={() => props.onOpen(p)}
              onKeyDown={(e) => e.key === "Enter" && props.onOpen(p)}
              className="grid min-h-[30px] cursor-pointer grid-cols-[20px_1fr_180px_90px] items-center gap-2 border-b border-border px-3 py-1 outline-none hover:bg-muted focus-visible:bg-accent focus-visible:text-accent-foreground"
            >
              <span role="cell" className={"font-bold " + (p.severity === "error" ? "text-destructive" : "text-warning")}>
                {p.severity === "error" ? "✖" : "⚠"}
              </span>
              <span role="cell">{p.message}</span>
              <span role="cell" className="truncate font-mono text-2xs text-muted-foreground">
                {where(props.workflow, p)}
              </span>
              <span role="cell" className="font-mono text-2xs text-muted-foreground">
                {p.line ? `line ${p.line}${p.column ? `:${p.column}` : ""}` : ""}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
