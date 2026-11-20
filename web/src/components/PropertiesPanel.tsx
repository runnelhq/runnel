import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { categoryColor, useCatalog, type Schema } from "../catalog";
import { idPattern, linkKind, type Action, type Problem, type Workflow } from "../workflow";
import type { Selection } from "./Canvas";
import { ActionIcon, ChevronIcon, CloseIcon, TrashIcon, WorkflowIcon } from "./Icons";
import type { SettableKind } from "./LinkEdge";
import { formatDuration, type RunView } from "../run";
import { exprContext, locate, useReference, type ExprContext } from "../expressions";
import { ExpressionInput } from "./ExpressionInput";

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
  /** Sets a field of the workflow: name, description, settings.timeout, settings.maxConcurrentRuns. */
  onSetWorkflow: (field: string, value: FieldValue) => void;
  /** Changes a link; when is success, failure, always or an expression "{{ … }}". */
  onLinkChange: (index: number, change: { when?: SettableKind | string; loop?: boolean; disabled?: boolean }) => void;
  onDeleteLink: (index: number) => void;
  onRename: (from: string, to: string) => void;
  onHide: () => void;
  /** The current or last run, to show the result of the selected action (mockup E8). */
  run?: RunView | null;
  onShowInLog?: (actionId: string) => void;
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
          <LinkProperties key={link} {...props} ctx={ctx} index={link} />
        ) : count > 1 ? (
          <p className="mx-4 my-4 text-xs text-muted-foreground">
            {count} elements selected. Select one action or link to see its properties.
          </p>
        ) : (
          <WorkflowProperties {...props} />
        )}
      </div>
    </section>
  );
}

function ActionProperties(props: PropertiesProps & { action: Action; index: number; ctx: ExprContext }) {
  const { action, index, workflow, problems, onSet } = props;
  const catalog = useCatalog();
  const d = catalog.get(action.type);
  const base = `$.actions[${index}]`;
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
  const summary = [action.timeout && "timeout", action.retry?.attempts && action.retry.attempts > 1 && `retry ${action.retry.attempts}×`, action.continueOnError && "continue on error"]
    .filter(Boolean)
    .join(" · ");
  const rest = own.filter((p) => !used.has(p));

  return (
    <div data-testid="properties">
      <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-1.5">
        <span
          className="grid size-8 shrink-0 place-items-center rounded-md"
          style={
            d
              ? { background: categoryColor[d.category] ?? categoryColor.general, color: "var(--rn-tiefe-0)" }
              : { background: "var(--rn-tiefe-300)", color: "var(--rn-tiefe-800)" }
          }
        >
          <ActionIcon type={action.type} known={!!d} />
        </span>
        <div className="min-w-0">
          <h3 className="m-0 text-base leading-tight font-semibold">{d?.label ?? "Unknown action type"}</h3>
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
      <Field label="Description" fieldKey="description" problems={at("description")}>
        {(p) => (
          <TextInput {...p} value={action.description ?? ""} placeholder="Optional note shown in the run log" onCommit={(v) => set("description")(v || null)} />
        )}
      </Field>
      {d && (
        <Section title="Configuration">
          {configFields.length > 0 ? configFields : <p className="mx-4 mt-0 mb-3 text-xs text-muted-foreground">This action has no configuration.</p>}
        </Section>
      )}
      <Section title="Execution" summary={summary || "defaults"}>
        {execution}
      </Section>
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
  testId?: string;
  onCommit: (value: string) => void;
}) {
  const { value, ctx, problems } = props;
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
        />
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
  options: [string, string][];
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div role="group" aria-label={props.label} className="flex gap-0.5 rounded-md border border-border bg-muted p-0.5">
      {props.options.map(([v, label]) => (
        <button
          key={v}
          type="button"
          aria-pressed={props.value === v}
          disabled={props.disabled}
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

const kindSymbols: Record<string, string> = { success: "✔", failure: "✖", always: "∞", expression: "ƒ" };
const kindColors: Record<string, string> = {
  success: "var(--link-success)",
  failure: "var(--link-failure)",
  always: "var(--link-always)",
  expression: "var(--link-expression)",
};

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
  const params = workflow.parameters ?? [];
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
          <ProblemList problems={problemsAt(problems, "$", "parameters")} />
        </div>
        <p className="mx-4 mt-0 mb-3 rounded-md bg-muted px-2.5 py-2 text-2xs text-muted-foreground">
          Adding and changing parameters and outputs follows in a later step; until then in the YAML view.
        </p>
      </Section>
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
