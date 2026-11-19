import { useEffect, useRef, useState, type ReactNode } from "react";
import { categoryColor, useCatalog, type Schema } from "../catalog";
import { idPattern, type Action, type Problem, type Workflow } from "../workflow";
import { ActionIcon, ChevronIcon, CloseIcon } from "./Icons";

/** A value written to a field; null removes the field from the file. */
export type FieldValue = string | number | boolean | null;

export interface PropertiesProps {
  workflow: Workflow;
  /** Ids of the selected actions. */
  selected: string[];
  problems: Problem[];
  /** Sets a field of an action, e.g. "with.message" or "retry.attempts". */
  onSet: (id: string, field: string, value: FieldValue) => void;
  onRename: (from: string, to: string) => void;
  onHide: () => void;
}

/** Pause after typing before a change is applied (decision for mockup E7). */
export const applyDelay = 500;

/**
 * Properties of the selected action (mockup design/mockups/e7-properties.html): ID,
 * description, the configuration as a form generated from the JSON Schema of the
 * descriptor (ADR 0014) and the execution settings. Changes apply after a short pause
 * or when leaving the field; problems reported by the server appear at their field.
 */
export function PropertiesPanel(props: PropertiesProps) {
  const { workflow, selected } = props;
  const index = selected.length === 1 ? (workflow.actions ?? []).findIndex((a) => a.id === selected[0]) : -1;
  const action = index >= 0 ? workflow.actions![index] : undefined;

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
      <div className="min-h-0 flex-1 overflow-auto pb-4">
        {action ? (
          // The key resets all fields when another action is selected.
          <ActionProperties key={action.id} {...props} action={action} index={index} />
        ) : (
          <p className="mx-4 my-4 text-xs text-muted-foreground">
            {selected.length > 1 ? `${selected.length} actions selected. Select one action to see its properties.` : "Select an action to see its properties."}
          </p>
        )}
      </div>
    </section>
  );
}

function ActionProperties(props: PropertiesProps & { action: Action; index: number }) {
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

function Section(props: { title: string; summary?: string; children: ReactNode }) {
  const [open, setOpen] = useState(true);
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
  required?: boolean;
  help?: string;
  problems?: Problem[];
  /** A problem found before sending, e.g. "Enter a number". */
  localError?: string | null;
  children: (p: { id: string; invalid: boolean }) => ReactNode;
}) {
  const [id] = useState(() => `field-${++nextFieldId}`);
  const errors = [...(props.localError ? [props.localError] : []), ...(props.problems ?? []).map((p) => p.message)];
  const invalid = errors.length > 0;
  return (
    <div className="mb-3 px-4" data-field={props.fieldKey}>
      <label htmlFor={id} className="mb-1 flex items-baseline gap-1 text-xs font-medium">
        {props.label}
        {props.required && (
          <span className="text-destructive" title="Required" aria-hidden="true">
            *
          </span>
        )}
        <span aria-hidden="true" className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">
          {props.fieldKey}
        </span>
      </label>
      {props.children({ id, invalid })}
      {invalid ? (
        <div role="alert" className="mt-1 text-2xs text-destructive">
          {errors.map((e, i) => (
            <div key={i}>✖ {e}</div>
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
            if (numeric && !v.includes("${")) {
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

function Check(props: { checked: boolean; label: string; help?: string; problems?: Problem[]; onChange: (on: boolean) => void }) {
  return (
    <div className="mb-3 px-4">
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
