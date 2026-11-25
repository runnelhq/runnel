import { useEffect, useId, useState, type DragEvent, type ReactNode } from "react";
import type { Schema } from "../catalog";
import type { ExprContext } from "../expressions";
import type { KeyValue } from "../workflow";
import { ExpressionInput } from "./ExpressionInput";
import { ChevronIcon, PlusIcon } from "./Icons";

type Item = Record<string, unknown>;

/** What the panel knows around the list (E14b): fields of the incoming records, targets of the data links. */
export interface ListContext {
  ctx: ExprContext;
  /** Fields of the incoming records; null while unknown. */
  fields: { name: string; type?: string }[] | null;
  /** Ids of the transforms the data links lead to. */
  targets: string[];
  /** Fields of the records on the info link (lookup data, S23b); null while unknown or without one. */
  infoFields?: { name: string; type?: string }[] | null;
}

/** A problem of an entry, located by its index in the list. */
export interface EntryProblem {
  index: number;
  message: string;
  warning: boolean;
}

/**
 * A list of the configuration in the properties panel (mockup design/mockups/e14-lists.html),
 * built from its JSON Schema: lists of texts as chips; lists of mappings as a table (up to
 * three short properties) or as cards (more or longer properties), with add, remove and
 * reorder. Every change writes the whole list as one step.
 */
export function ListField(props: {
  /** Name of the field, e.g. "fields"; "Add field" is derived from it. */
  name: string;
  labelId: string;
  schema: Schema;
  value: unknown;
  env: ListContext;
  /** Problems of single entries, shown at the entry (E14b). */
  problems?: EntryProblem[];
  onList: (list: KeyValue[][] | null) => void;
  onTexts: (texts: string[] | null) => void;
}) {
  const items = props.schema.items ?? {};
  const noun = props.name.endsWith("s") ? props.name.slice(0, -1).replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase() : "entry";
  if (!items.properties) {
    const texts = Array.isArray(props.value) ? props.value.map((v) => String(v)) : [];
    // The lists of texts so far are lists of field names (groupBy, remove, fields of core.log).
    return (
      <Chips
        labelId={props.labelId}
        texts={texts}
        noun={props.name.endsWith("s") ? noun : "field"}
        fields={items.format === "runnel-field" ? props.env.fields : null}
        onChange={(t) => props.onTexts(t.length > 0 ? t : null)}
      />
    );
  }
  return <Entries {...props} items={items} noun={noun} />;
}

/** A list of texts, e.g. groupBy of core.group-by. */
function Chips(props: {
  labelId: string;
  texts: string[];
  noun: string;
  /** Field names to offer (runnel-field); null: free text only. */
  fields: { name: string; type?: string }[] | null;
  onChange: (texts: string[]) => void;
}) {
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState("");
  const add = () => {
    const t = text.trim();
    if (t) props.onChange([...props.texts, t]);
    setText("");
  };
  return (
    <div role="list" aria-labelledby={props.labelId} className="flex flex-wrap items-center gap-1.5 rounded-md border border-input p-1.5">
      {props.texts.map((t, i) => (
        <span role="listitem" key={`${i}:${t}`} className="inline-flex h-[22px] items-center gap-1 rounded-full bg-muted pr-1 pl-2 font-mono text-xs">
          {t}
          <button
            type="button"
            aria-label={`Remove ${t}`}
            onClick={() => props.onChange(props.texts.filter((_, j) => j !== i))}
            className="cursor-pointer border-0 bg-transparent p-0 text-sm leading-none text-muted-foreground hover:text-foreground"
          >
            ×
          </button>
        </span>
      ))}
      {adding ? (
        props.fields ? (
          <div className="w-40">
            <FieldCombo
              label={`New ${props.noun}`}
              value=""
              fields={props.fields}
              used={new Set(props.texts)}
              autoFocus
              onCommit={(v) => {
                if (v.trim()) props.onChange([...props.texts, v.trim()]);
                setAdding(false);
              }}
            />
          </div>
        ) : (
        <input
          autoFocus
          aria-label={`New ${props.noun}`}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") add();
            if (e.key === "Escape") {
              setText("");
              setAdding(false);
            }
          }}
          onBlur={() => {
            add();
            setAdding(false);
          }}
          className="h-[22px] w-28 rounded-full border border-ring bg-background px-2 font-mono text-xs text-foreground outline-none"
        />
        )
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="inline-flex h-[22px] cursor-pointer items-center gap-1 rounded-full border border-dashed border-border bg-transparent px-2 text-xs text-primary"
        >
          <PlusIcon width={12} height={12} />
          Add {props.noun}
        </button>
      )}
    </div>
  );
}

const isEmpty = (v: unknown) => v === undefined || v === null || v === "";

/** Whether a property applies to an entry (x-runnel-only: only for some values of a sibling). */
function applies(s: Schema, item: Item): boolean {
  const only = s["x-runnel-only"];
  return !only || Object.entries(only).every(([k, values]) => values.some((v) => String(v) === String(item[k] ?? "")));
}

/**
 * The value written for a text typed into a property: a number for number types; for
 * properties of any type a number or true/false if it reads as one (as in YAML).
 */
export function typedValue(s: Schema, text: string): unknown {
  if (text === "") return "";
  if ((s.type === "integer" || s.type === "number") && !text.includes("${") && !text.includes("{{")) {
    const n = Number(text);
    return Number.isNaN(n) ? text : n;
  }
  if (s.type === undefined) {
    if (/^-?(0|[1-9]\d*)(\.\d+)?$/.test(text)) return Number(text);
    if (text === "true" || text === "false") return text === "true";
  }
  return text;
}

/** An entry as written to the file: properties in the order of the schema, empty and default values left out. */
export function toKeyValues(item: Item, props: [string, Schema][]): KeyValue[] {
  const known = new Set(props.map(([k]) => k));
  const out: KeyValue[] = [];
  for (const [k, s] of props) {
    const v = item[k];
    if (isEmpty(v) || !applies(s, item) || (s.type === "boolean" && v === (s.default ?? false))) continue;
    out.push({ key: k, value: v as string | number | boolean });
  }
  // Keys the schema does not know stay as they are (the check reports them).
  for (const [k, v] of Object.entries(item)) {
    if (!known.has(k) && !isEmpty(v) && ["string", "number", "boolean"].includes(typeof v)) out.push({ key: k, value: v as string | number | boolean });
  }
  return out;
}

function Entries(props: {
  name: string;
  labelId: string;
  items: Schema;
  value: unknown;
  env: ListContext;
  problems?: EntryProblem[];
  noun: string;
  onList: (list: KeyValue[][] | null) => void;
}) {
  const schemaProps = Object.entries(props.items.properties ?? {});
  const required = new Set(props.items.required ?? []);
  const entries: Item[] = Array.isArray(props.value) ? props.value.map((v) => (v && typeof v === "object" ? (v as Item) : {})) : [];
  // A new entry stays here until its required properties are filled in.
  const [pending, setPending] = useState<Item | null>(null);
  const [open, setOpen] = useState<number | "new" | null>(null);
  const [dragged, setDragged] = useState<number | null>(null);
  const table = schemaProps.length <= 3 && schemaProps.every(([, s]) => s.format !== "runnel-text" && !s["x-runnel-only"]);

  const rowProblems = (i: number) => (props.problems ?? []).filter((p) => p.index === i);
  /** Values of a property in the other entries, e.g. fields converted above. */
  const used = (k: string, i: number | "new") => new Set(entries.flatMap((e, j) => (j !== i && !isEmpty(e[k]) ? [String(e[k])] : [])));
  const commit = (list: Item[]) => props.onList(list.length > 0 ? list.map((item) => toKeyValues(item, schemaProps)) : null);
  const complete = (item: Item) => schemaProps.every(([k]) => !required.has(k) || !isEmpty(item[k]));
  const change = (i: number | "new", k: string, v: unknown) => {
    if (i === "new") {
      const next = { ...(pending ?? {}), [k]: v };
      if (complete(next)) {
        setPending(null);
        if (open === "new") setOpen(entries.length);
        commit([...entries, next]);
      } else setPending(next);
      return;
    }
    commit(entries.map((e, j) => (j === i ? { ...e, [k]: v } : e)));
  };
  const remove = (i: number | "new") => {
    if (i === "new") return setPending(null);
    setOpen(null);
    commit(entries.filter((_, j) => j !== i));
  };
  const move = (from: number, to: number) => {
    if (to < 0 || to >= entries.length || from === to) return;
    const next = [...entries];
    const [e] = next.splice(from, 1);
    next.splice(to, 0, e);
    if (open === from) setOpen(to);
    commit(next);
  };
  const add = () => {
    setPending({});
    setOpen("new");
  };

  const rows: { key: number | "new"; item: Item }[] = entries.map((item, i) => ({ key: i, item }));
  if (pending) rows.push({ key: "new", item: pending });

  const handle = (i: number | "new") =>
    i === "new" ? (
      <span className="w-3" />
    ) : (
      <button
        type="button"
        draggable
        aria-label={`Move ${props.noun} ${i + 1} (Alt+↑/↓)`}
        title="Drag to reorder"
        onDragStart={(e) => {
          e.dataTransfer.effectAllowed = "move";
          setDragged(i);
        }}
        onDragEnd={() => setDragged(null)}
        onKeyDown={(e) => {
          if (e.altKey && e.key === "ArrowUp") move(i, i - 1);
          if (e.altKey && e.key === "ArrowDown") move(i, i + 1);
        }}
        className="w-3 cursor-grab border-0 bg-transparent p-0 text-[10px] tracking-[-2px] text-muted-foreground select-none"
      >
        ⋮⋮
      </button>
    );
  const drop = (i: number | "new") =>
    i === "new"
      ? {}
      : {
          onDragOver: (e: DragEvent) => dragged !== null && e.preventDefault(),
          onDrop: (e: DragEvent) => {
            e.preventDefault();
            if (dragged !== null) move(dragged, i);
            setDragged(null);
          },
        };
  const removeButton = (i: number | "new") => (
    <button
      type="button"
      aria-label={i === "new" ? `Remove the new ${props.noun}` : `Remove ${props.noun} ${i + 1}`}
      onClick={() => remove(i)}
      className="cursor-pointer border-0 bg-transparent p-0 text-[15px] leading-none text-muted-foreground hover:text-foreground"
    >
      ×
    </button>
  );
  const addButton = (
    <button
      type="button"
      onClick={add}
      disabled={pending !== null}
      className={
        "flex h-7 w-full cursor-pointer items-center gap-1.5 border-0 bg-transparent px-2.5 text-xs text-primary disabled:cursor-default disabled:opacity-50" +
        (table ? "" : " mt-1.5 rounded-md border border-dashed border-border")
      }
    >
      <PlusIcon width={13} height={13} />
      Add {props.noun}
    </button>
  );

  if (table) {
    const cols = schemaProps.map(([, s]) => (s.type === "boolean" ? "auto" : s.enum ? "88px" : "minmax(0,1fr)")).join(" ");
    return (
      <div role="table" aria-labelledby={props.labelId} className="rounded-md border border-border" data-testid={`list-${props.name}`}>
        <div role="row" className="grid h-6 items-center gap-1.5 border-b border-border px-1.5" style={{ gridTemplateColumns: `12px ${cols} 14px` }}>
          <span />
          {schemaProps.map(([k]) => (
            <span role="columnheader" key={k} id={`${props.labelId}-${k}`} className="truncate text-[10px] font-semibold tracking-wide text-muted-foreground uppercase">
              {k.replace(/([a-z])([A-Z])/g, "$1 $2")}
              {required.has(k) && <span className="text-destructive">*</span>}
            </span>
          ))}
          <span />
        </div>
        {rows.map(({ key, item }) => (
          <div
            role="row"
            aria-label={key === "new" ? `New ${props.noun}` : `${props.noun} ${key + 1}`}
            key={key}
            {...drop(key)}
            className={"grid items-center gap-1.5 border-b border-border px-1.5 py-1" + (key === "new" ? " bg-accent/45" : "") + (dragged === key ? " opacity-50" : "")}
            style={{ gridTemplateColumns: `12px ${cols} 14px` }}
          >
            {handle(key)}
            {schemaProps.map(([k, s]) => (
              <Cell key={k} name={k} schema={s} value={item[k]} labelId={`${props.labelId}-${k}`} env={props.env} used={used(k, key)} compact onChange={(v) => change(key, k, v)} />
            ))}
            {removeButton(key)}
            {key !== "new" && <RowProblems problems={rowProblems(key)} className="col-span-full" />}
          </div>
        ))}
        {addButton}
      </div>
    );
  }

  return (
    <div role="list" aria-labelledby={props.labelId} data-testid={`list-${props.name}`}>
      {rows.map(({ key, item }) => {
        const isOpen = open === key;
        const title = String(item[schemaProps[0]?.[0]] ?? "") || (key === "new" ? `New ${props.noun}` : `${props.noun} ${key + 1}`);
        return (
          <div
            role="listitem"
            aria-label={key === "new" ? `New ${props.noun}` : `${props.noun} ${key + 1}`}
            key={key}
            {...drop(key)}
            className={"mb-1.5 rounded-md border bg-card " + (isOpen ? "border-ring" : "border-border") + (dragged === key ? " opacity-50" : "")}
          >
            <div className={"grid min-h-[30px] grid-cols-[12px_18px_1fr_14px] items-center gap-1.5 px-2 text-xs" + (isOpen ? " border-b border-border" : "")}>
              {handle(key)}
              <span className="grid size-[18px] place-items-center rounded-full bg-muted text-[10px] text-muted-foreground">{key === "new" ? "+" : key + 1}</span>
              {isOpen ? (
                // A click on the title folds the card again (without leaving the panel).
                <button
                  type="button"
                  aria-expanded
                  title="Collapse"
                  onClick={() => setOpen(null)}
                  className="flex min-w-0 cursor-pointer items-center gap-1 border-0 bg-transparent p-0 text-left text-xs text-foreground"
                >
                  <b className="truncate font-semibold">{title}</b>
                  <ChevronIcon open width={12} height={12} className="ml-auto shrink-0 text-muted-foreground" />
                </button>
              ) : (
                <button
                  type="button"
                  aria-expanded={false}
                  onClick={() => setOpen(key)}
                  className="flex min-w-0 cursor-pointer items-center gap-1 border-0 bg-transparent p-0 text-left text-xs text-foreground"
                >
                  <span className="truncate">{summary(item, schemaProps)}</span>
                  <ChevronIcon open={false} width={12} height={12} className="ml-auto shrink-0 text-muted-foreground" />
                </button>
              )}
              {isOpen ? removeButton(key) : <span />}
            </div>
            {isOpen && (
              <div className="px-2.5 pt-2">
                {schemaProps
                  .filter(([, s]) => applies(s, item))
                  .map(([k, s]) => (
                    <SubField key={k} id={`${props.labelId}-${key}-${k}`} name={k} schema={s} required={required.has(k)}>
                      <Cell name={k} schema={s} value={item[k]} labelId={`${props.labelId}-${key}-${k}`} env={props.env} used={used(k, key)} onChange={(v) => change(key, k, v)} />
                    </SubField>
                  ))}
              </div>
            )}
          </div>
        );
      })}
      {addButton}
    </div>
  );
}

/** A collapsed card: the first value in bold, then the other values. */
function summary(item: Item, props: [string, Schema][]): ReactNode {
  const values = props.filter(([k, s]) => !isEmpty(item[k]) && applies(s, item) && s.type !== "boolean").map(([k]) => String(item[k]));
  const flags = props.filter(([k, s]) => s.type === "boolean" && item[k] === true).map(([k]) => k);
  const [first, ...rest] = [...values, ...flags];
  if (first === undefined) return <span className="text-muted-foreground italic">empty</span>;
  return (
    <>
      <b className="font-mono font-semibold">{first}</b>
      {rest.length > 0 && <span className="text-muted-foreground"> · {rest.join(" · ")}</span>}
    </>
  );
}

function SubField(props: { id: string; name: string; schema: Schema; required: boolean; children: ReactNode }) {
  const label = props.name.charAt(0).toUpperCase() + props.name.slice(1);
  return (
    <div className="mb-2">
      <div className="mb-[3px] flex gap-1 text-2xs font-medium">
        <span id={props.id}>{label}</span>
        {props.required && <span className="text-destructive">*</span>}
        <span className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">{props.name}</span>
      </div>
      {props.children}
      {props.schema.description && <div className="mt-1 text-2xs text-muted-foreground">{props.schema.description}</div>}
    </div>
  );
}

/** One property of an entry: a choice for fixed values, a box for true/false, otherwise a text with expressions. */
function Cell(props: {
  name: string;
  schema: Schema;
  value: unknown;
  labelId: string;
  env: ListContext;
  /** Values of this property in the other entries. */
  used: Set<string>;
  compact?: boolean;
  onChange: (value: unknown) => void;
}) {
  const s = props.schema;
  const field = "h-[26px] w-full min-w-0 rounded-md border border-input bg-background px-1.5 text-xs text-foreground";
  if (s.enum && s.enum.length > 0) {
    const current = isEmpty(props.value) ? "" : String(props.value);
    return (
      <select aria-labelledby={props.labelId} value={current} onChange={(e) => props.onChange(e.target.value)} className={field + (current ? "" : " text-muted-foreground")}>
        <option value="">{s.default !== undefined ? `${String(s.default)} (default)` : props.name}</option>
        {s.enum.map((v) => (
          <option key={String(v)} value={String(v)}>
            {String(v)}
          </option>
        ))}
      </select>
    );
  }
  if (s.type === "boolean") {
    return (
      <input
        type="checkbox"
        aria-labelledby={props.labelId}
        checked={props.value === true}
        onChange={(e) => props.onChange(e.target.checked)}
        className="size-4 justify-self-center accent-primary"
      />
    );
  }
  const text = isEmpty(props.value) ? "" : String(props.value);
  // Ids of transforms a data link leads to (runnel-target, e.g. "to" of core.switch).
  if (s.format === "runnel-target") {
    const options = props.env.targets.includes(text) || text === "" ? props.env.targets : [...props.env.targets, text];
    return (
      <select aria-labelledby={props.labelId} value={text} onChange={(e) => props.onChange(e.target.value)} className={field + " font-mono" + (text ? "" : " text-muted-foreground")}>
        <option value="">{props.name}</option>
        {options.map((t) => (
          <option key={t} value={t}>
            {props.env.targets.includes(t) ? t : `${t} (no data link)`}
          </option>
        ))}
      </select>
    );
  }
  // Names of fields of the incoming records (runnel-field).
  if (s.format === "runnel-field" && props.env.fields) {
    return <FieldCombo labelledBy={props.labelId} value={text} fields={props.env.fields} used={props.used} compact={props.compact} onCommit={(v) => props.onChange(v)} />;
  }
  // Names of fields of the lookup data on the info link (runnel-info-field, mockup S23b).
  if (s.format === "runnel-info-field" && props.env.infoFields) {
    return <FieldCombo labelledBy={props.labelId} value={text} fields={props.env.infoFields} used={props.used} compact={props.compact} onCommit={(v) => props.onChange(v)} />;
  }
  return (
    <div className={props.compact ? "runnel-expr-sm min-w-0" : "min-w-0"}>
      <ExpressionInput
        labelId={props.labelId}
        value={text}
        invalid={false}
        // In a table the column says what goes in; an example would not fit.
        placeholder={!props.compact && s.examples?.[0] !== undefined ? `e.g. ${String(s.examples[0])}` : props.name}
        multiline={s.format === "runnel-text"}
        problems={[]}
        ctx={props.env.ctx}
        onCommit={(v) => props.onChange(typedValue(s, v))}
      />
    </div>
  );
}

/** Problems of an entry below it, with the suggestion of the check ("did you mean …?"). */
function RowProblems(props: { problems: EntryProblem[]; className?: string }) {
  if (props.problems.length === 0) return null;
  return (
    <div className={(props.className ?? "") + " text-2xs"} data-testid="entry-problems">
      {props.problems.map((p, i) => (
        <div key={i} className={p.warning ? "text-warning" : "text-destructive"}>
          {p.warning ? "⚠" : "✖"} {p.message}
        </div>
      ))}
    </div>
  );
}

/**
 * A field name with the fields of the incoming records to choose from (mockup E14, state
 * "convert"): fields used in other entries are marked; a name that is not listed can be typed.
 */
function FieldCombo(props: {
  labelledBy?: string;
  label?: string;
  value: string;
  fields: { name: string; type?: string }[];
  used: Set<string>;
  compact?: boolean;
  autoFocus?: boolean;
  onCommit: (value: string) => void;
}) {
  const listId = useId();
  const [text, setText] = useState(props.value);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  useEffect(() => setText(props.value), [props.value]);
  // While the name is unchanged all fields are offered, after typing those that contain it.
  const filter = text === props.value ? "" : text.toLowerCase();
  const shown = props.fields.filter((f) => f.name.toLowerCase().includes(filter));
  const commit = (v: string) => {
    setOpen(false);
    setActive(-1);
    setText(v);
    if (v !== props.value || props.autoFocus) props.onCommit(v);
  };
  return (
    <div className="relative min-w-0">
      <input
        role="combobox"
        aria-labelledby={props.labelledBy}
        aria-label={props.label}
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        autoFocus={props.autoFocus}
        value={text}
        placeholder="field"
        onFocus={() => setOpen(true)}
        onChange={(e) => {
          setText(e.target.value);
          setOpen(true);
          setActive(-1);
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" || e.key === "ArrowUp") {
            e.preventDefault();
            setOpen(true);
            const step = e.key === "ArrowDown" ? 1 : -1;
            setActive((a) => (shown.length === 0 ? -1 : (a + step + shown.length) % shown.length));
          }
          if (e.key === "Enter") {
            e.preventDefault();
            commit(open && active >= 0 && shown[active] ? shown[active].name : text.trim());
          }
          if (e.key === "Escape") {
            setText(props.value);
            setOpen(false);
          }
        }}
        onBlur={() => commit(text.trim())}
        className={
          "w-full min-w-0 rounded-md border border-input bg-background px-1.5 font-mono text-xs text-foreground outline-none focus:border-ring " +
          (props.compact ? "h-[26px]" : "h-[30px]")
        }
      />
      {open && shown.length > 0 && (
        <div
          id={listId}
          role="listbox"
          aria-label="Fields of the incoming records"
          className="absolute top-[calc(100%+2px)] right-0 left-0 z-20 min-w-44 rounded-md border border-border bg-popover text-xs shadow-md"
        >
          <div className="px-2.5 pt-1.5 pb-1 text-[10px] text-muted-foreground">Fields of the incoming records</div>
          {shown.map((f, i) => (
            <div
              key={f.name}
              role="option"
              aria-selected={i === active}
              onMouseDown={(e) => {
                e.preventDefault();
                commit(f.name);
              }}
              className={
                "flex h-[26px] cursor-pointer items-center justify-between gap-2 px-2.5 " +
                (i === active ? "bg-accent text-accent-foreground" : "hover:bg-muted") +
                (props.used.has(f.name) ? " text-muted-foreground" : "")
              }
            >
              <span className="truncate font-mono">{f.name}</span>
              <span className="font-mono text-[10px] text-muted-foreground">{props.used.has(f.name) ? "used" : (f.type ?? "")}</span>
            </div>
          ))}
          <div className="border-t border-border px-2.5 pt-1 pb-1.5 text-[10px] text-muted-foreground">↑↓ choose · Enter takes it · other names can be typed</div>
        </div>
      )}
    </div>
  );
}
