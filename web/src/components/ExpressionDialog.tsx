import { useMemo, useRef, useState, type KeyboardEvent } from "react";
import { completionStatus } from "@codemirror/autocomplete";
import type { EditorView } from "@codemirror/view";
import { place, tokens, type ExprContext, type Located } from "../expressions";
import { ExpressionInput } from "./ExpressionInput";
import { CloseIcon } from "./Icons";

/** An entry of the reference: what is shown and what a click inserts. */
interface RefEntry {
  name: string;
  type: string;
  description: string;
  /** Inserted text; an expression ({{ … }} added outside of one) or a variable ${…}. */
  insert: string;
}

/** The entries of the reference by group, in the order of the mockup. */
function reference(ctx: ExprContext): [string, RefEntry[]][] {
  const context: RefEntry[] = [];
  for (const p of ctx.params) {
    const parts = ["Parameter"];
    if (p.default) parts.push(`default ${p.default}`);
    if (p.required) parts.push("required");
    context.push({ name: `params.${p.name}`, type: p.type ?? "string", description: parts.join(" · "), insert: `params.${p.name}` });
  }
  for (const v of ctx.ref.variables) {
    context.push({ name: `vars.${v.name}`, type: "string", description: `Variable · ${v.value} · ${v.source}`, insert: `vars.${v.name}` });
  }
  for (const root of ctx.ref.roots) {
    if (root.name === "run") {
      for (const f of root.fields ?? []) context.push({ name: `run.${f.name}`, type: f.type, description: f.description, insert: `run.${f.name}` });
    } else if (root.name === "results") {
      for (const id of ctx.actions) {
        context.push({ name: `results.${id}`, type: "result", description: `Result of ${id}: success, status, output …`, insert: `results.${id}.` });
      }
    } else if (root.name === "result" || root.name === "env") {
      context.push({ name: root.name, type: root.type, description: root.description, insert: `${root.name}.` });
    }
  }
  const groups: [string, RefEntry[]][] = [["Context", context]];
  for (const f of ctx.ref.functions) {
    const group = f.group ?? "Functions";
    let entries = groups.find(([g]) => g === group)?.[1];
    if (!entries) {
      entries = [];
      groups.push([group, entries]);
    }
    entries.push({ name: f.signature ?? f.name, type: f.type, description: f.description, insert: `${f.name}(` });
  }
  return groups;
}

/** Whether a value is one expression and keeps the type of its result (ADR 0006). */
function keepsType(text: string): boolean {
  const t = text.trim();
  const exprs = tokens(t).filter((x) => x.kind === "expr");
  return exprs.length === 1 && exprs[0].from === 0 && exprs[0].to === t.length;
}

/**
 * The larger expression editor (roadmap S35b, mockup design/mockups/s35-expressions.html,
 * state "expanded"): line numbers and a searchable reference of names and functions;
 * a click on an entry inserts it at the cursor. Apply (Ctrl+Enter) writes the field,
 * Cancel and Escape leave it unchanged.
 */
export function ExpressionDialog(props: {
  label: string;
  fieldKey: string;
  /** Id of the action or link the field belongs to, shown next to the field key. */
  owner?: string;
  value: string;
  multiline: boolean;
  problems: Located[];
  ctx: ExprContext;
  onApply: (value: string) => void;
  onCancel: () => void;
}) {
  const [text, setText] = useState(props.value);
  const [query, setQuery] = useState("");
  const view = useRef<EditorView | null>(null);
  const textRef = useRef(text);
  textRef.current = text;
  const actions = useRef({ apply: props.onApply, cancel: props.onCancel });
  actions.current = { apply: props.onApply, cancel: props.onCancel };

  const groups = useMemo(() => reference(props.ctx), [props.ctx]);
  const q = query.trim().toLowerCase();
  const shown = groups
    .map(([g, entries]) => [g, q ? entries.filter((e) => (e.name + " " + e.description).toLowerCase().includes(q)) : entries] as const)
    .filter(([, entries]) => entries.length > 0);

  const insert = (e: RefEntry) => {
    const v = view.current;
    if (!v) return;
    const { from, to } = v.state.selection.main;
    const inside = place(v.state.doc.toString(), from).in === "expr";
    const before = inside ? e.insert : `{{ ${e.insert}`;
    const after = inside ? "" : " }}";
    v.dispatch({
      changes: { from, to, insert: before + after },
      selection: { anchor: from + before.length },
      userEvent: "input.type",
    });
    v.focus();
  };

  // Escape closes a visible list of suggestions, otherwise the dialog – decided before the
  // editor sees the key, which would also use it to cancel a query that shows nothing yet.
  const onKeyDownCapture = (e: KeyboardEvent) => {
    if (e.key !== "Escape") return;
    const v = view.current;
    if (v && completionStatus(v.state) === "active") return;
    e.preventDefault();
    e.stopPropagation();
    actions.current.cancel();
  };
  // Ctrl+Enter applies (also from the search field). No key reaches the page behind the
  // dialog (Ctrl+S, F5, Delete …).
  const onKeyDown = (e: KeyboardEvent) => {
    e.stopPropagation();
    // Saving waits until the dialog is closed; the browser must not save the page either.
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") e.preventDefault();
    if (e.defaultPrevented) return;
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      actions.current.apply(textRef.current);
    }
  };

  const changed = text !== props.value;
  // Problems are known for the text of the field; a changed text is checked when applied.
  const status = changed ? (
    "Checked when applied"
  ) : props.problems.length === 0 ? (
    <>
      <span className="text-success">✔</span> No problems
    </>
  ) : (
    <>
      <span className="text-destructive">✖</span> {props.problems.length === 1 ? "1 problem" : `${props.problems.length} problems`}
    </>
  );

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-tiefe-950/45"
      onMouseDown={(e) => e.target === e.currentTarget && props.onCancel()}
    >
      <div
        role="dialog"
        aria-modal="true"
        onKeyDownCapture={onKeyDownCapture}
        onKeyDown={onKeyDown}
        aria-label={`Edit ${props.label}`}
        className="grid h-[520px] max-h-[calc(100vh-32px)] w-[860px] max-w-[calc(100vw-32px)] grid-rows-[auto_1fr_auto] overflow-hidden rounded-xl border border-border bg-popover text-popover-foreground shadow-lg"
      >
        <div className="flex items-center gap-2.5 border-b border-border px-5 py-3.5">
          <span
            className="grid h-6 min-w-6 place-items-center rounded-full border-[1.5px] border-current bg-card px-1 text-xs font-bold"
            style={{ color: "var(--link-expression)" }}
          >
            ƒ
          </span>
          <h2 id="expression-dialog-title" className="m-0 text-md font-semibold">
            {props.label}
          </h2>
          <span className="font-mono text-2xs text-muted-foreground">
            {props.owner ? `${props.owner} · ` : ""}
            {props.fieldKey}
          </span>
          <button
            type="button"
            aria-label="Close"
            onClick={props.onCancel}
            className="ml-auto grid size-7 cursor-pointer place-items-center rounded-md border-0 bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <CloseIcon width={14} height={14} />
          </button>
        </div>
        <div className="grid min-h-0 grid-cols-[1fr_280px]">
          <div className="flex min-h-0 flex-col gap-2.5 px-5 py-4">
            <div className="min-h-0 flex-1 [&>div]:h-full [&_.runnel-expr]:h-full">
              <ExpressionInput
                labelId="expression-dialog-title"
                value={props.value}
                invalid={false}
                multiline={props.multiline}
                problems={changed ? [] : props.problems}
                ctx={props.ctx}
                large
                viewRef={view}
                onChange={setText}
                onSubmit={() => actions.current.apply(textRef.current)}
                onCommit={() => undefined}
                testId="expression-dialog-editor"
              />
            </div>
            <div className="flex flex-wrap gap-x-3 gap-y-1 text-2xs text-muted-foreground">
              <span>
                <Kbd>{"{{"}</Kbd> expression
              </span>
              <span>
                <Kbd>{"${"}</Kbd> variable
              </span>
              <span>
                <Kbd>Ctrl</Kbd>+<Kbd>Space</Kbd> suggestions
              </span>
              <span>Click an entry on the right to insert it</span>
            </div>
          </div>
          <div className="flex min-h-0 flex-col border-l border-border bg-sidebar">
            <label className="mx-3 mt-3 mb-2 flex h-7 items-center gap-1.5 rounded-md border border-input bg-background px-2 text-muted-foreground">
              <svg viewBox="0 0 24 24" width={14} height={14} fill="none" stroke="currentColor" strokeWidth={1.6} aria-hidden="true">
                <circle cx="11" cy="11" r="6" />
                <path d="M20 20l-4.5-4.5" />
              </svg>
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search names and functions"
                aria-label="Search names and functions"
                className="min-w-0 flex-1 border-0 bg-transparent text-xs text-foreground outline-none"
              />
            </label>
            <div className="min-h-0 flex-1 overflow-auto px-2 pb-3 text-xs" role="list" aria-label="Reference">
              {shown.length === 0 && <p className="mx-2 my-2 text-muted-foreground">Nothing matches “{query}”.</p>}
              {shown.map(([group, entries]) => (
                <div key={group}>
                  <h5 className="label-caps mx-2 mt-2.5 mb-1">{group}</h5>
                  {entries.map((e) => (
                    <button
                      key={e.name}
                      type="button"
                      role="listitem"
                      title={`Insert ${e.insert.replace(/[.(]$/, "")}`}
                      // Keeps the cursor of the editor where it is.
                      onMouseDown={(ev) => ev.preventDefault()}
                      onClick={() => insert(e)}
                      className="grid w-full cursor-pointer grid-cols-[1fr_auto] gap-x-2 rounded-sm border-0 bg-transparent px-2 py-1 text-left text-foreground hover:bg-sidebar-accent"
                    >
                      <span className="font-mono">{e.name}</span>
                      <span className="font-mono text-[10px] text-muted-foreground">{e.type}</span>
                      <small className="col-span-2 text-2xs text-muted-foreground">{e.description}</small>
                    </button>
                  ))}
                </div>
              ))}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2 border-t border-border px-5 py-3">
          <span className="text-xs text-muted-foreground" data-testid="expression-dialog-status">
            {status} · {keepsType(text) ? "result keeps its type" : "result is text"}
          </span>
          <span className="flex-1" />
          <button
            type="button"
            onClick={props.onCancel}
            className="inline-flex h-7 cursor-pointer items-center rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => props.onApply(text)}
            className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-primary bg-primary px-2.5 text-xs font-medium text-primary-foreground"
          >
            Apply <span className="font-mono text-[10px] opacity-75">Ctrl+Enter</span>
          </button>
        </div>
      </div>
    </div>
  );
}

function Kbd(props: { children: string }) {
  return <kbd className="rounded-[3px] border border-border px-[3px] font-mono text-[10px]">{props.children}</kbd>;
}
