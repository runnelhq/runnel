import { useEffect, useMemo, useRef, useState } from "react";
import type { Schema } from "../catalog";
import {
  fieldLines,
  missingIn,
  placeholder,
  shortText,
  typeLabel,
  typesOf,
  useSchema,
  type ReferenceKind,
  type VariableInfo,
  type VariablesInfo,
} from "../reference";
import { ChevronIcon, SearchIcon } from "./Icons";

/** The open file the panel helps with. */
export interface ReferenceTarget {
  path: string;
  kind: ReferenceKind;
  /** The current text of the file, also unsaved: which fields it has. */
  text: string;
}

/** What the panel asks the editor to do. */
export type ReferenceAction =
  | { op: "goto"; path: string; line: number }
  | { op: "insert"; path: string; parent: string; key: string; schema: Schema }
  | { op: "addVariable"; path: string; name: string };

/**
 * Lower part of the sidebar for project.yaml, environments and triggers (mockup E18):
 * "Fields" of the file kind from its schema (S58) with Insert, and for project.yaml and
 * environments "Variables" – every ${…} of the project, where it is set and where it is
 * missing (E18a).
 */
export function ReferencePanel(props: {
  target: ReferenceTarget;
  variables: VariablesInfo | null;
  onAction: (a: ReferenceAction) => void;
}) {
  const { target } = props;
  const hasVariables = target.kind !== "trigger";
  // Environments open with Variables, project.yaml with Fields (decision for mockup E18).
  const [tab, setTab] = useState<"fields" | "variables">(target.kind === "environment" ? "variables" : "fields");
  useEffect(() => setTab(target.kind === "environment" ? "variables" : "fields"), [target.path, target.kind]);
  const showVars = hasVariables && tab === "variables";
  const env = target.kind === "environment" ? target.path.replace(/^.*\//, "").replace(/\.ya?ml$/, "") : "";

  return (
    <section aria-label={showVars ? "Variables" : "Reference"} className="flex min-h-[160px] flex-1 flex-col border-t border-sidebar-border">
      <div className="flex h-9 shrink-0 items-center gap-2 pr-3 pl-4">
        <span className="label-caps">{showVars ? "Variables" : "Reference"}</span>
        {hasVariables && (
          <div role="group" aria-label="Help" className="ml-auto inline-flex gap-0.5 rounded-md border border-border bg-muted p-0.5">
            {(["fields", "variables"] as const).map((t) => (
              <button
                key={t}
                type="button"
                aria-pressed={tab === t}
                onClick={() => setTab(t)}
                className={
                  "cursor-pointer rounded-sm border-0 px-[7px] py-0.5 text-2xs " +
                  (tab === t ? "bg-background text-foreground shadow-sm" : "bg-transparent text-muted-foreground")
                }
              >
                {t === "fields" ? "Fields" : "Variables"}
              </button>
            ))}
          </div>
        )}
      </div>
      {showVars ? (
        <Variables info={props.variables} env={env} onAction={props.onAction} />
      ) : (
        <Fields target={target} onAction={props.onAction} />
      )}
    </section>
  );
}

interface Field {
  key: string;
  parent: string;
  schema: Schema;
  required: boolean;
}

function Fields(props: { target: ReferenceTarget; onAction: (a: ReferenceAction) => void }) {
  const { target } = props;
  const schema = useSchema(target.kind);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [others, setOthers] = useState(false);
  const search = useRef<HTMLInputElement>(null);
  const lines = useMemo(() => fieldLines(target.text), [target.text]);
  const triggerType = target.kind === "trigger" ? (/^type:\s*(\w+)/m.exec(target.text)?.[1] ?? "") : "";

  // "/" jumps into the search field, as in the palette.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey) return;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) || t.closest(".cm-editor"))) return;
      e.preventDefault();
      search.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const groups = useMemo(() => {
    if (!schema?.properties) return [];
    const top: Field[] = [];
    const typed: Field[] = [];
    const other: Field[] = [];
    for (const [key, s] of Object.entries(schema.properties)) {
      if (target.kind === "trigger" && key === "with") {
        const req = s.required ?? [];
        for (const [k, ws] of Object.entries(s.properties ?? {})) {
          const f = { key: k, parent: "with", schema: ws, required: req.includes(k) };
          if (typesOf(ws.description).includes(triggerType)) typed.push(f);
          else other.push(f);
        }
        continue;
      }
      top.push({ key, parent: "", schema: s, required: schema.required?.includes(key) ?? false });
    }
    const q = query.trim().toLowerCase();
    const match = (f: Field) => !q || f.key.toLowerCase().includes(q) || (f.schema.description ?? "").toLowerCase().includes(q);
    const name = target.kind === "trigger" ? "All triggers" : target.kind === "project" ? "project.yaml" : "Environment";
    const out: { name: string; fields: Field[]; closed?: boolean }[] = [{ name, fields: top.filter(match) }];
    if (target.kind === "trigger") {
      if (triggerType) out.push({ name: `with: for ${triggerType}`, fields: typed.filter(match) });
      const otherTypes = triggerTypes(triggerType);
      out.push({ name: `Other types: ${otherTypes.join(", ")}`, fields: other.filter(match), closed: !others && !q });
    }
    return out;
  }, [schema, target.kind, triggerType, query, others]);

  const where = target.kind === "trigger" && triggerType ? `${target.path} · type ${triggerType}` : target.path;
  return (
    <>
      <p className="mx-4 mt-[-4px] mb-2 shrink-0 text-2xs text-muted-foreground">
        Fields of <span className="font-mono">{where}</span>
      </p>
      <label className="mx-3 mb-2 flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-input bg-background px-2 text-muted-foreground">
        <SearchIcon width={14} height={14} />
        <input
          ref={search}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setQuery("");
              e.currentTarget.blur();
            }
          }}
          placeholder="Search fields"
          aria-label="Search fields"
          className="min-w-0 flex-1 border-0 bg-transparent text-xs text-foreground outline-none"
        />
        <kbd className="rounded-[3px] border border-border px-1 font-mono text-[10px]">/</kbd>
      </label>
      <div className="min-h-0 flex-1 overflow-auto pb-3">
        {!schema && <p className="mx-4 text-xs text-muted-foreground">Loading the fields…</p>}
        {groups.map((g) => (
          <div key={g.name} role="group" aria-label={g.name}>
            <button
              type="button"
              aria-expanded={!g.closed}
              onClick={() => g.name.startsWith("Other") && setOthers((o) => !o)}
              className={
                "flex h-[26px] w-full items-center gap-1.5 border-0 bg-transparent px-4 text-xs " +
                (g.closed ? "cursor-pointer font-medium text-muted-foreground" : "cursor-default font-semibold text-sidebar-foreground")
              }
            >
              <ChevronIcon open={!g.closed} width={12} height={12} className="text-muted-foreground" />
              {g.name}
              <span className="ml-auto text-2xs font-normal text-muted-foreground">{g.fields.length}</span>
            </button>
            {!g.closed && (
              <ul className="m-0 mb-1 list-none px-2 py-0">
                {g.fields.map((f) => {
                  const id = (f.parent ? f.parent + "." : "") + f.key;
                  return (
                    <FieldItem
                      key={id}
                      field={f}
                      line={lines.get(id)}
                      open={open === id}
                      onOpen={() => setOpen(open === id ? null : id)}
                      onGoto={(line) => props.onAction({ op: "goto", path: target.path, line })}
                      onInsert={() => props.onAction({ op: "insert", path: target.path, parent: f.parent, key: f.key, schema: f.schema })}
                    />
                  );
                })}
              </ul>
            )}
          </div>
        ))}
      </div>
    </>
  );
}

const triggerTypes = (current: string) => ["cron", "interval", "file", "http"].filter((t) => t !== current);

function FieldItem(props: {
  field: Field;
  line: number | undefined;
  open: boolean;
  onOpen: () => void;
  onGoto: (line: number) => void;
  onInsert: () => void;
}) {
  const { field: f, line } = props;
  const s = f.schema;
  const ref = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (props.open) ref.current?.scrollIntoView?.({ block: "nearest" });
  }, [props.open]);
  const head = (
    <span className="flex min-w-0 items-center gap-1.5">
      <span className={"font-mono text-xs " + (line && !props.open ? "font-normal text-muted-foreground" : "font-medium text-foreground")}>{f.key}</span>
      {f.required && <span className="font-bold text-destructive">*</span>}
      {line && !props.open && <span className="text-[10px] text-success">✓ line {line}</span>}
      <span className="ml-auto rounded-sm border border-border px-[5px] text-[10px] text-muted-foreground">{typeLabel(s)}</span>
    </span>
  );
  if (!props.open) {
    return (
      <li ref={ref} data-testid={`field-${f.parent ? f.parent + "." : ""}${f.key}`}>
        <button
          type="button"
          title={s.description}
          onClick={props.onOpen}
          className={"block w-full cursor-pointer rounded-md border-0 bg-transparent px-2 text-left hover:bg-sidebar-accent " + (line ? "py-0.5" : "py-[5px]")}
        >
          {head}
          {!line && <span className="block truncate text-2xs text-muted-foreground">{shortText(s.description)}</span>}
        </button>
      </li>
    );
  }
  const example = s.examples?.[0];
  return (
    <li
      ref={ref}
      data-testid={`field-${f.parent ? f.parent + "." : ""}${f.key}`}
      className="my-0.5 mb-1.5 rounded-md border border-border bg-card px-2 py-[5px] shadow-sm"
    >
      <button type="button" onClick={props.onOpen} className="block w-full cursor-pointer border-0 bg-transparent p-0 text-left">
        {head}
      </button>
      <p className="my-1.5 text-xs leading-snug text-foreground">{s.description}</p>
      <dl className="mb-2 grid grid-cols-[auto_1fr] gap-x-2 gap-y-[3px] text-2xs">
        {s.enum && (
          <>
            <dt className="text-muted-foreground">Allowed</dt>
            <dd className="m-0">
              {s.enum.map((v) => (
                <span key={String(v)} className={"mr-[3px] mb-[3px] inline-block rounded-sm border bg-muted px-[5px] font-mono text-[10px] " + (v === s.default ? "border-primary" : "border-border")}>
                  {String(v)}
                </span>
              ))}
            </dd>
          </>
        )}
        {example !== undefined && (
          <>
            <dt className="text-muted-foreground">Example</dt>
            <dd className="m-0 font-mono text-[10px]">{String(example)}</dd>
          </>
        )}
        {s.default !== undefined && (
          <>
            <dt className="text-muted-foreground">Default</dt>
            <dd className="m-0 font-mono text-[10px]">{String(s.default)}</dd>
          </>
        )}
      </dl>
      {!line && (
        <pre className="m-0 mb-2 overflow-hidden rounded-sm bg-muted px-2 py-1.5 font-mono text-[11px] leading-normal">
          {(f.parent ? `${f.parent}:\n  ` : "") + f.key + ":" + (s.type === "object" || s.type === "array" ? "" : " " + placeholder(s))}
        </pre>
      )}
      <div className="flex items-center gap-1.5">
        {line ? (
          <button type="button" onClick={() => props.onGoto(line)} className="inline-flex h-6 cursor-pointer items-center rounded-md border border-input bg-background px-2 text-2xs font-medium">
            Go to line {line}
          </button>
        ) : (
          <>
            <button type="button" onClick={props.onInsert} className="inline-flex h-6 cursor-pointer items-center rounded-md border border-primary bg-primary px-2 text-2xs font-medium text-primary-foreground">
              + Insert
            </button>
            <small className="text-[10px] text-muted-foreground">at the right place in the file</small>
          </>
        )}
      </div>
    </li>
  );
}

function Variables(props: { info: VariablesInfo | null; env: string; onAction: (a: ReferenceAction) => void }) {
  const { info, env } = props;
  const [open, setOpen] = useState<string | null>(null);
  if (!info) return <p className="mx-4 text-xs text-muted-foreground">Loading the variables…</p>;
  const envs = info.environments;
  const cell = (v: VariableInfo, e: string) => {
    if (v.environments[e]) return <span className="font-bold text-success" title={`set in environments/${e}.yaml`}>●</span>;
    if (missingIn(v, e)) return <span className="font-bold text-warning" title={`used, but not set for ${e}`}>⚠</span>;
    if (v.project) return <span className="text-muted-foreground" title="from project.yaml">↑</span>;
    return <span className="text-muted-foreground" title="not set">–</span>;
  };
  return (
    <>
      <p className="mx-4 mt-[-4px] mb-2 shrink-0 text-2xs text-muted-foreground">
        {env ? (
          <>
            Column <b>{env}</b> is this file
          </>
        ) : (
          <>
            Used as <span className="font-mono">{"${…}"}</span> in the project
          </>
        )}
      </p>
      <div className="min-h-0 flex-1 overflow-auto pb-3">
        <table aria-label="Variables" className="mx-2 w-[calc(100%-16px)] border-collapse text-xs">
          <thead>
            <tr>
              <th className="pb-1 pl-2 text-left text-[10px] font-semibold text-muted-foreground">Variable</th>
              <th className="pb-1 text-[10px] font-semibold text-muted-foreground">project</th>
              {envs.map((e) => (
                <th key={e} className={"pb-1 text-[10px] font-semibold " + (e === env ? "text-foreground" : "text-muted-foreground")}>
                  {e}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {info.variables.map((v) => {
              const uses = v.uses.length;
              return (
                <VariableRow
                  key={v.name}
                  v={v}
                  envs={envs}
                  env={env}
                  cell={cell}
                  uses={uses}
                  open={open === v.name}
                  onOpen={() => setOpen(open === v.name ? null : v.name)}
                  onAction={props.onAction}
                />
              );
            })}
          </tbody>
        </table>
        {info.variables.length === 0 && <p className="mx-4 text-xs text-muted-foreground">The project uses no variables.</p>}
        <p className="mx-4 mt-2 text-[10px] leading-relaxed text-muted-foreground">
          ● set here · ↑ from project.yaml · ⚠ used, but not set · – not set
          {info.truncated && " · large projects: not all files were searched"}
        </p>
      </div>
    </>
  );
}

function VariableRow(props: {
  v: VariableInfo;
  envs: string[];
  env: string;
  cell: (v: VariableInfo, e: string) => React.ReactNode;
  uses: number;
  open: boolean;
  onOpen: () => void;
  onAction: (a: ReferenceAction) => void;
}) {
  const { v, envs, env } = props;
  const cur = (e: string) => (e === env ? "bg-accent/60" : "");
  const missing = envs.filter((e) => missingIn(v, e));
  const addTo = env && missing.includes(env) ? env : missing[0];
  return (
    <>
      <tr data-testid={`variable-${v.name}`} onClick={props.onOpen} className={"cursor-pointer " + (props.open ? "bg-card" : "hover:bg-sidebar-accent")}>
        <td className="border-t border-sidebar-border py-1 pl-2 text-left font-mono font-medium">
          {v.name}
          <small className="block font-sans text-[10px] text-muted-foreground">
            {props.uses === 0 ? "not used" : `${props.uses} ${props.uses === 1 ? "use" : "uses"}`}
          </small>
        </td>
        <td className="border-t border-sidebar-border text-center">
          {v.project ? <span className="font-bold text-success" title="set in project.yaml">●</span> : <span className="text-muted-foreground">–</span>}
        </td>
        {envs.map((e) => (
          <td key={e} className={"border-t border-sidebar-border text-center " + cur(e)}>
            {props.cell(v, e)}
          </td>
        ))}
      </tr>
      {props.open && (
        <tr>
          <td colSpan={envs.length + 2} className="p-0">
            <div className="mb-2 rounded-b-md border border-t-0 border-border bg-card px-2.5 py-2 text-2xs">
              {missing.length > 0 && (
                <div className="mb-1.5 text-warning">
                  ⚠ Not set for <b>{missing.join(", ")}</b>: runs with {missing.length === 1 ? "it" : "them"} fail.
                </div>
              )}
              <div className="mb-2 grid grid-cols-[76px_1fr] gap-x-2 gap-y-0.5">
                <span className="text-muted-foreground">project</span>
                <span className="font-mono text-[10px] break-all">{v.project?.value ?? "–"}</span>
                {envs.map((e) => (
                  <Level key={e} name={e} value={v.environments[e]?.value} missing={missing.includes(e)} />
                ))}
              </div>
              {v.uses.length > 0 && <div className="mb-0.5 text-muted-foreground">Used in</div>}
              <ul className="m-0 mb-2 list-none p-0">
                {v.uses.map((u) => (
                  <li key={`${u.file}:${u.line}:${u.column}`}>
                    <button
                      type="button"
                      onClick={() => props.onAction({ op: "goto", path: u.file, line: u.line })}
                      className="cursor-pointer border-0 bg-transparent p-0 font-mono text-[10px] text-primary underline underline-offset-2"
                    >
                      {u.file}:{u.line}
                    </button>
                    {u.parameter && <span className="text-muted-foreground"> (parameter)</span>}
                  </li>
                ))}
              </ul>
              {addTo && (
                <button
                  type="button"
                  onClick={() => props.onAction({ op: "addVariable", path: `environments/${addTo}.yaml`, name: v.name })}
                  className="inline-flex h-6 cursor-pointer items-center rounded-md border border-primary bg-primary px-2 text-2xs font-medium text-primary-foreground"
                >
                  + Add to {env === addTo ? `${addTo}.yaml` : `environments/${addTo}.yaml`}
                </button>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function Level(props: { name: string; value: string | undefined; missing: boolean }) {
  return (
    <>
      <span className="text-muted-foreground">{props.name}</span>
      <span className="font-mono text-[10px] break-all">{props.value ?? (props.missing ? "missing" : "–")}</span>
    </>
  );
}
