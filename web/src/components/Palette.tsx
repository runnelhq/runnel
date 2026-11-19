import { useEffect, useMemo, useRef, useState } from "react";
import { categoryColor, categoryLabel, paletteDrag, paletteMime, useCatalog, type Descriptor } from "../catalog";
import { ActionIcon, ChevronIcon, SearchIcon } from "./Icons";

/** What the palette needs to know about the open file. */
export interface PaletteTarget {
  /** The active file is a workflow shown as an editable canvas. */
  editable: boolean;
  /** The workflow already has a start action (a second one is not allowed). */
  hasStart: boolean;
}

/**
 * Palette below the explorer (mockup E6b): the action types of the catalog by category,
 * with a search field ("/" jumps into it). Entries are dragged onto the canvas.
 */
export function Palette(props: { target: PaletteTarget | null }) {
  const catalog = useCatalog();
  const [query, setQuery] = useState("");
  const [closed, setClosed] = useState<Set<string>>(new Set());
  const search = useRef<HTMLInputElement>(null);

  // "/" jumps into the search field, unless the user is typing somewhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey) return;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return;
      e.preventDefault();
      search.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase();
    const match = (d: Descriptor) =>
      !q || [d.type, d.label, d.description].some((s) => s.toLowerCase().includes(q));
    const byCategory = new Map<string, Descriptor[]>();
    for (const d of [...catalog.values()].filter(match).sort((a, b) => a.label.localeCompare(b.label))) {
      byCategory.set(d.category, [...(byCategory.get(d.category) ?? []), d]);
    }
    const order = Object.keys(categoryLabel);
    return [...byCategory.entries()].sort(([a], [b]) => (order.indexOf(a) + 1 || 99) - (order.indexOf(b) + 1 || 99));
  }, [catalog, query]);

  const editable = props.target?.editable ?? false;

  return (
    <section aria-label="Palette" className="flex min-h-[160px] flex-1 flex-col border-t border-sidebar-border pt-0">
      <div className="flex h-9 shrink-0 items-center pl-4">
        <span className="label-caps">Palette</span>
      </div>
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
          placeholder="Search actions"
          aria-label="Search actions"
          className="min-w-0 flex-1 border-0 bg-transparent text-foreground outline-none"
        />
        <kbd className="rounded-[3px] border border-border px-1 font-mono text-[10px]">/</kbd>
      </label>
      <div className="min-h-0 flex-1 overflow-auto pb-2">
        {groups.map(([category, items]) => {
          const open = query.trim() !== "" || !closed.has(category);
          return (
            <div key={category} role="group" aria-label={categoryLabel[category] ?? category}>
              <button
                type="button"
                aria-expanded={open}
                onClick={() =>
                  setClosed((c) => {
                    const next = new Set(c);
                    if (next.has(category)) next.delete(category);
                    else next.add(category);
                    return next;
                  })
                }
                className="flex h-[26px] w-full cursor-pointer items-center gap-1.5 border-0 bg-transparent px-4 text-xs font-semibold text-sidebar-foreground"
              >
                <ChevronIcon open={open} width={12} height={12} className="text-muted-foreground" />
                {categoryLabel[category] ?? category}
                <span className="ml-auto text-2xs font-normal text-muted-foreground">{items.length}</span>
              </button>
              {open && (
                <ul className="m-0 list-none px-2 py-0">
                  {items.map((d) => (
                    <Item key={d.type} d={d} editable={editable} blocked={d.type === "core.start" && !!props.target?.hasStart} />
                  ))}
                </ul>
              )}
            </div>
          );
        })}
        {groups.length === 0 && catalog.size > 0 && (
          <p className="mx-4 my-2 text-xs text-muted-foreground">No action matches “{query}”.</p>
        )}
        <p className="mx-4 my-2 text-xs text-muted-foreground">
          {editable ? "Drag an action onto the canvas." : "Open a workflow on the canvas to add actions."}
        </p>
      </div>
    </section>
  );
}

function Item(props: { d: Descriptor; editable: boolean; blocked: boolean }) {
  const { d, editable, blocked } = props;
  const enabled = editable && !blocked;
  return (
    <li
      draggable={enabled}
      aria-disabled={!enabled}
      data-testid={`palette-${d.type}`}
      title={blocked ? "The workflow already has a start action" : `${d.label} – ${d.description}`}
      onDragStart={(e) => {
        e.dataTransfer.setData(paletteMime, d.type);
        e.dataTransfer.effectAllowed = "copy";
        paletteDrag.type = d.type;
      }}
      onDragEnd={() => {
        paletteDrag.type = null;
      }}
      className={
        "flex h-[34px] items-center gap-2 rounded-md px-2 " +
        (enabled ? "cursor-grab hover:bg-sidebar-accent" : editable ? "cursor-not-allowed opacity-45" : "cursor-default opacity-70")
      }
    >
      <span
        className="grid size-[22px] shrink-0 place-items-center rounded-sm"
        style={{ background: categoryColor[d.category] ?? categoryColor.general, color: "var(--rn-tiefe-0)" }}
      >
        <ActionIcon type={d.type} known width={13} height={13} />
      </span>
      <span className="min-w-0 leading-tight">
        <span className="block truncate">{d.label}</span>
        <small className="block truncate font-mono text-[10px] text-muted-foreground">{d.type}</small>
      </span>
    </li>
  );
}
