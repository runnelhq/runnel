import { useEffect, useMemo, useRef, useState } from "react";
import { kindOf } from "../files";
import { CloseIcon, CopyIcon, FolderIcon, KindIcon, SaveIcon, SearchIcon } from "./Icons";

/**
 * Many open files (mockup E21, design/mockups/e21-tabs.html): the list of all open files at
 * the end of the tab bar (B) and the menu of a tab (C).
 */

/** Closes a popup on a click outside and on Escape. */
function useDismiss(ref: React.RefObject<HTMLElement | null>, onClose: () => void, except?: React.RefObject<HTMLElement | null>) {
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!ref.current?.contains(t) && !except?.current?.contains(t)) close.current();
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    window.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [ref, except]);
}

const nameOf = (path: string) => path.slice(path.lastIndexOf("/") + 1);
const dirOf = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "");

/** B: all open files in the order of the tabs; typing filters by name and path. */
export function OpenFilesList(props: {
  paths: string[];
  active: string | null;
  dirty: Set<string>;
  button: React.RefObject<HTMLElement | null>;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
  onCloseMany: (paths: string[]) => void;
  onDismiss: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useDismiss(ref, props.onDismiss, props.button);
  const [query, setQuery] = useState("");
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? props.paths.filter((p) => p.toLowerCase().includes(q)) : props.paths;
  }, [props.paths, query]);
  const [index, setIndex] = useState(0);
  useEffect(() => setIndex(query ? 0 : Math.max(0, props.paths.indexOf(props.active ?? ""))), [query, props.paths, props.active]);
  const choose = (path: string | undefined) => {
    if (!path) return;
    props.onSelect(path);
    props.onDismiss();
  };
  const saved = props.paths.filter((p) => !props.dirty.has(p));
  const mark = (text: string) => {
    const q = query.trim();
    const i = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1;
    if (i < 0) return text;
    return (
      <>
        {text.slice(0, i)}
        <mark className="rounded-[2px] bg-ring/25 font-semibold text-inherit">{text.slice(i, i + q.length)}</mark>
        {text.slice(i + q.length)}
      </>
    );
  };
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label="Open files"
      className="absolute top-[38px] right-1 z-40 w-[380px] rounded-md border border-border bg-popover p-1 text-xs text-popover-foreground shadow-lg"
    >
      <label className="m-0.5 mb-1 flex h-[30px] items-center gap-1.5 rounded-md border border-ring bg-background px-2 outline-2 outline-ring/25">
        <SearchIcon width={14} height={14} className="shrink-0 text-muted-foreground" />
        <input
          autoFocus
          aria-label="Go to an open file"
          placeholder="Go to an open file"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") setIndex((i) => Math.min(i + 1, shown.length - 1));
            else if (e.key === "ArrowUp") setIndex((i) => Math.max(i - 1, 0));
            else if (e.key === "Enter") choose(shown[index]);
            else return;
            e.preventDefault();
          }}
          className="min-w-0 flex-1 border-0 bg-transparent text-xs outline-none"
        />
      </label>
      <div className="flex px-2 pt-1.5 pb-0.5 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
        Open files
        <span className="ml-auto font-normal tracking-normal normal-case">
          {query.trim() ? `${shown.length} of ${props.paths.length}` : "in the order of the tabs · ● unsaved"}
        </span>
      </div>
      <ul role="listbox" aria-label="Open files" className="m-0 max-h-[60vh] list-none overflow-auto p-0">
        {shown.map((p, i) => (
          <li
            key={p}
            role="option"
            aria-selected={i === index}
            aria-current={p === props.active || undefined}
            onMouseEnter={() => setIndex(i)}
            onClick={() => choose(p)}
            className={
              "flex h-[30px] cursor-default items-center gap-2 rounded-sm pr-1.5 pl-2 " +
              (i === index ? "bg-accent text-accent-foreground " : "") +
              (p === props.active ? "font-semibold" : "")
            }
          >
            <KindIcon kind={kindOf(p)} width={15} height={15} className="shrink-0 text-muted-foreground" />
            <span className="min-w-0 shrink truncate">{mark(nameOf(p))}</span>
            <span className="min-w-10 flex-1 truncate font-mono text-[10px] font-normal text-muted-foreground">{mark(dirOf(p))}</span>
            {props.dirty.has(p) && (
              <span className="text-[10px]" title="Unsaved changes">
                ●
              </span>
            )}
            <button
              type="button"
              aria-label={`Close ${nameOf(p)}`}
              title="Close"
              onClick={(e) => {
                e.stopPropagation();
                props.onClose(p);
              }}
              className="grid size-5 shrink-0 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground hover:bg-secondary hover:text-foreground"
            >
              <CloseIcon width={12} height={12} />
            </button>
          </li>
        ))}
        {shown.length === 0 && <li className="px-2 py-1.5 text-muted-foreground">No open file matches “{query.trim()}”</li>}
      </ul>
      {!query.trim() && (
        <>
          <hr className="my-1 border-0 border-t border-border" />
          <div className="flex items-center gap-1.5 px-1 pt-1 pb-0.5">
            <MenuButton disabled={saved.length === 0} onClick={() => props.onCloseMany(saved)}>
              Close saved files
            </MenuButton>
            <MenuButton onClick={() => props.onCloseMany(props.paths)}>Close all…</MenuButton>
            <small className="ml-auto text-[10px] text-muted-foreground">↑↓ choose · Enter opens · Esc</small>
          </div>
        </>
      )}
    </div>
  );
}

function MenuButton(props: { disabled?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      disabled={props.disabled}
      onClick={props.onClick}
      className="inline-flex h-6 cursor-pointer items-center rounded-md border border-input bg-background px-2 text-2xs font-medium hover:bg-muted disabled:cursor-default disabled:opacity-50"
    >
      {props.children}
    </button>
  );
}

/** C: the menu of a tab (right click). */
export function TabMenu(props: {
  path: string;
  paths: string[];
  dirty: Set<string>;
  x: number;
  y: number;
  onCloseMany: (paths: string[]) => void;
  onCopyPath: (path: string) => void;
  onReveal: (path: string) => void;
  onDismiss: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useDismiss(ref, props.onDismiss);
  useEffect(() => ref.current?.querySelector<HTMLButtonElement>("button:not([disabled])")?.focus(), []);
  const { path, paths } = props;
  const saved = paths.filter((p) => !props.dirty.has(p));
  const right = paths.slice(paths.indexOf(path) + 1);
  const run = (f: () => void) => () => {
    props.onDismiss();
    f();
  };
  const item = (label: string, onClick: () => void, opts: { icon?: React.ReactNode; hint?: string; disabled?: boolean } = {}) => (
    <button
      key={label}
      type="button"
      role="menuitem"
      disabled={opts.disabled}
      onClick={run(onClick)}
      className="flex h-[30px] w-full cursor-pointer items-center gap-2.5 rounded-sm border-0 bg-transparent px-2 text-left text-sm text-popover-foreground hover:bg-accent focus-visible:bg-accent disabled:cursor-default disabled:opacity-45 disabled:hover:bg-transparent"
    >
      <span aria-hidden="true" className="inline-flex w-[15px] text-muted-foreground">
        {opts.icon}
      </span>
      {label}
      {opts.hint && <small className="ml-auto font-mono text-[10px] text-muted-foreground">{opts.hint}</small>}
    </button>
  );
  return (
    <div
      ref={ref}
      role="menu"
      aria-label={`Actions for ${nameOf(path)}`}
      style={{ left: props.x, top: props.y }}
      className="fixed z-40 w-[232px] rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {item("Close", () => props.onCloseMany([path]), { icon: <CloseIcon width={14} height={14} /> })}
      {item("Close others", () => props.onCloseMany(paths.filter((p) => p !== path)), { disabled: paths.length < 2 })}
      {item("Close to the right", () => props.onCloseMany(right), { disabled: right.length === 0 })}
      {item("Close saved", () => props.onCloseMany(saved), {
        icon: <SaveIcon width={14} height={14} />,
        hint: saved.length === 1 ? "1 file" : `${saved.length} files`,
        disabled: saved.length === 0,
      })}
      {item("Close all", () => props.onCloseMany(paths))}
      <hr className="my-1 border-0 border-t border-border" />
      {item("Copy path", () => props.onCopyPath(path), { icon: <CopyIcon width={14} height={14} /> })}
      {item("Reveal in explorer", () => props.onReveal(path), { icon: <FolderIcon width={14} height={14} /> })}
    </div>
  );
}
