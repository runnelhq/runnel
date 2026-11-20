import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { tileColor, useCatalog, type Schema } from "../catalog";
import {
  idPattern,
  linkKind,
  stepsKey,
  usedFields,
  type Action,
  type Problem,
  type RecordSchema,
  type TransformSchemas,
  type Workflow,
} from "../workflow";
import type { Selection } from "./Canvas";
import { ActionIcon, ChevronIcon, CloseIcon, PipelineIcon, TrashIcon, WorkflowIcon } from "./Icons";
import { linkColors, linkSymbols, type SettableKind } from "./LinkEdge";
import { formatDuration, type RunView } from "../run";
import { exprContext, locate, useReference, type ExprContext } from "../expressions";
import { ExpressionInput } from "./ExpressionInput";
import { ExpressionDialog } from "./ExpressionDialog";

/** A value written to a field; null removes the field from the file. */
export type FieldValue = string | number | boolean | null;

export interface PropertiesProps {
  workflow: Workflow;
  /** The selected actions and links. */
  selected: Selection;
  problems: Problem[];
  /** A field to show and focus (data-field key); seq changes with every request. */
  focus?: { key: string; seq: number } | null;
  /** Sets a field of an action, e.g. "with.message" or "retry.attempts". */
  onSet: (id: string, field: string, value: FieldValue) => void;
  /** Sets a field of the workflow or pipeline: name, description, settings.*. */
  onSetWorkflow: (field: string, value: FieldValue) => void;
  /** Changes a link; when is success, failure, always or an expression "{{ … }}"; type (pipelines) data or error. */
  onLinkChange: (index: number, change: LinkChange) => void;
  onDeleteLink: (index: number) => void;
  onRename: (from: string, to: string) => void;
  onHide: () => void;
  /** Pipelines: the fields around each transform (/validate), missing while the file has errors in its fields or links. */
  schemas?: Record<string, TransformSchemas>;
  /** The current or last run, to show the result of the selected action (mockup E8). */
  run?: RunView | null;
  onShowInLog?: (actionId: string) => void;
}

/** A change of a link in the panel. */
export interface LinkChange {
  when?: SettableKind | string;
  loop?: boolean;
  disabled?: boolean;
  type?: "data" | "error";
}

/** Pause after typing before a change is applied (decision for mockup E7). */
export const applyDelay = 500;

/**
 * Properties (mockup design/mockups/e7-properties.html) of the selected action – ID,
 * description, the configuration as a form generated from the JSON Schema of the
 * descriptor (ADR 0014) and the execution settings –, of the selected link, or of the
 * workflow when nothing is selected. Changes apply after a short pause or when leaving
 * the field; problems reported by the server appear at their field.
 */
export function PropertiesPanel(props: PropertiesProps) {
  const { workflow, selected, focus } = props;
  const actions = selected.actions;
  const index = actions.length === 1 ? (workflow.actions ?? []).findIndex((a) => a.id === actions[0]) : -1;
  const action = index >= 0 ? workflow.actions![index] : undefined;
  const link = actions.length === 0 && selected.links.length === 1 ? selected.links[0] : undefined;
  const count = actions.length + selected.links.length;
  const scroller = useRef<HTMLDivElement>(null);
  const ref = useReference();
  const ctx = useMemo(() => exprContext(ref, workflow), [ref, workflow]);

  // Shows and focuses a field, e.g. after a click on a problem; it lights up briefly.
  useEffect(() => {
    if (!focus) return;
    let frame = requestAnimationFrame(function find(tries = 0) {
      const el = scroller.current?.querySelector<HTMLElement>(`[data-field="${CSS.escape(focus.key)}"]`);
      if (!el) {
        if (tries < 20) frame = requestAnimationFrame(() => find(tries + 1));
        return;
      }
      el.scrollIntoView({ block: "nearest" });
      el.querySelector<HTMLElement>("input, textarea, select, button, .cm-content")?.focus({ preventScroll: true });
      el.classList.add("runnel-flash");
      setTimeout(() => el.classList.remove("runnel-flash"), 1200);
    });
    return () => cancelAnimationFrame(frame);
  }, [focus]);

  return (
    <section aria-label="Properties" className="flex min-h-0 w-[320px] shrink-0 flex-col border-l border-border bg-background">
      <div className="flex h-9 shrink-0 items-center justify-between border-b border-border pr-2 pl-4">
        <span className="label-caps">Properties</span>
        <button
          type="button"
          title="Hide properties"
          aria-label="Hide properties"
          onClick={props.onHide}
          className="grid size-6 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <CloseIcon width={14} height={14} />
        </button>
      </div>
      <div ref={scroller} className="min-h-0 flex-1 overflow-auto pb-4">
        {action ? (
          // The key resets all fields when another action is selected.
          <ActionProperties key={action.id} {...props} ctx={ctx} action={action} index={index} />
        ) : link !== undefined && workflow.links?.[link] ? (
          workflow.kind === "pipeline" ? (
            <PipelineLinkProperties key={link} {...props} index={link} />
          ) : (
            <LinkProperties key={link} {...props} ctx={ctx} index={link} />
          )
        ) : count > 1 ? (
          <p className="mx-4 my-4 text-xs text-muted-foreground">
            {count} elements selected. Select one {workflow.kind === "pipeline" ? "transform" : "action"} or link to see its properties.
          </p>
        ) : workflow.kind === "pipeline" ? (
          <PipelineProperties {...props} />
        ) : (
          <WorkflowProperties {...props} />
        )}
      </div>
    </section>
  );
}

function ActionProperties(props: PropertiesProps & { action: Action; index: number; ctx: ExprContext }) {
  const { action, index, workflow, problems, onSet } = props;
  const pipeline = workflow.kind === "pipeline";
  const catalog = useCatalog(workflow.kind);
  const d = catalog.get(action.type);
  const base = `$.${stepsKey(workflow)}[${index}]`;
  const set = (field: string) => (value: FieldValue) => onSet(action.id, field, value);

  // Problems of this action, by field; the rest is listed at the top.
  const own = problems.filter((p) => p.path === base || p.path?.startsWith(base + "."));
  const used = new Set<Problem>();
  const at = (field: string, name?: string) => {
    const found = own.filter(
      (p) =>
        p.path === `${base}.${field}` ||
        p.path?.startsWith(`${base}.${field}.`) ||
        (name !== undefined && p.path === `${base}.with` && p.message.includes(`field "${name}"`)),
    );
    found.forEach((p) => used.add(p));
    return found;
  };

  const ids = new Set((workflow.actions ?? []).map((a) => a.id));
  const incoming = (workflow.links ?? []).filter((l) => l.to === action.id && !l.disabled).length;
  const schema = d?.schema;
  const config = Object.entries(schema?.properties ?? {});

  const configFields = config.map(([name, s]) => (
    <SchemaField
      key={name}
      name={name}
      schema={s}
      required={schema?.required?.includes(name) ?? false}
      value={action.with?.[name]}
      problems={at(`with.${name}`, name)}
      ctx={props.ctx}
      owner={action.id}
      onChange={set(`with.${name}`)}
    />
  ));
  const execution = (
    <>
      <Field label="Timeout" fieldKey="timeout" help="Per attempt, e.g. 30s or 5m. Empty: no limit" problems={at("timeout")}>
        {(p) => <TextInput {...p} mono value={action.timeout ?? ""} onCommit={(v) => set("timeout")(v || null)} />}
      </Field>
      <RetryFields action={action} problems={[...at("retry")]} onSet={(f, v) => set(`retry.${f}`)(v)} />
      <Check
        fieldKey="continueOnError"
        checked={action.continueOnError ?? false}
        label="Continue on error"
        help="Failure links are not followed; the workflow continues as if it succeeded"
        onChange={(on) => set("continueOnError")(on ? true : null)}
      />
      <Field
        label="Join"
        fieldKey="join"
        problems={at("join")}
        help={
          incoming > 1
            ? action.join
              ? "When several incoming links apply: wait for all, start on the first, or run once per link"
              : "Not set: the project default applies (usually all)"
            : "Only for actions with several incoming links"
        }
      >
        {() => (
          <Segmented
            label="Join"
            value={action.join}
            disabled={incoming <= 1 && !action.join}
            options={[
              ["all", "All"],
              ["first", "First"],
              ["each", "Each"],
            ]}
            onChange={(v) => set("join")(v)}
          />
        )}
      </Field>
    </>
  );
  const summary = pipeline
    ? `copies ${action.copies || 1} · ${action.distribution || "copy"}`
    : [action.timeout && "timeout", action.retry?.attempts && action.retry.attempts > 1 && `retry ${action.retry.attempts}×`, action.continueOnError && "continue on error"]
        .filter(Boolean)
        .join(" · ");
  const outgoing = (workflow.links ?? []).filter((l) => l.from === action.id && l.type === "data").length;
  const rest = own.filter((p) => !used.has(p));

  return (
    <div data-testid="properties">
      <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-1.5">
        <span
          className="grid size-8 shrink-0 place-items-center rounded-md"
          style={d ? { background: tileColor(d), color: "var(--rn-tiefe-0)" } : { background: "var(--rn-tiefe-300)", color: "var(--rn-tiefe-800)" }}
        >
          <ActionIcon type={action.type} known={!!d} />
        </span>
        <div className="min-w-0">
          <h3 className="m-0 text-base leading-tight font-semibold">{d?.label ?? `Unknown ${pipeline ? "transform" : "action"} type`}</h3>
          <div className="font-mono text-2xs text-muted-foreground">{action.type}</div>
        </div>
      </div>
      <p className="mx-4 mt-0 mb-2 text-xs text-muted-foreground">
        {d ? d.description : "This type is not known to this version of Runnel. Change its configuration in the YAML view."}
      </p>
      <LastRun {...props} id={action.id} />
      {rest.length > 0 && (
        <ul className="mx-4 mt-0 mb-3 list-none rounded-md bg-danger-soft px-2.5 py-1.5 text-2xs text-destructive">
          {rest.map((p, i) => (
            <li key={i}>
              {p.severity === "error" ? "✖" : "⚠"} {p.message}
            </li>
          ))}
        </ul>
      )}
      <IdField id={action.id} taken={ids} onRename={(to) => props.onRename(action.id, to)} problems={at("id")} />
      {!pipeline && (
        <Field label="Description" fieldKey="description" problems={at("description")}>
          {(p) => (
            <TextInput {...p} value={action.description ?? ""} placeholder="Optional note shown in the run log" onCommit={(v) => set("description")(v || null)} />
          )}
        </Field>
      )}
      {d && (
        <Section title="Configuration">
          {configFields.length > 0 ? (
            configFields
          ) : (
            <p className="mx-4 mt-0 mb-3 text-xs text-muted-foreground">This {pipeline ? "transform" : "action"} has no configuration.</p>
          )}
        </Section>
      )}
      <Section title="Execution" summary={summary || "defaults"}>
        {pipeline ? (
          <TransformExecution action={action} parallel={d ? !!d.parallel : true} outgoing={outgoing} at={at} set={set} />
        ) : (
          execution
        )}
      </Section>
      {pipeline && <IncomingFields workflow={workflow} action={action} schemas={props.schemas} />}
    </div>
  );
}

function Section(props: { title: string; summary?: string; defaultOpen?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(props.defaultOpen ?? true);
  return (
    <div className="mt-2 border-t border-border">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex h-[34px] w-full cursor-pointer items-center gap-1.5 border-0 bg-transparent px-4 text-xs font-semibold text-foreground"
      >
        <ChevronIcon open={open} width={12} height={12} className="text-muted-foreground" />
        {props.title}
        {props.summary && <span className="ml-auto font-mono text-2xs font-normal text-muted-foreground">{props.summary}</span>}
      </button>
      {open && props.children}
    </div>
  );
}

let nextFieldId = 0;

/** A labelled field with help text and the problems reported for it. */
function Field(props: {
  label: string;
  fieldKey: string;
  /** The key shown next to the label, if it differs from fieldKey. */
  keyLabel?: string;
  required?: boolean;
  help?: string;
  problems?: Problem[];
  /** A problem found before sending, e.g. "Enter a number". */
  localError?: string | null;
  /** Messages shown instead of the problems' own, e.g. with "did you mean …?". */
  messages?: string[];
  /** Corrections offered below the problems, e.g. "Replace with result". */
  fixes?: { label: string; apply: () => void }[];
  children: (p: { id: string; labelId: string; invalid: boolean }) => ReactNode;
}) {
  const [id] = useState(() => `field-${++nextFieldId}`);
  const errors = [...(props.localError ? [props.localError] : []), ...(props.messages ?? (props.problems ?? []).map((p) => p.message))];
  const invalid = errors.length > 0;
  return (
    <div className="mb-3 px-4" data-field={props.fieldKey}>
      <label id={`${id}-label`} htmlFor={id} className="mb-1 flex items-baseline gap-1 text-xs font-medium">
        {props.label}
        {props.required && (
          <span className="text-destructive" title="Required" aria-hidden="true">
            *
          </span>
        )}
        <span aria-hidden="true" className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">
          {props.keyLabel ?? props.fieldKey}
        </span>
      </label>
      {props.children({ id, labelId: `${id}-label`, invalid })}
      {invalid ? (
        <div role="alert" className="mt-1 text-2xs text-destructive">
          {errors.map((e, i) => (
            <div key={i}>✖ {e}</div>
          ))}
          {props.fixes?.map((f) => (
            <button
              key={f.label}
              type="button"
              onClick={f.apply}
              className="mt-1 inline-flex h-[18px] cursor-pointer items-center rounded-sm border border-border bg-background px-1.5 text-2xs text-foreground hover:bg-muted"
            >
              {f.label}
            </button>
          ))}
        </div>
      ) : (
        props.help && <div className="mt-1 text-2xs text-muted-foreground">{props.help}</div>
      )}
    </div>
  );
}

const inputClass = (invalid: boolean, mono?: boolean, variable?: boolean) =>
  "block w-full min-h-[30px] rounded-md border bg-background px-2 outline-none focus:outline-2 " +
  (variable ? "text-expression " : "text-foreground ") +
  (mono ? "font-mono text-xs " : "text-sm ") +
  (invalid ? "border-destructive outline-2 outline-danger-soft" : "border-input focus:border-ring focus:outline-ring/25");

/**
 * A text field that applies its value after a pause in typing and when it is left.
 * While it has the focus, changes from outside (e.g. after a reload) do not replace
 * what is being typed.
 */
function TextInput(props: {
  id: string;
  labelId?: string;
  invalid: boolean;
  value: string;
  placeholder?: string;
  mono?: boolean;
  multiline?: boolean;
  onCommit: (value: string) => void;
}) {
  const { value, onCommit } = props;
  const [text, setText] = useState(value);
  const focused = useRef(false);
  const committed = useRef(value);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pending = useRef<() => void>(() => undefined);

  useEffect(() => {
    committed.current = value;
    if (!focused.current) setText(value);
  }, [value]);
  // A change still waiting for the pause is applied when the field goes away.
  useEffect(() => () => pending.current(), []);

  const commit = (v: string) => {
    clearTimeout(timer.current ?? undefined);
    pending.current = () => undefined;
    if (v === committed.current) return;
    committed.current = v;
    onCommit(v);
  };
  const change = (v: string) => {
    setText(v);
    clearTimeout(timer.current ?? undefined);
    timer.current = setTimeout(() => commit(v), applyDelay);
    pending.current = () => commit(v);
  };
  const variable = /^\$\{[^}]+\}$/.test(text.trim());
  const common = {
    id: props.id,
    value: text,
    placeholder: props.placeholder,
    "aria-invalid": props.invalid,
    onFocus: () => {
      focused.current = true;
    },
    onBlur: () => {
      focused.current = false;
      commit(text);
    },
    className: inputClass(props.invalid, props.mono || variable, variable),
  };
  if (props.multiline) {
    return (
      <textarea
        {...common}
        rows={Math.min(12, Math.max(3, text.split("\n").length))}
        onChange={(e) => change(e.target.value)}
        className={common.className + " resize-y py-1.5"}
      />
    );
  }
  return (
    <input
      {...common}
      onChange={(e) => change(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") commit(text);
      }}
    />
  );
}

/** The id of the action: renamed with Enter or when the field is left (like on the canvas). */
function IdField(props: { id: string; taken: Set<string>; problems: Problem[]; onRename: (to: string) => void }) {
  const [text, setText] = useState(props.id);
  useEffect(() => setText(props.id), [props.id]);
  const error = !idPattern.test(text)
    ? "Use letters, digits, - and _, starting with a letter"
    : text !== props.id && props.taken.has(text)
      ? `“${text}” is already used`
      : null;
  const commit = () => {
    if (error) setText(props.id);
    else if (text !== props.id) props.onRename(text);
  };
  return (
    <Field label="ID" fieldKey="id" problems={props.problems} localError={error}>
      {(p) => (
        <input
          id={p.id}
          value={text}
          aria-invalid={p.invalid}
          onChange={(e) => setText(e.target.value.trim())}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit();
            if (e.key === "Escape") setText(props.id);
          }}
          className={inputClass(p.invalid)}
        />
      )}
    </Field>
  );
}

/** A field of the configuration, rendered from its JSON Schema. */
function SchemaField(props: {
  name: string;
  schema: Schema;
  required: boolean;
  value: unknown;
  problems: Problem[];
  ctx: ExprContext;
  /** Id of the action, shown in the larger editor. */
  owner: string;
  onChange: (value: FieldValue) => void;
}) {
  const { name, schema: s, value } = props;
  const [localError, setLocalError] = useState<string | null>(null);
  const label = name.charAt(0).toUpperCase() + name.slice(1).replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase();
  const key = `with.${name}`;
  const scalar = value === undefined || value === null || ["string", "number", "boolean"].includes(typeof value);

  if (!scalar || s.type === "array" || s.type === "object") {
    return (
      <Field label={label} fieldKey={key} required={props.required} help={s.description} problems={props.problems}>
        {() => <p className="m-0 rounded-md bg-muted px-2 py-1.5 text-2xs text-muted-foreground">Lists and nested values are changed in the YAML view for now.</p>}
      </Field>
    );
  }
  if (s.type === "boolean") {
    return (
      <Check
        fieldKey={`with.${name}`}
        checked={value === true}
        label={label}
        help={s.description}
        problems={props.problems}
        onChange={(on) => props.onChange(on ? true : null)}
      />
    );
  }
  if (s.enum && s.enum.length > 0) {
    const current = value === undefined ? "" : String(value);
    return (
      <Field label={label} fieldKey={key} required={props.required} help={s.description} problems={props.problems}>
        {(p) => (
          <select
            id={p.id}
            value={current}
            aria-invalid={p.invalid}
            onChange={(e) => props.onChange(e.target.value === "" ? null : e.target.value)}
            className={inputClass(p.invalid) + (current === "" ? " text-muted-foreground" : "")}
          >
            <option value="">{s.default !== undefined ? `${String(s.default)} (default)` : "Not set"}</option>
            {s.enum!.map((v) => (
              <option key={String(v)} value={String(v)}>
                {String(v)}
              </option>
            ))}
          </select>
        )}
      </Field>
    );
  }

  const numeric = s.type === "integer" || s.type === "number";
  const example = s.examples?.[0];
  const placeholder =
    s.default !== undefined ? `${String(s.default)} (default)` : example !== undefined ? `e.g. ${String(example)}` : undefined;
  let help = s.description;
  if (props.required && !help) help = "Required";
  if (!numeric) {
    // Text fields take ${…} variables and {{ … }} expressions (roadmap S35a).
    const text = value === undefined || value === null ? "" : String(value);
    return (
      <ExpressionField
        label={label}
        fieldKey={key}
        required={props.required}
        help={help}
        problems={props.problems}
        value={text}
        placeholder={placeholder}
        multiline={s.format === "runnel-text"}
        ctx={props.ctx}
        owner={props.owner}
        onCommit={(v) => props.onChange(v === "" ? null : v)}
      />
    );
  }
  return (
    <Field label={label} fieldKey={key} required={props.required} help={help} problems={props.problems} localError={localError}>
      {(p) => (
        <TextInput
          {...p}
          value={value === undefined || value === null ? "" : String(value)}
          placeholder={placeholder}
          mono={numeric || s.format === "runnel-duration"}
          multiline={s.format === "runnel-text"}
          onCommit={(v) => {
            if (v === "") {
              setLocalError(null);
              return props.onChange(null);
            }
            if (numeric && !v.includes("${") && !v.includes("{{")) {
              const n = Number(v);
              if (v.trim() === "" || Number.isNaN(n) || (s.type === "integer" && !Number.isInteger(n))) {
                setLocalError(s.type === "integer" ? "Enter a whole number" : "Enter a number");
                return;
              }
              setLocalError(null);
              return props.onChange(n);
            }
            setLocalError(null);
            props.onChange(v);
          }}
        />
      )}
    </Field>
  );
}

/**
 * A field with variables and expressions: problems of expressions are marked at their
 * position, a misspelt name can be replaced with one click (mockup S35, state "error").
 */
function ExpressionField(props: {
  label: string;
  fieldKey: string;
  required?: boolean;
  help?: string;
  problems: Problem[];
  value: string;
  placeholder?: string;
  multiline?: boolean;
  ctx: ExprContext;
  /** Id of the action or the link, shown in the larger editor. */
  owner?: string;
  testId?: string;
  onCommit: (value: string) => void;
}) {
  const { value, ctx, problems } = props;
  // The larger editor (roadmap S35b) with the text of the field when it was opened.
  const [expanded, setExpanded] = useState<string | null>(null);
  const located = useMemo(
    () => problems.map((p) => ({ problem: p, at: locate(value, p.message, ctx) })),
    [problems, value, ctx],
  );
  const marks = useMemo(() => located.flatMap((l) => (l.at ? [l.at] : [])), [located]);
  const messages = located.map((l) => (l.at?.fix ? `${l.problem.message} – did you mean ${l.at.fix}?` : l.problem.message));
  const fixes = located.flatMap(({ at }) =>
    at?.fix
      ? [{ label: `Replace with ${at.fix}`, apply: () => props.onCommit(value.slice(0, at.from) + at.fix + value.slice(at.to)) }]
      : [],
  );
  return (
    <Field
      label={props.label}
      fieldKey={props.fieldKey}
      required={props.required}
      help={props.help}
      problems={problems}
      messages={messages}
      fixes={fixes}
    >
      {(p) => (
        <>
          <ExpressionInput
            labelId={p.labelId}
            invalid={p.invalid}
            value={value}
            placeholder={props.placeholder}
            multiline={props.multiline}
            problems={marks}
            ctx={ctx}
            testId={props.testId}
            onCommit={props.onCommit}
            onExpand={setExpanded}
          />
          {expanded !== null && (
            <ExpressionDialog
              label={props.label}
              fieldKey={props.fieldKey}
              owner={props.owner}
              value={expanded}
              multiline={props.multiline ?? false}
              problems={expanded === value ? marks : []}
              ctx={ctx}
              onCancel={() => setExpanded(null)}
              onApply={(v) => {
                setExpanded(null);
                if (v !== value) props.onCommit(v);
              }}
            />
          )}
        </>
      )}
    </Field>
  );
}

function RetryFields(props: { action: Action; problems: Problem[]; onSet: (field: string, value: FieldValue) => void }) {
  const { retry } = props.action;
  const [localError, setLocalError] = useState<string | null>(null);
  const attempts = retry?.attempts ?? 0;
  return (
    <>
      <Field
        label="Retry"
        fieldKey="retry"
        help="Attempts in total · backoff before the second attempt"
        problems={props.problems}
        localError={localError}
      >
        {(p) => (
          <div className="grid grid-cols-2 gap-2">
            <TextInput
              id={p.id}
              invalid={p.invalid}
              mono
              value={retry?.attempts !== undefined ? String(retry.attempts) : ""}
              placeholder="1"
              onCommit={(v) => {
                if (v === "") {
                  setLocalError(null);
                  return props.onSet("attempts", null);
                }
                const n = Number(v);
                if (!Number.isInteger(n) || n < 1) {
                  setLocalError("Attempts: enter a whole number of 1 or more");
                  return;
                }
                setLocalError(null);
                props.onSet("attempts", n);
              }}
            />
            <TextInput
              id={`${p.id}-backoff`}
              invalid={p.invalid}
              mono
              value={retry?.backoff ?? ""}
              placeholder="e.g. 30s"
              onCommit={(v) => props.onSet("backoff", v || null)}
            />
          </div>
        )}
      </Field>
      <Field label="Backoff strategy" fieldKey="retry.strategy">
        {() => (
          <Segmented
            label="Backoff strategy"
            value={retry?.strategy ?? "fixed"}
            disabled={attempts <= 1}
            options={[
              ["fixed", "Fixed"],
              ["exponential", "Exponential"],
            ]}
            onChange={(v) => props.onSet("strategy", v === "fixed" ? null : v)}
          />
        )}
      </Field>
    </>
  );
}

function Segmented(props: {
  label: string;
  value: string | undefined;
  options: [string, ReactNode][];
  disabled?: boolean;
  /** Options that cannot be chosen (the chosen one stays selectable). */
  disabledOptions?: string[];
  onChange: (value: string) => void;
}) {
  return (
    <div role="group" aria-label={props.label} className="flex gap-0.5 rounded-md border border-border bg-muted p-0.5">
      {props.options.map(([v, label]) => (
        <button
          key={v}
          type="button"
          aria-pressed={props.value === v}
          disabled={props.disabled || props.disabledOptions?.includes(v)}
          onClick={() => props.value !== v && props.onChange(v)}
          className="flex-1 cursor-pointer rounded-sm border-0 bg-transparent px-1.5 py-[3px] text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm disabled:cursor-default disabled:opacity-50"
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function Check(props: {
  checked: boolean;
  label: string;
  help?: string;
  fieldKey?: string;
  problems?: Problem[];
  onChange: (on: boolean) => void;
}) {
  return (
    <div className="mb-3 px-4" data-field={props.fieldKey}>
      <label className="flex cursor-pointer items-start gap-2 text-xs">
        <input
          type="checkbox"
          checked={props.checked}
          onChange={(e) => props.onChange(e.target.checked)}
          className="mt-0.5 size-4 shrink-0 accent-primary"
        />
        <span>
          {props.label}
          {props.help && <small className="block text-2xs text-muted-foreground">{props.help}</small>}
        </span>
      </label>
      {(props.problems ?? []).map((p, i) => (
        <div key={i} role="alert" className="mt-1 text-2xs text-destructive">
          ✖ {p.message}
        </div>
      ))}
    </div>
  );
}

/** Problems at a YAML path below base, e.g. "$.links[2]" and field "when". */
function problemsAt(problems: Problem[], base: string, field: string) {
  const path = base === "$" ? `$.${field}` : `${base}.${field}`;
  return problems.filter((p) => p.path === path || p.path?.startsWith(path + ".") || p.path?.startsWith(path + "["));
}

const kindSymbols = linkSymbols;
const kindColors = linkColors;

/**
 * Execution of a transform (mockup S25a): copies and the distribution to several outgoing
 * data links. Inputs and outputs run in one copy only (they would read or write twice).
 */
function TransformExecution(props: {
  action: Action;
  parallel: boolean;
  outgoing: number;
  at: (field: string) => Problem[];
  set: (field: string) => (value: FieldValue) => void;
}) {
  const { action, parallel, outgoing, at, set } = props;
  const copies = action.copies || 1;
  const distribution = action.distribution || "copy";
  const canDistribute = outgoing > 1 || distribution === "distribute";
  return (
    <>
      <Field
        label="Copies"
        fieldKey="copies"
        problems={at("copies")}
        help={
          parallel
            ? "Instances that share the incoming records; the order of the records is then no longer fixed"
            : `${action.type} runs as one instance (it would read or write the same data more than once)`
        }
      >
        {(p) => (
          <Stepper
            {...p}
            value={copies}
            min={1}
            max={parallel || copies > 1 ? maxCopies : 1}
            onCommit={(n) => set("copies")(n === 1 ? null : n)}
          />
        )}
      </Field>
      <Field
        label="Distribution"
        fieldKey="distribution"
        problems={at("distribution")}
        help={
          !canDistribute
            ? "Only with more than one outgoing data link"
            : distribution === "distribute"
              ? "Records go to the data links in turn"
              : "Every outgoing data link gets every record"
        }
      >
        {() => (
          <Segmented
            label="Distribution"
            value={distribution}
            options={[
              ["copy", "Copy to all"],
              ["distribute", "Distribute"],
            ]}
            disabledOptions={canDistribute ? [] : ["distribute"]}
            onChange={(v) => set("distribution")(v === "copy" ? null : v)}
          />
        )}
      </Field>
    </>
  );
}

/** Upper limit of copies (model.MaxCopies). */
const maxCopies = 64;

/** A whole number with − and + buttons; typed values apply when the field is left or Enter is pressed. */
function Stepper(props: { id: string; labelId?: string; invalid: boolean; value: number; min: number; max: number; onCommit: (n: number) => void }) {
  const { value, min, max, onCommit } = props;
  const [text, setText] = useState(String(value));
  useEffect(() => setText(String(value)), [value]);
  const commit = (n: number) => {
    const clamped = Math.min(max, Math.max(min, n));
    setText(String(clamped));
    if (clamped !== value) onCommit(clamped);
  };
  const typed = () => {
    const n = Number(text);
    if (text.trim() === "" || !Number.isInteger(n)) setText(String(value));
    else commit(n);
  };
  const button =
    "grid w-7 cursor-pointer place-items-center border border-l-0 border-input bg-background text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-50 last:rounded-r-md";
  return (
    <div className="flex">
      <input
        id={props.id}
        aria-labelledby={props.labelId}
        aria-invalid={props.invalid}
        inputMode="numeric"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={typed}
        onKeyDown={(e) => e.key === "Enter" && typed()}
        className={inputClass(props.invalid, true) + " rounded-r-none"}
      />
      <button type="button" aria-label="Fewer" title="Fewer" disabled={value <= min} onClick={() => commit(value - 1)} className={button}>
        −
      </button>
      <button type="button" aria-label="More" title="More" disabled={value >= max} onClick={() => commit(value + 1)} className={button}>
        +
      </button>
    </div>
  );
}

/** Wraps a condition in {{ … }} if it is not an expression yet; "" stays empty. */
export function asExpression(text: string): string {
  const t = text.trim();
  if (t === "") return "";
  if (!t.startsWith("{{")) return `{{ ${t} }}`;
  return t.endsWith("}}") ? t : `${t} }}`;
}

/**
 * Properties of a link: condition, loop, disabled (mockup E7, state "link"); a condition
 * can be an expression (mockup S35, state "condition").
 */
function LinkProperties(props: PropertiesProps & { index: number; ctx: ExprContext }) {
  const { index, workflow, problems } = props;
  const l = workflow.links![index];
  // "Expression" chosen, but nothing written yet: the field is shown, the file unchanged.
  const [editing, setEditing] = useState(false);
  const saved = linkKind(l.when);
  const kind = editing ? "expression" : saved;
  const base = `$.links[${index}]`;
  const own = problems.filter((p) => p.path === base || p.path?.startsWith(base + "."));
  const rest = own.filter((p) => !["when", "loop", "disabled"].some((f) => p.path?.startsWith(`${base}.${f}`)));
  const help: Record<string, string> = {
    success: `Followed when ${l.from} has succeeded`,
    failure: `Followed when ${l.from} has failed (after all retries)`,
    always: `Followed when ${l.from} has finished, whatever the result`,
    expression: "Followed when the expression is true",
  };
  return (
    <div data-testid="link-properties">
      <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-1.5">
        <span
          className="grid h-6 min-w-6 place-items-center rounded-full border-[1.5px] border-current bg-card px-1 text-xs font-bold"
          style={{ color: kindColors[kind] }}
        >
          {kindSymbols[kind]}
        </span>
        <div>
          <h3 className="m-0 text-base leading-tight font-semibold">Link</h3>
          <div className="font-mono text-2xs text-muted-foreground">links[{index}]</div>
        </div>
      </div>
      <p className="mx-4 mt-0 mb-2 font-mono text-xs" data-testid="link-ends">
        {l.from} → {l.to}
      </p>
      {rest.length > 0 && <ProblemList problems={rest} />}
      <Field label="Condition" fieldKey={kind === "expression" ? "when-kind" : "when"} keyLabel="when" help={kind === "expression" ? undefined : help[kind]}>
        {() => (
          <Segmented
            label="Condition"
            value={kind}
            options={[
              ["success", "✔ Success"],
              ["failure", "✖ Failure"],
              ["always", "∞ Always"],
              ["expression", "ƒ Expr."],
            ]}
            onChange={(v) => {
              if (v === "expression") return setEditing(true);
              setEditing(false);
              props.onLinkChange(index, { when: v as SettableKind });
            }}
          />
        )}
      </Field>
      {kind === "expression" && (
        <ExpressionField
          label="Expression"
          fieldKey="when"
          help={`${help.expression}. \${…} is replaced before the expression runs`}
          problems={problemsAt(problems, base, "when")}
          value={saved === "expression" ? (l.when ?? "") : "{{  }}"}
          ctx={props.ctx}
          owner={`${l.from} → ${l.to}`}
          testId="link-expression"
          onCommit={(v) => {
            const when = asExpression(v);
            if (when === "" || when.replace(/\s/g, "") === "{{}}") return;
            setEditing(false);
            props.onLinkChange(index, { when });
          }}
        />
      )}
      <Check
        fieldKey="loop"
        checked={l.loop ?? false}
        label="Loop link"
        help="Leads back to an earlier action; set automatically when a drawn link closes a cycle"
        problems={problemsAt(problems, base, "loop")}
        onChange={(on) => props.onLinkChange(index, { loop: on })}
      />
      <Check
        fieldKey="disabled"
        checked={l.disabled ?? false}
        label="Disabled"
        help="Kept in the file, but never followed (shown dashed)"
        problems={problemsAt(problems, base, "disabled")}
        onChange={(on) => props.onLinkChange(index, { disabled: on })}
      />
      <div className="px-4">
        <button
          type="button"
          onClick={() => props.onDeleteLink(index)}
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-destructive bg-background px-2.5 text-xs font-medium text-destructive hover:bg-danger-soft"
        >
          <TrashIcon width={14} height={14} />
          Delete link
        </button>
      </div>
    </div>
  );
}

/** Properties of the workflow when nothing is selected (mockup E7, state "workflow"). */
function WorkflowProperties(props: PropertiesProps) {
  const { workflow, problems, onSetWorkflow } = props;
  const [localError, setLocalError] = useState<string | null>(null);
  const outputs = workflow.outputs ?? [];
  return (
    <div data-testid="workflow-properties">
      <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-1.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground">
          <WorkflowIcon />
        </span>
        <div className="min-w-0">
          <h3 className="m-0 text-base leading-tight font-semibold">Workflow</h3>
        </div>
      </div>
      <p className="mx-4 mt-0 mb-2 text-xs text-muted-foreground">Nothing selected – properties of the workflow.</p>
      <Field label="Name" fieldKey="name" required problems={problemsAt(problems, "$", "name")}>
        {(p) => <TextInput {...p} value={workflow.name ?? ""} onCommit={(v) => onSetWorkflow("name", v || null)} />}
      </Field>
      <Field label="Description" fieldKey="description" problems={problemsAt(problems, "$", "description")}>
        {(p) => (
          <TextInput {...p} multiline value={workflow.description ?? ""} onCommit={(v) => onSetWorkflow("description", v || null)} />
        )}
      </Field>
      <Section title="Settings">
        <Field
          label="Timeout"
          fieldKey="settings.timeout"
          help="Limits the whole run, e.g. 2h. Empty: no limit"
          problems={problemsAt(problems, "$", "settings.timeout")}
        >
          {(p) => (
            <TextInput
              {...p}
              mono
              value={workflow.settings?.timeout ?? ""}
              placeholder="Empty: no limit"
              onCommit={(v) => onSetWorkflow("settings.timeout", v || null)}
            />
          )}
        </Field>
        <Field
          label="Max. concurrent runs"
          fieldKey="settings.maxConcurrentRuns"
          help="How many runs of this workflow may run at the same time"
          problems={problemsAt(problems, "$", "settings.maxConcurrentRuns")}
          localError={localError}
        >
          {(p) => (
            <TextInput
              {...p}
              mono
              value={workflow.settings?.maxConcurrentRuns ? String(workflow.settings.maxConcurrentRuns) : ""}
              placeholder="Empty: no limit"
              onCommit={(v) => {
                const n = Number(v);
                if (v !== "" && (!Number.isInteger(n) || n < 1)) {
                  setLocalError("Enter a whole number of 1 or more");
                  return;
                }
                setLocalError(null);
                onSetWorkflow("settings.maxConcurrentRuns", v === "" ? null : n);
              }}
            />
          )}
        </Field>
      </Section>
      <ParametersSection workflow={workflow} problems={problems} />
      <Section title="Outputs" summary={String(outputs.length)} defaultOpen={false}>
        <div data-field="outputs">
          {outputs.length > 0 && (
            <ul className="mx-4 mt-0 mb-3 list-none rounded-md border border-border p-0">
              {outputs.map((o) => (
                <li key={o.name} className="grid grid-cols-[1fr_auto] gap-x-2 border-b border-border px-2.5 py-1.5 text-xs last:border-b-0">
                  <b className="font-mono">{o.name}</b>
                  <span className="truncate font-mono">{o.value}</span>
                  <small className="col-span-2 font-mono text-2xs text-muted-foreground">{o.type ?? "string"}</small>
                </li>
              ))}
            </ul>
          )}
          <ProblemList problems={problemsAt(problems, "$", "outputs")} />
        </div>
      </Section>
    </div>
  );
}

/** The parameters of a workflow or pipeline (read-only for now). */
function ParametersSection(props: { workflow: Workflow; problems: Problem[] }) {
  const params = props.workflow.parameters ?? [];
  return (
    <Section title="Parameters" summary={String(params.length)}>
      <div data-field="parameters">
        {params.length > 0 && (
          <ul className="mx-4 mt-0 mb-3 list-none rounded-md border border-border p-0">
            {params.map((p) => (
              <li key={p.name} className="grid grid-cols-[1fr_auto] gap-x-2 border-b border-border px-2.5 py-1.5 text-xs last:border-b-0">
                <b className="font-mono">{p.name}</b>
                <span className="font-mono">{p.default ?? ""}</span>
                <small className="col-span-2 font-mono text-2xs text-muted-foreground">
                  {p.type ?? "string"} · {p.required ? "required" : "optional"}
                </small>
              </li>
            ))}
          </ul>
        )}
        <ProblemList problems={problemsAt(props.problems, "$", "parameters")} />
      </div>
      <p className="mx-4 mt-0 mb-3 rounded-md bg-muted px-2.5 py-2 text-2xs text-muted-foreground">
        {props.workflow.kind === "pipeline"
          ? "Adding and changing parameters follows in a later step; until then in the YAML view."
          : "Adding and changing parameters and outputs follows in a later step; until then in the YAML view."}
      </p>
    </Section>
  );
}

/** Properties of a pipeline link: its type, data or error (mockup S25a, state "link"). */
function PipelineLinkProperties(props: PropertiesProps & { index: number }) {
  const { index, workflow, problems } = props;
  const l = workflow.links![index];
  const kind = l.type === "error" ? "error" : "data";
  const catalog = useCatalog("pipeline");
  const from = workflow.actions?.find((a) => a.id === l.from);
  const d = from ? catalog.get(from.type) : undefined;
  const base = `$.links[${index}]`;
  const own = problems.filter((p) => p.path === base || p.path?.startsWith(base + "."));
  const rest = own.filter((p) => !p.path?.startsWith(`${base}.type`));
  const help =
    kind === "data"
      ? `Passes the records of ${l.from} on`
      : d && !d.recordErrors
        ? `${d.type} passes no records to error links`
        : `${d?.type ?? l.from} passes records it cannot process to its error links`;
  return (
    <div data-testid="link-properties">
      <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-1.5">
        <span
          className="grid h-6 min-w-6 place-items-center rounded-full border-[1.5px] border-current bg-card px-1 text-xs font-bold"
          style={{ color: kindColors[kind] }}
        >
          {kind === "data" ? "→" : kindSymbols.error}
        </span>
        <div>
          <h3 className="m-0 text-base leading-tight font-semibold">Link</h3>
          <div className="font-mono text-2xs text-muted-foreground" data-testid="link-ends">
            {l.from} → {l.to}
          </div>
        </div>
      </div>
      <p className="mx-4 mt-0 mb-2 text-xs text-muted-foreground">
        {kind === "data" ? (
          <>
            Records of <b className="text-foreground">{l.from}</b> flow to <b className="text-foreground">{l.to}</b>.
          </>
        ) : (
          <>
            Records that <b className="text-foreground">{l.from}</b> could not process, with the reason.
          </>
        )}
      </p>
      {rest.length > 0 && <ProblemList problems={rest} />}
      <Field label="Type" fieldKey="type" help={help} problems={problemsAt(problems, base, "type")}>
        {() => (
          <Segmented
            label="Type"
            value={kind}
            options={[
              [
                "data",
                <>
                  <span style={{ color: kindColors.data }}>→</span> Data
                </>,
              ],
              [
                "error",
                <>
                  <span style={{ color: kindColors.error }}>✖</span> Error
                </>,
              ],
            ]}
            onChange={(v) => props.onLinkChange(index, { type: v as "data" | "error" })}
          />
        )}
      </Field>
      <LinkFields kind={kind} schemas={props.schemas?.[l.from]} known={props.schemas !== undefined} />
      <div className="px-4">
        <button
          type="button"
          onClick={() => props.onDeleteLink(index)}
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-destructive bg-background px-2.5 text-xs font-medium text-destructive hover:bg-danger-soft"
        >
          <TrashIcon width={14} height={14} />
          Delete link
        </button>
      </div>
    </div>
  );
}

/** Fields of the incoming records of a transform (mockup S25a, state "canvas"); fields used in its configuration are marked. */
function IncomingFields(props: { workflow: Workflow; action: Action; schemas?: Record<string, TransformSchemas> }) {
  const { workflow, action, schemas } = props;
  const from = [...new Set((workflow.links ?? []).filter((l) => l.to === action.id).map((l) => l.from))];
  if (from.length === 0) return null;
  const input = schemas?.[action.id]?.input;
  const used = usedFields(action.with);
  return (
    <Section title="Incoming fields" summary={input ? `from ${from.join(", ")}${input.open ? " · open" : ""}` : undefined}>
      {input ? (
        <>
          <FieldList schema={input} used={used} />
          <p className="mx-4 mt-0 mb-3 text-2xs text-muted-foreground">
            {input.open
              ? "Fields come from the transforms before. Not all of them are known before the pipeline runs, e.g. columns of a file read without fields:."
              : "Fields come from the transforms before; fields used in the configuration are marked."}
          </p>
        </>
      ) : (
        <NoFields />
      )}
    </Section>
  );
}

/** Fields of the records on a pipeline link (mockup S25a, state "link"). */
function LinkFields(props: { kind: "data" | "error"; schemas?: TransformSchemas; known: boolean }) {
  const schema = props.schemas ? (props.kind === "error" ? props.schemas.errors : props.schemas.output) : undefined;
  return (
    <Section title="Fields on this link" summary={schema ? (schema.open ? "open" : "closed") : undefined}>
      {schema ? <FieldList schema={schema} meta={props.kind === "error"} /> : <NoFields />}
      {props.kind === "error" && (
        <p className="mx-4 mt-0 mb-3 text-2xs text-muted-foreground">
          The record goes on unchanged, with the message in <code className="font-mono">_error</code>, the field in{" "}
          <code className="font-mono">_errorField</code> and the id of the transform in <code className="font-mono">_errorTransform</code>.
        </p>
      )}
    </Section>
  );
}

function NoFields() {
  return (
    <p className="mx-4 mt-0 mb-3 text-2xs text-muted-foreground">The fields are shown once the transforms and links have no errors.</p>
  );
}

/** Error fields added on error links, shown dimmed. */
const errorFields = new Set(["_error", "_errorField", "_errorTransform"]);

/** A list of fields with their types; an open schema ends with a note on further fields. */
function FieldList(props: { schema: RecordSchema; used?: Set<string>; meta?: boolean }) {
  const fields = props.schema.fields ?? [];
  return (
    <ul data-testid="fields" className="mx-4 mt-0 mb-3 list-none rounded-md border border-border p-0 text-xs">
      {fields.map((f) => {
        const used = props.used?.has(f.name);
        return (
          <li
            key={f.name}
            data-used={used || undefined}
            title={used ? "Used in the configuration" : undefined}
            className={
              "grid h-[26px] grid-cols-[1fr_auto] items-center gap-2 border-b border-border px-2.5 last:border-b-0" +
              (used ? " bg-accent/60" : "")
            }
          >
            <span className={"truncate font-mono" + (props.meta && errorFields.has(f.name) ? " text-muted-foreground" : "")}>{f.name}</span>
            <span className="font-mono text-[10px] text-muted-foreground">{f.type || "any"}</span>
          </li>
        );
      })}
      {props.schema.open && (
        <li className="flex h-[26px] items-center px-2.5 text-muted-foreground italic">
          {fields.length > 0 ? "and fields known only when the pipeline runs" : "fields known only when the pipeline runs"}
        </li>
      )}
      {!props.schema.open && fields.length === 0 && <li className="flex h-[26px] items-center px-2.5 text-muted-foreground italic">no fields</li>}
    </ul>
  );
}

/** Properties of the pipeline when nothing is selected: name, description, settings, parameters. */
function PipelineProperties(props: PropertiesProps) {
  const { workflow, problems, onSetWorkflow } = props;
  const settings = workflow.settings ?? {};
  return (
    <div data-testid="pipeline-properties">
      <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-1.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground">
          <PipelineIcon />
        </span>
        <div className="min-w-0">
          <h3 className="m-0 text-base leading-tight font-semibold">Pipeline</h3>
        </div>
      </div>
      <p className="mx-4 mt-0 mb-2 text-xs text-muted-foreground">Nothing selected – properties of the pipeline.</p>
      <Field label="Name" fieldKey="name" required problems={problemsAt(problems, "$", "name")}>
        {(p) => <TextInput {...p} value={workflow.name ?? ""} onCommit={(v) => onSetWorkflow("name", v || null)} />}
      </Field>
      <Field label="Description" fieldKey="description" problems={problemsAt(problems, "$", "description")}>
        {(p) => (
          <TextInput {...p} multiline value={workflow.description ?? ""} onCommit={(v) => onSetWorkflow("description", v || null)} />
        )}
      </Field>
      <Section title="Settings">
        <NumberSetting
          label="Batch size"
          field="batchSize"
          value={settings.batchSize}
          help="Records passed over a link at once. Empty: 500"
          problems={problems}
          onSet={onSetWorkflow}
        />
        <NumberSetting
          label="Buffer size"
          field="bufferSize"
          value={settings.bufferSize}
          help="Records a link holds before the transform before it waits. Empty: 10000"
          problems={problems}
          onSet={onSetWorkflow}
        />
        <Check
          fieldKey="settings.strict"
          checked={settings.strict ?? false}
          label="Strict"
          help="Every link needs known fields; unknown fields are errors instead of warnings"
          problems={problemsAt(problems, "$", "settings.strict")}
          onChange={(on) => onSetWorkflow("settings.strict", on ? true : null)}
        />
      </Section>
      <ParametersSection workflow={workflow} problems={problems} />
    </div>
  );
}

/** A setting that is a whole number of 1 or more; empty removes it (the default applies). */
function NumberSetting(props: {
  label: string;
  field: string;
  value: number | undefined;
  help: string;
  problems: Problem[];
  onSet: (field: string, value: FieldValue) => void;
}) {
  const [localError, setLocalError] = useState<string | null>(null);
  const key = `settings.${props.field}`;
  return (
    <Field label={props.label} fieldKey={key} help={props.help} problems={problemsAt(props.problems, "$", key)} localError={localError}>
      {(p) => (
        <TextInput
          {...p}
          mono
          value={props.value ? String(props.value) : ""}
          placeholder="Empty: default"
          onCommit={(v) => {
            const n = Number(v);
            if (v !== "" && (!Number.isInteger(n) || n < 1)) {
              setLocalError("Enter a whole number of 1 or more");
              return;
            }
            setLocalError(null);
            props.onSet(key, v === "" ? null : n);
          }}
        />
      )}
    </Field>
  );
}

function ProblemList(props: { problems: Problem[] }) {
  if (props.problems.length === 0) return null;
  return (
    <ul className="mx-4 mt-0 mb-3 list-none rounded-md bg-danger-soft px-2.5 py-1.5 text-2xs text-destructive">
      {props.problems.map((p, i) => (
        <li key={i}>
          {p.severity === "error" ? "✖" : "⚠"} {p.message}
        </li>
      ))}
    </ul>
  );
}

const lastRunText: Record<string, string> = {
  running: "Running",
  waiting: "Waiting for further incoming links",
  succeeded: "Succeeded in the last run",
  failed: "Failed in the last run",
  cancelled: "Cancelled in the last run",
  skipped: "Skipped in the last run",
};

/** Result of the action in the current or last run, with a jump to its lines in the log (mockup E8). */
function LastRun(props: PropertiesProps & { id: string }) {
  const a = props.run?.actions[props.id];
  if (!a || a.status === "pending") return null;
  const failed = a.status === "failed";
  return (
    <div
      data-testid="last-run"
      className={"mx-4 mt-0 mb-2.5 rounded-md px-2.5 py-2 text-xs " + (failed ? "bg-danger-soft text-destructive" : "bg-muted text-foreground")}
    >
      <b>
        {failed ? "✖ " : a.status === "succeeded" ? "✔ " : ""}
        {lastRunText[a.status]}
      </b>
      {a.durationMs !== undefined && ` after ${formatDuration(a.durationMs)}`}
      {a.error && <div className="mt-0.5">{a.error}</div>}
      {a.counts && (
        <div data-testid="run-counts" className="mt-1.5 grid grid-cols-3 gap-1 text-2xs text-muted-foreground">
          <div>
            <b className="block font-mono text-sm text-foreground tabular-nums">{a.counts.read}</b>read
          </div>
          <div>
            <b className="block font-mono text-sm text-foreground tabular-nums">{a.counts.written}</b>written
          </div>
          {a.counts.errors ? (
            <div>
              <b className="block font-mono text-sm text-destructive tabular-nums">✖ {a.counts.errors}</b>to error links
            </div>
          ) : null}
        </div>
      )}
      {props.onShowInLog && (
        <button
          type="button"
          onClick={() => props.onShowInLog!(props.id)}
          className="mt-1 block cursor-pointer border-0 bg-transparent p-0 font-mono text-2xs text-muted-foreground underline"
        >
          Show in log
        </button>
      )}
    </div>
  );
}
