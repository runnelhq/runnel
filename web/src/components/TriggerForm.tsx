import { useEffect, useMemo, useState } from "react";
import { categoryColor, type Schema } from "../catalog";
import { useReference, type ExprContext } from "../expressions";
import { useForm, useFormChange, useTriggers, type FieldChange, type TriggerData } from "../forms";
import { useSchema, useVariables } from "../reference";
import { graphOf, validate, type Parameter, type Problem } from "../workflow";
import { ChevronIcon, CopyIcon, TriggerIcon, TriggerTypeIcon } from "./Icons";
import { Field, SchemaField, TextInput, inputClass } from "./PropertiesPanel";
import { SecretInput } from "./SecretField";

/**
 * A trigger file as a form (mockup E20, design/mockups/e20-forms.html): the type as
 * cards, the fields of the type from the schema (x-runnel-variants) in sections, the
 * schedule in words with the next times, target and environment as a choice, the
 * parameters of the target as a table; the state on the server (next and last run) in
 * the head. Each change goes through POST /forms/edit, which changes only its line.
 */

/** The types as cards: label and what they do (mockup E20). */
export const triggerKinds: [type: string, label: string, text: string][] = [
  ["cron", "Schedule", "At set times"],
  ["interval", "Interval", "Every few minutes"],
  ["file", "Folder", "New files in a folder"],
  ["http", "HTTP call", "A call to an address"],
];

const noParams: ExprContext["params"] = [];

export function TriggerForm(props: {
  path: string;
  text: string;
  /** Whether the text is saved: the server knows only the saved trigger. */
  saved: boolean;
  problems: Problem[];
  /** Files of the project, for the choice of the target. */
  files: Set<string>;
  onText: (text: string) => void;
  onError: (message: string) => void;
  onOpenFile?: (path: string) => void;
}) {
  const { path, text, onError } = props;
  const form = useForm(path, text);
  const data = form === undefined ? undefined : (form?.trigger ?? null);
  const [more, setMore] = useState(false);
  // The fields a new type removed, with the text before it for Undo.
  const [removed, setRemoved] = useState<{ type: string; fields: string[]; before: string } | null>(null);
  const schema = useSchema("trigger");
  const ref = useReference();
  const ctx = useMemo<ExprContext>(() => ({ ref, params: noParams, actions: [] }), [ref]);

  const apply = useFormChange(path, text, props.onText, onError);
  const change = (c: FieldChange) => {
    const before = text;
    void apply(c).then((r) => r && setRemoved(c.field === "type" && r.removed?.length ? { type: String(c.value), fields: r.removed, before } : null));
  };

  if (data === undefined || !schema) return <div className="flex-1" aria-busy="true" />;
  if (data === null) {
    return (
      <div className="grid flex-1 place-items-center p-8 text-center text-xs text-muted-foreground" role="alert">
        The trigger cannot be read as a form; change it in the YAML view.
      </div>
    );
  }
  const variant = schema.properties?.with?.["x-runnel-variants"]?.[data.type];
  const kind = triggerKinds.find(([t]) => t === data.type);
  const enabled = data.enabled !== false;
  const at = (p: string) => props.problems.filter((x) => x.path === p || x.path?.startsWith(p + "."));

  // Sections in the order of their first field; "When" first, "More" folded and last.
  // auth has its own field.
  const sections = new Map<string, [string, Schema][]>([["When", []]]);
  for (const [name, s] of Object.entries(variant?.properties ?? {})) {
    const group = name === "auth" ? "Sign-in" : (s["x-runnel-group"] ?? "More");
    sections.set(group, [...(sections.get(group) ?? []), [name, s]]);
  }
  const order = [...sections.keys()].filter((g) => g !== "More");
  if (sections.has("More")) order.push("More");

  const fieldOf = ([name, s]: [string, Schema]) => {
    if (name === "auth") return <AuthField key={name} name={data.name} value={data.with.auth} problems={at("$.with.auth")} onChange={change} />;
    return (
      <SchemaField
        key={name}
        name={name}
        schema={s}
        required={variant?.required?.includes(name) ?? false}
        value={data.with[name]}
        problems={props.problems.filter((p) => p.path === `$.with.${name}`)}
        ctx={ctx}
        owner={data.name}
        onChange={(value) => change({ field: `with.${name}`, value })}
        onChangeMap={(map) => change({ field: `with.${name}`, map })}
      />
    );
  };

  return (
    <div className="flex-1 overflow-auto bg-background" data-testid="trigger-form">
      <div className="mx-auto max-w-[680px] px-7 pt-4 pb-10">
        <div className="mb-1 flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-md text-white" style={{ background: categoryColor.execution }}>
            <TriggerIcon width={20} height={20} />
          </span>
          <div className="min-w-0">
            <h2 className="m-0 text-lg font-semibold">{data.name}</h2>
            <div className="text-xs text-muted-foreground">
              {kind?.[1] ?? data.type} · starts <span className="font-mono">{data.target}</span>
            </div>
          </div>
          <label className="ml-auto inline-flex cursor-pointer items-center gap-2 text-xs font-medium">
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              aria-label="Enabled"
              onClick={() => change({ field: "enabled", value: enabled ? false : null })}
              className={"relative h-[17px] w-[30px] shrink-0 cursor-pointer rounded-full border-0 p-0 " + (enabled ? "bg-primary" : "bg-input")}
            >
              <span className={"absolute top-0.5 size-[13px] rounded-full bg-background shadow-sm transition-[left] " + (enabled ? "left-[15px]" : "left-0.5")} />
            </button>
            Enabled
            <span className="font-mono text-[10px] font-normal text-muted-foreground">enabled</span>
          </label>
        </div>
        <label className="mb-1 ml-12 flex items-center gap-2 text-xs text-muted-foreground">
          <input
            aria-label="Description"
            defaultValue={data.description ?? ""}
            key={data.description ?? ""}
            placeholder="What the trigger starts and why"
            onBlur={(e) => {
              const v = e.target.value.trim();
              if (v !== (data.description ?? "")) change({ field: "description", value: v === "" ? null : v });
            }}
            className="min-w-0 flex-1 border-0 border-b border-transparent bg-transparent py-0.5 text-xs text-muted-foreground outline-none hover:border-border focus:border-ring"
          />
          <span className="font-mono text-[10px]">description</span>
        </label>
        <ServerState path={path} saved={props.saved} enabled={enabled} />

        <Section title="When">
          <div role="radiogroup" aria-label="Type" className="mx-4 mb-3 grid grid-cols-4 gap-2">
            {triggerKinds.map(([t, label, what]) => (
              <TypeCard key={t} type={t} label={label} text={what} checked={t === data.type} onClick={() => t !== data.type && change({ field: "type", value: t })} />
            ))}
          </div>
          {removed && removed.type === data.type && (
            <div role="status" className="mx-4 mb-2.5 flex items-center gap-2 rounded-sm bg-muted px-2 py-1.5 text-2xs text-muted-foreground">
              ⓘ
              <span>
                Type changed to <b className="font-semibold text-foreground">{removed.type}</b>: <span className="font-mono">{removed.fields.join(", ")}</span> {removed.fields.length > 1 ? "were" : "was"} removed.
              </span>
              <button
                type="button"
                onClick={() => {
                  props.onText(removed.before);
                  setRemoved(null);
                }}
                className="ml-auto inline-flex h-6 cursor-pointer items-center rounded-md border border-input bg-background px-2 text-2xs font-medium text-foreground hover:bg-muted"
              >
                Undo
              </button>
            </div>
          )}
          {sections.get("When")!.map(fieldOf)}
          {data.type === "cron" && data.schedule && <ScheduleReading schedule={data.schedule} />}
          {data.type === "http" && <Address method={String(data.with.method ?? "")} path={String(data.with.path ?? "")} />}
        </Section>
        {sections.has("Sign-in") && <Section title="Sign-in">{sections.get("Sign-in")!.map(fieldOf)}</Section>}
        <TargetSection data={data} files={props.files} problems={props.problems} at={at} onChange={change} onOpenFile={props.onOpenFile} />
        {order
          .filter((g) => g !== "When" && g !== "Sign-in")
          .map((g) => {
            const folded = g === "More" && !more;
            return (
              <Section key={g} title={g} folded={folded} onToggle={g === "More" ? () => setMore((m) => !m) : undefined} summary={sections.get(g)!.map(([n]) => n).join(", ")}>
                {sections.get(g)!.map(fieldOf)}
              </Section>
            );
          })}
      </div>
    </div>
  );
}

function Section(props: { title: string; folded?: boolean; onToggle?: () => void; summary?: string; children: React.ReactNode }) {
  return (
    <section className="border-t border-border pt-2.5 pb-1" aria-label={props.title}>
      <h3 className="m-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">
        <button
          type="button"
          aria-expanded={!props.folded}
          onClick={props.onToggle}
          className={"-ml-1 inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent p-0 text-sm " + (props.folded ? "font-medium text-muted-foreground" : "font-semibold")}
        >
          <ChevronIcon open={!props.folded} width={12} height={12} className="text-muted-foreground" />
          {props.title}
        </button>
        {props.folded && <span className="text-2xs font-normal text-muted-foreground">{props.summary}</span>}
      </h3>
      {!props.folded && <div className="-mx-4">{props.children}</div>}
    </section>
  );
}

function TypeCard(props: { type: string; label: string; text: string; checked: boolean; onClick?: () => void }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={props.checked}
      onClick={props.onClick}
      className="group flex cursor-pointer items-start gap-2 rounded-md border border-border bg-background p-2 text-left aria-checked:border-primary aria-checked:ring-1 aria-checked:ring-primary"
    >
      <span
        className={"grid size-6 shrink-0 place-items-center rounded-md " + (props.checked ? "text-white" : "bg-muted text-muted-foreground")}
        style={props.checked ? { background: categoryColor.execution } : undefined}
      >
        <TriggerTypeIcon type={props.type} width={14} height={14} />
      </span>
      <span className="min-w-0">
        <b className="block text-xs">{props.label}</b>
        <span className="block text-[10px] leading-snug text-muted-foreground">{props.text}</span>
      </span>
    </button>
  );
}

export { TypeCard as TriggerTypeCard };

const dateTime = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });

/** Next and last run of the saved trigger on the server (GET /triggers). */
function ServerState(props: { path: string; saved: boolean; enabled: boolean }) {
  const list = useTriggers(props.saved);
  const t = list?.triggers.find((x) => x.file === props.path);
  let next: React.ReactNode = null;
  if (!props.saved) next = <span>Next run after saving</span>;
  else if (!props.enabled) next = <span>Disabled: starts no runs</span>;
  else if (t?.next) {
    next = (
      <span>
        Next {t.type === "file" ? "look into the folder" : "run"} <b className="font-semibold text-foreground">{dateTime(t.next)}</b>
      </span>
    );
  }
  const last = props.saved && t?.last;
  if (!next && !last) return <div className="mb-3" />;
  return (
    <div className="mb-3 ml-12 flex flex-wrap items-center gap-x-4 gap-y-1 text-2xs text-muted-foreground" data-testid="trigger-state">
      {next}
      {last && (
        <span>
          Last run{" "}
          {last.error ? <span className="font-bold text-destructive">✕</span> : last.skipped ? "skipped" : <span className="font-bold text-success">✓</span>} {dateTime(last.time)}
          {last.runId && <span className="font-mono"> · {last.runId.slice(0, 5)}…{last.runId.slice(-2)}</span>}
        </span>
      )}
    </div>
  );
}

/** The schedule in words and its next times (from the server, mockup E20). */
function ScheduleReading(props: { schedule: { text?: string; next: string[] } }) {
  return (
    <div className="mx-4 -mt-1.5 mb-3 rounded-sm bg-muted px-2.5 py-1.5 text-2xs" data-testid="schedule-reading">
      {props.schedule.text && <b className="font-semibold">{props.schedule.text}</b>}
      <ol className="m-0 mt-1 pl-4 font-mono text-muted-foreground">
        {props.schedule.next.map((n) => (
          <li key={n}>{new Date(n).toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" })}</li>
        ))}
      </ol>
    </div>
  );
}

/** The address callers use (mockup E20, state http), with the listener of the server. */
function Address(props: { method: string; path: string }) {
  const http = useTriggers()?.http;
  if (!props.path) return null;
  const url = (http ?? "") + props.path;
  return (
    <div className="mx-4 mb-3">
      <div className="flex items-center gap-2 rounded-md border border-border bg-muted py-1.5 pr-1.5 pl-2.5 font-mono text-xs" data-testid="trigger-address">
        <span className="font-bold text-primary">{props.method || "?"}</span>
        <span className="min-w-0 truncate">{url}</span>
        <button
          type="button"
          onClick={() => void navigator.clipboard?.writeText(url)}
          className="ml-auto inline-flex h-6 shrink-0 cursor-pointer items-center gap-1 rounded-md border border-input bg-background px-2 font-sans text-2xs font-medium hover:bg-muted"
        >
          <CopyIcon width={12} height={12} />
          Copy
        </button>
      </div>
      <div className="mt-1 text-2xs text-muted-foreground">
        {http ? "The address callers use; the listener is set when the server starts (--triggers)" : "The listener of HTTP triggers is not open; the server opens it when it starts with an HTTP trigger"}
      </div>
    </div>
  );
}

/** auth of an HTTP trigger: none or a token as a secret field (S39f). */
function AuthField(props: { name: string; value: unknown; problems: Problem[]; onChange: (c: FieldChange) => void }) {
  const token = props.value && typeof props.value === "object" ? String((props.value as Record<string, unknown>).token ?? "") : null;
  return (
    <>
      <Field label="Caller signs in with" fieldKey="with.auth" required problems={token === null ? props.problems : []}>
        {(f) => (
          <select
            id={f.id}
            value={token === null ? (props.value === undefined ? "" : "none") : "token"}
            onChange={(e) =>
              e.target.value === "token"
                ? props.onChange({ field: "with.auth", map: [{ key: "token", value: `\${secret:${props.name}-token}` }] })
                : props.onChange({ field: "with.auth", value: e.target.value === "none" ? "none" : null })
            }
            className={inputClass(f.invalid) + " cursor-pointer"}
          >
            <option value="">Not set</option>
            <option value="token">Token (Authorization: Bearer)</option>
            <option value="none">None – only on this computer or in a trusted network</option>
          </select>
        )}
      </Field>
      {token !== null && (
        <Field label="Token" fieldKey="with.auth.token" problems={props.problems} help="The caller sends it as Authorization: Bearer; use ${secret:NAME}">
          {(f) => <SecretInput id={f.id} invalid={f.invalid} value={token} onChange={(v) => props.onChange({ field: "with.auth", map: [{ key: "token", value: v ?? "" }] })} />}
        </Field>
      )}
    </>
  );
}

/** Target, environment and the parameters of the target (mockup E20). */
function TargetSection(props: {
  data: TriggerData;
  files: Set<string>;
  problems: Problem[];
  at: (path: string) => Problem[];
  onChange: (c: FieldChange) => void;
  onOpenFile?: (path: string) => void;
}) {
  const { data } = props;
  const vars = useVariables(null);
  const params = useTargetParameters(data.target);
  const targets = [...props.files].filter((f) => /\.(workflow|pipeline)\.yaml$/.test(f)).sort();
  if (data.target && !targets.includes(data.target)) targets.unshift(data.target);
  const envs = vars?.environments ?? [];
  // Parameters of the target, then those given here that it does not declare.
  const rows: { name: string; info?: Parameter }[] = (params ?? []).map((p) => ({ name: p.name, info: p }));
  for (const n of Object.keys(data.params)) if (!rows.some((r) => r.name === n)) rows.push({ name: n });
  const fromPath = new Set([...String(data.with.path ?? "").matchAll(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g)].map((m) => m[1]));
  const fileParam = data.type === "file" ? String(data.with.param ?? "") : "";
  return (
    <Section title="Target">
      <div className="grid grid-cols-[1fr_180px]">
        <Field label="Workflow or pipeline" fieldKey="target" required problems={props.at("$.target")}>
          {(f) => (
            <div className="flex gap-1.5">
              <select id={f.id} value={data.target} onChange={(e) => props.onChange({ field: "target", value: e.target.value })} className={inputClass(f.invalid, true) + " cursor-pointer"}>
                {targets.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
              {props.onOpenFile && props.files.has(data.target) && (
                <button
                  type="button"
                  onClick={() => props.onOpenFile!(data.target)}
                  className="inline-flex h-[30px] shrink-0 cursor-pointer items-center rounded-md border border-input bg-background px-2 text-2xs font-medium hover:bg-muted"
                >
                  Open
                </button>
              )}
            </div>
          )}
        </Field>
        <Field label="Environment" fieldKey="environment" problems={props.at("$.environment")} help={vars?.defaultEnvironment ? `Empty: the default environment (${vars.defaultEnvironment})` : "Empty: the default environment"}>
          {(f) => (
            <select id={f.id} value={data.environment ?? ""} onChange={(e) => props.onChange({ field: "environment", value: e.target.value || null })} className={inputClass(f.invalid, true) + " cursor-pointer"}>
              <option value="">Default</option>
              {[...new Set([...envs, ...(data.environment ? [data.environment] : [])])].map((e) => (
                <option key={e} value={e}>
                  {e}
                </option>
              ))}
            </select>
          )}
        </Field>
      </div>
      <div className="mb-3 px-4" data-field="params">
        <div className="mb-1 flex items-baseline text-xs font-medium">
          Parameters
          <span className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">params</span>
        </div>
        {rows.length === 0 ? (
          <div className="text-2xs text-muted-foreground">{params === null ? "Loading the parameters of the target…" : "The target has no parameters."}</div>
        ) : (
          <table className="w-full border-collapse text-xs" aria-label="Parameters">
            <thead>
              <tr className="text-left text-[10px] text-muted-foreground">
                <th className="w-40 px-1.5 pb-1 font-semibold">Parameter of the target</th>
                <th className="px-1.5 pb-1 font-semibold">Value</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ name, info }) => {
                const value = data.params[name];
                const source = fromPath.has(name) ? "from the path" : name === fileParam ? "the path of the file" : "";
                const problems = props.problems.filter((p) => p.path === `$.params.${name}`);
                const missing = info?.required && value === undefined && !source && data.type !== "http";
                return (
                  <tr key={name} className={"border-t border-border " + (missing ? "bg-warning-soft" : "")} data-param={name}>
                    <td className="px-1.5 py-1 align-middle font-mono font-medium">
                      {name}
                      <small className="block font-sans text-[10px] font-normal text-muted-foreground">
                        {info ? [info.type ?? "string", info.required ? "required" : info.default !== undefined ? `default ${info.default}` : ""].filter(Boolean).join(" · ") : "not a parameter of the target"}
                      </small>
                    </td>
                    <td className="px-1.5 py-1">
                      {source && value === undefined ? (
                        <span className="text-muted-foreground">{source}</span>
                      ) : (
                        <TextInput
                          id={`param-${name}`}
                          invalid={problems.length > 0}
                          mono
                          value={value === undefined || value === null ? "" : String(value)}
                          placeholder={info?.default !== undefined ? String(info.default) : ""}
                          onCommit={(v) => props.onChange({ field: `params.${name}`, value: v === "" ? null : v })}
                        />
                      )}
                      {problems.map((p, i) => (
                        <div key={i} role="alert" className="mt-0.5 text-2xs text-destructive">
                          ✖ {p.message}
                        </div>
                      ))}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {data.target && <div className="mt-1 text-2xs text-muted-foreground">The parameters of {data.target.split("/").pop()}; empty: its default</div>}
      </div>
    </Section>
  );
}

/** The parameters the saved target declares (from /validate); null while loading. */
function useTargetParameters(target: string): Parameter[] | null {
  const [params, setParams] = useState<Parameter[] | null>(null);
  useEffect(() => {
    let live = true;
    setParams(null);
    if (!target) return setParams([]);
    validate(target).then(
      (v) => live && setParams(graphOf(v)?.parameters ?? []),
      () => live && setParams([]),
    );
    return () => {
      live = false;
    };
  }, [target]);
  return params;
}
