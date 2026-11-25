import { useEffect, useRef, useState } from "react";
import { categoryColor } from "../catalog";
import { useForm, useFormChange, type FieldChange, type SettingsData } from "../forms";
import { missingIn, useVariables, type VariableInfo } from "../reference";
import type { Problem } from "../workflow";
import { ChevronIcon, LinesIcon, PlusIcon, ProjectIcon, TrashIcon } from "./Icons";
import { Field, OnErrorField, TextInput, inputClass } from "./PropertiesPanel";

/**
 * An environment or project.yaml as a form (mockup E20, states "environment" and
 * "project"): variables as a table with the value of the other level, the secret store
 * as a choice, the secrets of secretStore: inline only by name. Each change goes through
 * POST /forms/edit, which changes only its line.
 */

/** The secret stores with what they mean (ADR 0022); local and shared follow with S39d/S39e. */
const stores: [string, string][] = [
  ["env", "env – environment variables of the computer"],
  ["inline", "inline – secrets: in this file, plain text"],
  ["local", "local – the local secret store (not available yet)"],
  ["shared", "shared – a shared secret store (not available yet)"],
];

const namePattern = /^[A-Za-z_][A-Za-z0-9_-]*$/;

export function SettingsForm(props: {
  kind: "environment" | "project";
  path: string;
  text: string;
  /** Whether the text is saved: the variables of the other files come from the saved files. */
  saved: boolean;
  projectDir?: string | null;
  problems: Problem[];
  /** Workflows of the project, for settings.onError of project.yaml (S72b). */
  workflows?: string[];
  onText: (text: string) => void;
  onError: (message: string) => void;
}) {
  const { kind, path, text, onError } = props;
  const form = useForm(path, text);
  const data = form === undefined ? undefined : ((kind === "project" ? form?.project : form?.environment) ?? null);
  const vars = useVariables(`${props.saved}:${path}`);
  const [more, setMore] = useState(false);
  const apply = useFormChange(path, text, props.onText, onError);
  const change = (c: FieldChange) => apply(c).then(() => undefined);

  if (data === undefined) return <div className="flex-1" aria-busy="true" />;
  if (data === null) {
    return (
      <div className="grid flex-1 place-items-center p-8 text-center text-xs text-muted-foreground" role="alert">
        The {kind === "project" ? "project file" : "environment"} cannot be read as a form; change it in the YAML view.
      </div>
    );
  }
  const env = kind === "environment" ? data.name : null;
  const at = (p: string) => props.problems.filter((x) => x.path === p);
  const info = new Map((vars?.variables ?? []).map((v) => [v.name, v]));

  return (
    <div className="flex-1 overflow-auto bg-background" data-testid="settings-form">
      <div className="mx-auto max-w-[680px] px-7 pt-4 pb-10">
        <div className="mb-1 flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-md text-white" style={{ background: categoryColor[env ? "general" : "flow"] }}>
            {env ? <LinesIcon width={20} height={20} /> : <ProjectIcon width={20} height={20} />}
          </span>
          <div className="min-w-0">
            <h2 className="m-0 text-lg font-semibold">{data.name}</h2>
            <div className="truncate text-xs text-muted-foreground">
              {env ? (
                <>
                  Environment · the values of a run with <span className="font-mono">{env}</span>
                  {vars?.defaultEnvironment === env && " · the default environment"}
                </>
              ) : (
                <>
                  Project{props.projectDir && <> · <span className="font-mono">{props.projectDir}</span></>}
                </>
              )}
            </div>
          </div>
        </div>
        <label className="mb-3 ml-12 flex items-center gap-2 text-xs text-muted-foreground">
          <input
            aria-label="Description"
            defaultValue={data.description ?? ""}
            key={data.description ?? ""}
            placeholder={env ? "What the environment is for, e.g. the production server" : "What the project does"}
            onBlur={(e) => {
              const v = e.target.value.trim();
              if (v !== (data.description ?? "")) void change({ field: "description", value: v === "" ? null : v });
            }}
            className="min-w-0 flex-1 border-0 border-b border-transparent bg-transparent py-0.5 text-xs text-muted-foreground outline-none hover:border-border focus:border-ring"
          />
          <span className="font-mono text-[10px]">description</span>
        </label>

        <Section title={env ? "Environment" : "Project"}>
          {env ? (
            <Field label="Name" fieldKey="name" help="The name of the file; rename it in the explorer">
              {() => <div className="flex h-[30px] items-center rounded-md border border-dashed border-border px-2 font-mono text-xs text-muted-foreground">{env}</div>}
            </Field>
          ) : (
            <>
              <Field label="Name" fieldKey="name" required problems={at("$.name")}>
                {(f) => <TextInput {...f} mono value={data.name} onCommit={(v) => v.trim() && void change({ field: "name", value: v.trim() })} />}
              </Field>
              <Field label="Default environment" fieldKey="defaultEnvironment" problems={at("$.defaultEnvironment")} help="Used when a run names none; the environments of environments/">
                {(f) => (
                  <select
                    id={f.id}
                    value={data.defaultEnvironment ?? ""}
                    onChange={(e) => void change({ field: "defaultEnvironment", value: e.target.value || null })}
                    className={inputClass(f.invalid, true) + " cursor-pointer"}
                  >
                    <option value="">None</option>
                    {[...new Set([...(vars?.environments ?? []), ...(data.defaultEnvironment ? [data.defaultEnvironment] : [])])].map((e) => (
                      <option key={e} value={e}>
                        {e}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
              <OnErrorField
                kind="project"
                value={typeof data.settings?.onError === "string" ? data.settings.onError : undefined}
                choices={{ workflows: props.workflows ?? [] }}
                problems={at("$.settings.onError")}
                onSet={(value) => void change({ field: "settings.onError", value })}
              />
            </>
          )}
        </Section>

        <Variables data={data} env={env} info={info} environments={vars?.environments ?? []} problems={props.problems} onChange={change} />

        <Section title="Secrets" count={data.secrets.length || undefined}>
          <Field
            label="Secret store"
            fieldKey="secretStore"
            problems={at("$.secretStore")}
            help={env ? `Where \${secret:NAME} is read from with ${env}; empty: that of project.yaml` : "For all environments that set none; empty: local"}
          >
            {(f) => (
              <select id={f.id} value={data.secretStore ?? ""} onChange={(e) => void change({ field: "secretStore", value: e.target.value || null })} className={inputClass(f.invalid) + " cursor-pointer"}>
                <option value="">{env ? "As in project.yaml" : "Not set (local)"}</option>
                {stores.map(([v, label]) => (
                  <option key={v} value={v}>
                    {label}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {(data.secrets.length > 0 || data.secretStore === "inline") && <InlineSecrets data={data} file={env ? "this environment" : "project.yaml"} onChange={change} />}
        </Section>

        {!env && (
          <Section title="Defaults and limits" folded={!more} onToggle={() => setMore((m) => !m)} summary="workflow.defaultJoin, settings.maxConcurrentRuns">
            <Field label="Join of all workflows" fieldKey="workflow.defaultJoin" problems={at("$.workflow.defaultJoin")} help="When an action with several incoming links starts, if it sets no join">
              {(f) => (
                <select
                  id={f.id}
                  value={String(data.workflow?.defaultJoin ?? "")}
                  onChange={(e) => void change({ field: "workflow.defaultJoin", value: e.target.value || null })}
                  className={inputClass(f.invalid) + " cursor-pointer"}
                >
                  <option value="">Not set (all)</option>
                  <option value="all">all – every incoming link decided</option>
                  <option value="first">first – the first link that arrives</option>
                  <option value="each">each – once for each link</option>
                </select>
              )}
            </Field>
            <Field label="Runs at the same time" fieldKey="settings.maxConcurrentRuns" problems={at("$.settings.maxConcurrentRuns")} help="Most runs of the project on the server; empty or 0: no limit">
              {(f) => (
                <TextInput
                  {...f}
                  mono
                  value={data.settings?.maxConcurrentRuns === undefined ? "" : String(data.settings.maxConcurrentRuns)}
                  placeholder="0"
                  onCommit={(v) => {
                    const n = Number(v.trim());
                    if (v.trim() === "") void change({ field: "settings.maxConcurrentRuns", value: null });
                    else if (Number.isInteger(n) && n >= 0) void change({ field: "settings.maxConcurrentRuns", value: n });
                    else onError("Runs at the same time: enter a whole number, 0 for no limit");
                  }}
                />
              )}
            </Field>
          </Section>
        )}

        {!env && <HistorySection data={data} problems={props.problems} onChange={change} onError={onError} />}
      </div>
    </div>
  );
}

/**
 * settings.history of project.yaml (S26b, S26c) in the form (S26e1): which runs of runnel run
 * are kept and how long ended runs stay. The three fields are written together as a mapping;
 * all empty removes it.
 */
type HistoryValues = { cli?: string; keepDays?: number; maxRuns?: number };

function HistorySection(props: { data: SettingsData; problems: Problem[]; onChange: (c: FieldChange) => Promise<void>; onError: (message: string) => void }) {
  // Open when project.yaml sets something.
  const [open, setOpen] = useState(() => props.data.settings?.history !== undefined);
  const h = (props.data.settings?.history ?? {}) as HistoryValues;
  // The values with the changes not yet answered by the server: a change right after
  // another one keeps the first.
  const latest = useRef(h);
  useEffect(() => {
    latest.current = h;
  }, [props.data]); // h comes from props.data
  const at = (p: string) => props.problems.filter((x) => x.path === p || !!x.path?.startsWith(p + "."));
  const set = (key: keyof HistoryValues, value: string | number | undefined) => {
    const next = { ...latest.current, [key]: value };
    latest.current = next;
    const map = (["cli", "keepDays", "maxRuns"] as const).filter((k) => next[k] !== undefined && next[k] !== "").map((k) => ({ key: k, value: next[k]! }));
    void props.onChange(map.length > 0 ? { field: "settings.history", map } : { field: "settings.history", value: null });
  };
  const number = (key: "keepDays" | "maxRuns", label: string, max: number) => (v: string) => {
    const n = Number(v.trim());
    if (v.trim() === "") set(key, undefined);
    else if (Number.isInteger(n) && n >= 1 && n <= max) set(key, n);
    else props.onError(`${label}: enter a whole number from 1 to ${max.toLocaleString("en-US")}`);
  };
  return (
    <Section title="Run history" folded={!open} onToggle={() => setOpen((o) => !o)} summary="settings.history">
      <Field label="Runs of runnel run" fieldKey="settings.history.cli" problems={at("$.settings.history.cli")} help="Which runs started on the command line are kept; --history overrides it">
        {(f) => (
          <select id={f.id} value={h.cli ?? ""} onChange={(e) => set("cli", e.target.value || undefined)} className={inputClass(f.invalid) + " cursor-pointer"}>
            <option value="">Not set (all)</option>
            <option value="all">all – every run</option>
            <option value="errors">errors – only failed runs</option>
            <option value="none">none – no runs</option>
          </select>
        )}
      </Field>
      <Field label="Keep for days" fieldKey="settings.history.keepDays" problems={at("$.settings.history.keepDays")} help="Ended runs older than this are removed; empty: 30">
        {(f) => <TextInput {...f} mono value={h.keepDays === undefined ? "" : String(h.keepDays)} placeholder="30" onCommit={number("keepDays", "Keep for days", 3650)} />}
      </Field>
      <Field label="Most runs" fieldKey="settings.history.maxRuns" problems={at("$.settings.history.maxRuns")} help="Only the newest ended runs are kept; empty: 10,000">
        {(f) => <TextInput {...f} mono value={h.maxRuns === undefined ? "" : String(h.maxRuns)} placeholder="10000" onCommit={number("maxRuns", "Most runs", 1_000_000)} />}
      </Field>
    </Section>
  );
}

function Section(props: { title: string; count?: number; folded?: boolean; onToggle?: () => void; summary?: string; children: React.ReactNode }) {
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
        {props.count !== undefined && <span className="text-2xs font-normal text-muted-foreground">{props.count}</span>}
        {props.folded && <span className="text-2xs font-normal text-muted-foreground">{props.summary}</span>}
      </h3>
      {!props.folded && <div className="-mx-4">{props.children}</div>}
    </section>
  );
}

const uses = (v: VariableInfo | undefined) => {
  const n = v?.uses.filter((u) => !u.parameter).length ?? 0;
  return n === 1 ? "1 use" : `${n} uses`;
};

/** Variables as a table (mockup E20): the value here, beside it the other level. */
function Variables(props: {
  data: SettingsData;
  env: string | null;
  info: Map<string, VariableInfo>;
  environments: string[];
  problems: Problem[];
  onChange: (c: FieldChange) => Promise<void>;
}) {
  const { data, env, info } = props;
  const [adding, setAdding] = useState<string | null>(null);
  const names = Object.keys(data.variables);
  // Used, but set neither here nor elsewhere for this level.
  const missing = [...info.values()].filter((v) => !(v.name in data.variables) && (env ? missingIn(v, env) : !v.project && props.environments.every((e) => !v.environments[e]) && v.uses.some((u) => !u.parameter && !u.default)));
  const inherited = env ? [...info.values()].filter((v) => v.project && !(v.name in data.variables)).map((v) => v.name) : [];
  const addName = adding?.trim() ?? "";
  const addProblem = addName === "" ? null : !namePattern.test(addName) ? "Use letters, digits, “_” and “-”, not starting with a digit" : addName in data.variables ? `${addName} is already set` : null;
  const add = () => {
    if (!addName || addProblem) return;
    void props.onChange({ field: `variables.${addName}`, value: "" }).then(() => setAdding(null));
  };
  return (
    <Section title="Variables" count={names.length}>
      <div className="mb-3 px-4" data-field="variables">
        {names.length + missing.length > 0 && (
          <table className="mb-1.5 w-full border-collapse text-xs" aria-label="Variables">
            <thead>
              <tr className="text-left text-[10px] text-muted-foreground">
                <th className="w-[150px] px-1.5 pb-1 font-semibold">Name</th>
                <th className="px-1.5 pb-1 font-semibold">{env ? `Value in ${env}` : "Value"}</th>
                <th className="w-[170px] px-1.5 pb-1 font-semibold">{env ? "project.yaml" : "Overridden in"}</th>
                <th className="w-7" />
              </tr>
            </thead>
            <tbody>
              {names.map((n) => {
                const v = info.get(n);
                const problems = props.problems.filter((p) => p.path === `$.variables.${n}`);
                const over = env ? null : props.environments.filter((e) => v?.environments[e]);
                return (
                  <tr key={n} className="border-t border-border" data-variable={n}>
                    <td className="px-1.5 py-1 font-mono font-medium">
                      {n}
                      <small className="block font-sans text-[10px] font-normal text-muted-foreground">{uses(v)}</small>
                    </td>
                    <td className="px-1.5 py-1">
                      <TextInput id={`variable-${n}`} invalid={problems.length > 0} mono value={String(data.variables[n] ?? "")} onCommit={(value) => void props.onChange({ field: `variables.${n}`, value })} />
                      {problems.map((p, i) => (
                        <div key={i} role="alert" className="mt-0.5 text-2xs text-destructive">
                          ✖ {p.message}
                        </div>
                      ))}
                    </td>
                    <td className="px-1.5 py-1 font-mono text-[11px] text-muted-foreground">
                      {env ? (
                        v?.project ? (
                          <>
                            <s>{v.project.value}</s> <Tag>overridden</Tag>
                          </>
                        ) : (
                          "–"
                        )
                      ) : over && over.length > 0 ? (
                        over.map((e) => <Tag key={e}>{e}</Tag>)
                      ) : (
                        "–"
                      )}
                    </td>
                    <td className="px-1.5 py-1 text-right">
                      <button
                        type="button"
                        aria-label={`Remove ${n}`}
                        title={`Remove ${n}`}
                        onClick={() => void props.onChange({ field: `variables.${n}`, value: null })}
                        className="inline-grid size-6 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground"
                      >
                        <TrashIcon width={13} height={13} />
                      </button>
                    </td>
                  </tr>
                );
              })}
              {missing.map((v) => (
                <tr key={v.name} className="border-t border-border bg-warning-soft" data-variable={v.name}>
                  <td className="px-1.5 py-1 font-mono font-medium text-warning">
                    {v.name}
                    <small className="block font-sans text-[10px] font-normal text-muted-foreground">{uses(v)} · not set</small>
                  </td>
                  <td className="px-1.5 py-1" colSpan={3}>
                    <button
                      type="button"
                      onClick={() => void props.onChange({ field: `variables.${v.name}`, value: "" })}
                      className="inline-flex h-6 cursor-pointer items-center gap-1 rounded-md border border-input bg-background px-2 text-2xs font-medium hover:bg-muted"
                    >
                      <PlusIcon width={12} height={12} />
                      {env ? `Set for ${env}` : "Set in project.yaml"}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {adding === null ? (
          <button type="button" onClick={() => setAdding("")} className="inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent py-1 text-xs font-medium text-primary">
            <PlusIcon width={13} height={13} />
            Add variable
          </button>
        ) : (
          <div className="flex items-center gap-2">
            <input
              autoFocus
              aria-label="Name of the new variable"
              value={adding}
              placeholder="NAME"
              onChange={(e) => setAdding(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") add();
                if (e.key === "Escape") setAdding(null);
              }}
              aria-invalid={!!addProblem}
              className={inputClass(!!addProblem, true) + " max-w-[220px]"}
            />
            <button type="button" disabled={!addName || !!addProblem} onClick={add} className="inline-flex h-[30px] cursor-pointer items-center rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted disabled:cursor-default disabled:opacity-50">
              Add
            </button>
            {addProblem && <span className="text-2xs text-destructive">✖ {addProblem}</span>}
          </div>
        )}
        {inherited.length > 0 && (
          <div className="mt-1 text-2xs text-muted-foreground">
            Variables of project.yaml that {env} does not set apply unchanged: <span className="font-mono">{inherited.join(", ")}</span>
          </div>
        )}
      </div>
    </Section>
  );
}

function Tag(props: { children: React.ReactNode }) {
  return <span className="mr-1 inline-flex h-[18px] items-center rounded-full bg-expression-soft px-1.5 font-sans text-[10px] font-semibold text-expression">{props.children}</span>;
}

/** The secrets of secretStore: inline – names only, values are never shown (mockup E20). */
function InlineSecrets(props: { data: SettingsData; file: string; onChange: (c: FieldChange) => Promise<void> }) {
  const [adding, setAdding] = useState<{ name: string; value: string } | null>(null);
  const valid = adding !== null && namePattern.test(adding.name) && !props.data.secrets.includes(adding.name) && adding.value !== "";
  return (
    <div className="mb-3 px-4" data-field="secrets">
      <div className="mb-1 flex items-baseline text-xs font-medium">
        Secrets in {props.file}
        <span className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">secrets</span>
      </div>
      {props.data.secrets.length > 0 && (
        <table className="mb-1.5 w-full border-collapse text-xs" aria-label="Secrets">
          <tbody>
            {props.data.secrets.map((n) => (
              <tr key={n} className="border-t border-border" data-secret={n}>
                <td className="w-[150px] px-1.5 py-1 font-mono font-medium">{n}</td>
                <td className="px-1.5 py-1">
                  <div className="flex h-[26px] items-center rounded-md border border-input bg-muted px-2 font-mono text-xs text-muted-foreground">
                    ••••••••<span className="ml-auto font-sans text-[10px]">hidden in the form</span>
                  </div>
                </td>
                <td className="w-7 px-1.5 py-1 text-right">
                  <button
                    type="button"
                    aria-label={`Remove secret ${n}`}
                    onClick={() => void props.onChange({ field: `secrets.${n}`, value: null })}
                    className="inline-grid size-6 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground"
                  >
                    <TrashIcon width={13} height={13} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {props.data.secrets.length > 0 && (
        <div role="note" className="mb-1.5 flex gap-1.5 rounded-sm bg-danger-soft px-2 py-1.5 text-2xs text-destructive">
          ⚠
          <span>
            <b className="font-semibold text-foreground">Plain text in {props.file}.</b> The values are in the file and in Git; use them only for development. The form shows names only.
          </span>
        </div>
      )}
      {adding === null ? (
        <button type="button" onClick={() => setAdding({ name: "", value: "" })} className="inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent py-1 text-xs font-medium text-primary">
          <PlusIcon width={13} height={13} />
          Add secret
        </button>
      ) : (
        <div className="flex items-center gap-2">
          <input autoFocus aria-label="Name of the new secret" placeholder="name" value={adding.name} onChange={(e) => setAdding({ ...adding, name: e.target.value.trim() })} className={inputClass(false, true) + " max-w-[180px]"} />
          <input
            type="password"
            autoComplete="off"
            aria-label="Value of the new secret"
            placeholder="value"
            value={adding.value}
            onChange={(e) => setAdding({ ...adding, value: e.target.value })}
            className={inputClass(false, true)}
          />
          <button
            type="button"
            disabled={!valid}
            onClick={() => void props.onChange({ field: `secrets.${adding.name}`, value: adding.value }).then(() => setAdding(null))}
            className="inline-flex h-[30px] cursor-pointer items-center rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted disabled:cursor-default disabled:opacity-50"
          >
            Add
          </button>
        </div>
      )}
    </div>
  );
}
