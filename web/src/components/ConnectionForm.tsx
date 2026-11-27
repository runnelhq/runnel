import { useMemo, useState } from "react";
import { apiBase } from "../api";
import { connectionColor, useConnectionTypes, type Schema } from "../catalog";
import { useReference, type ExprContext } from "../expressions";
import { useSchema, useVariables } from "../reference";
import type { Problem } from "../workflow";
import { ChevronIcon, ConnectionIcon, PlugIcon } from "./Icons";
import { useForm, useFormChange, type FieldChange } from "../forms";
import { SchemaField } from "./PropertiesPanel";

/**
 * A connection file as a form (mockup S39b, design/mockups/s39-connections.html): the
 * fields of its type from the schema (x-runnel-variants), in sections (x-runnel-group),
 * only those of the chosen sign-in (x-runnel-only); each change goes through
 * POST /forms/edit, which changes only the line concerned.
 */

/** The answer of POST /connections/{name}/test (S39c1). */
export interface TestResult {
  ok: boolean;
  message: string;
  environment?: string;
  durationMs: number;
}

const noParams: ExprContext["params"] = [];

export function ConnectionForm(props: {
  path: string;
  text: string;
  problems: Problem[];
  result: TestResult | null;
  /** Opens the help of the connection type ("?" next to the type, mockup E15f). */
  onHelp: (type: string) => void;
  onText: (text: string) => void;
  onError: (message: string) => void;
}) {
  const { path, text, onError } = props;
  const form = useForm(path, text);
  const data = form === undefined ? undefined : (form?.connection ?? null);
  const [more, setMore] = useState(false);
  const schema = useSchema("connection");
  const ref = useReference();
  const ctx = useMemo<ExprContext>(() => ({ ref, params: noParams, actions: [] }), [ref]);

  const apply = useFormChange(path, text, props.onText, onError);
  const change = (c: FieldChange) => void apply(c);

  if (data === undefined || !schema) return <div className="flex-1" aria-busy="true" />;
  if (data === null) {
    return (
      <div className="grid flex-1 place-items-center p-8 text-center text-xs text-muted-foreground" role="alert">
        The connection cannot be read as a form; change it in the YAML view.
      </div>
    );
  }
  const variant = schema.properties?.with?.["x-runnel-variants"]?.[data.type];
  const label = variant?.description?.split(": ")[0] ?? data.type;
  const what = variant?.description?.split(": ").slice(1).join(": ") ?? "";

  // Sections in the order of their first field; "More" is folded and comes last.
  const sections = new Map<string, [string, Schema][]>();
  for (const [name, s] of Object.entries(variant?.properties ?? {})) {
    if (!applies(s, data.with, variant)) continue;
    const group = s["x-runnel-group"] ?? "Settings";
    sections.set(group, [...(sections.get(group) ?? []), [name, s]]);
  }
  const order = [...sections.keys()].filter((g) => g !== "More");
  if (sections.has("More")) order.push("More");

  return (
    <div className="flex-1 overflow-auto bg-background" data-testid="connection-form">
      <div className="mx-auto max-w-[680px] px-7 pt-4 pb-10">
        <div className="mb-1 flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-md text-white" style={{ background: connectionColor(data.type) }}>
            <ConnectionIcon type={data.type} width={20} height={20} />
          </span>
          <div className="min-w-0">
            <h2 className="m-0 text-lg font-semibold">{data.name}</h2>
            <div className="text-xs text-muted-foreground">
              {label}
              {what && ` · ${what.charAt(0).toLowerCase()}${what.slice(1)}`}
              {variant && (
                <button
                  type="button"
                  aria-label={`Help for ${label}`}
                  title={`Help for ${label} (F1)`}
                  onClick={() => props.onHelp(data.type)}
                  className="ml-1 inline-grid size-5 cursor-pointer place-items-center rounded-full border border-border bg-transparent align-middle text-xs leading-none font-semibold text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                >
                  ?
                </button>
              )}
            </div>
          </div>
        </div>
        <label className="mb-3 ml-12 flex items-center gap-2 text-xs text-muted-foreground">
          <input
            aria-label="Description"
            defaultValue={data.description ?? ""}
            key={data.description ?? ""}
            placeholder="What the connection is for, e.g. ERP database, read only"
            onBlur={(e) => {
              const v = e.target.value.trim();
              if (v !== (data.description ?? "")) change({ field: "description", value: v === "" ? null : v });
            }}
            className="min-w-0 flex-1 border-0 border-b border-transparent bg-transparent py-0.5 text-xs text-muted-foreground outline-none hover:border-border focus:border-ring"
          />
          <span className="font-mono text-[10px]">description</span>
        </label>
        {props.result && <ResultBanner result={props.result} />}
        {order.map((group) => {
          const folded = group === "More" && !more;
          return (
            <section key={group} className="border-t border-border pt-2.5 pb-1" aria-label={group}>
              <h3 className="m-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">
                <button
                  type="button"
                  aria-expanded={!folded}
                  onClick={() => group === "More" && setMore((m) => !m)}
                  className={"-ml-1 inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent p-0 text-sm " + (folded ? "font-medium text-muted-foreground" : "font-semibold")}
                >
                  <ChevronIcon open={!folded} width={12} height={12} className="text-muted-foreground" />
                  {group}
                </button>
                {folded && <span className="text-2xs font-normal text-muted-foreground">{sections.get(group)!.map(([n]) => n).join(", ")}</span>}
              </h3>
              {!folded && (
                <div className="-mx-4">
                  {sections.get(group)!.map(([name, s]) => (
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
                  ))}
                </div>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}

/** Whether a field applies with the values of its siblings (x-runnel-only, e.g. only=auth:bearer). */
function applies(s: Schema, values: Record<string, unknown>, parent: Schema | undefined): boolean {
  const only = s["x-runnel-only"];
  if (!only) return true;
  return Object.entries(only).every(([sibling, allowed]) => {
    const v = values[sibling] ?? parent?.properties?.[sibling]?.default;
    return allowed.includes(v);
  });
}

function ResultBanner(props: { result: TestResult }) {
  const r = props.result;
  return (
    <div
      role="status"
      data-testid="test-result"
      className={"mb-3 flex items-start gap-2.5 rounded-md px-3 py-2.5 text-xs " + (r.ok ? "bg-success-soft" : "bg-danger-soft")}
    >
      <span className={"font-bold " + (r.ok ? "text-success" : "text-destructive")}>{r.ok ? "✓" : "✕"}</span>
      <div className="min-w-0">
        <div className="font-semibold break-words">{r.ok ? `Connected in ${r.durationMs} ms` : "The test failed"}</div>
        <div className="mt-0.5 text-2xs break-words text-muted-foreground">
          {r.environment ? `Environment ${r.environment} · ` : ""}
          {r.message}
        </div>
      </div>
    </div>
  );
}

/** "Test connection" with the environment beside it, like Run (mockup S39b). */
export function TestButton(props: { name: string; type: string; onResult: (r: TestResult | null) => void }) {
  const vars = useVariables(props.name);
  const t = useConnectionTypes().get(props.type);
  const envs = vars?.environments ?? [];
  const [env, setEnv] = useState("");
  const [busy, setBusy] = useState(false);
  const current = env || vars?.defaultEnvironment || envs[0] || "";
  const run = async () => {
    setBusy(true);
    props.onResult(null);
    try {
      const res = await fetch(`${apiBase}/connections/${encodeURIComponent(props.name)}/test`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ environment: current }),
      });
      const body = await res.json();
      props.onResult(res.ok ? body : { ok: false, message: body.error ?? res.statusText, environment: current, durationMs: 0 });
    } catch (e) {
      props.onResult({ ok: false, message: (e as Error).message, environment: current, durationMs: 0 });
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="inline-flex">
      <button
        type="button"
        disabled={busy}
        onClick={() => void run()}
        // What the test checks, from the catalog (mockup E15f).
        title={
          `Test the saved connection${current ? ` with the environment ${current}` : ""}` +
          (t?.test ? `: ${t.test.charAt(0).toLowerCase()}${t.test.slice(1)}` : "") +
          (t ? `\n? next to the type or F1 for the help of ${t.label}` : "")
        }
        className={
          "inline-flex h-7 cursor-pointer items-center gap-1.5 border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted disabled:cursor-default " +
          (envs.length > 0 ? "rounded-l-md" : "rounded-md")
        }
      >
        {busy ? <span className="size-3 animate-spin rounded-full border-2 border-border border-t-primary motion-reduce:animate-none" /> : <PlugIcon width={14} height={14} />}
        Test connection
      </button>
      {envs.length > 0 && (
        <select
          aria-label="Environment of the test"
          value={current}
          onChange={(e) => setEnv(e.target.value)}
          className="h-7 cursor-pointer rounded-r-md border border-l-0 border-input bg-background px-1.5 text-xs"
        >
          {envs.map((e) => (
            <option key={e} value={e}>
              {e}
            </option>
          ))}
        </select>
      )}
    </span>
  );
}
