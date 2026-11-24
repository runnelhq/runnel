import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { kindOf, tabName } from "../files";
import { ChevronIcon, CloseIcon, KindIcon } from "./Icons";
import { OpenFilesList, TabMenu } from "./OpenFiles";

/**
 * Tabs of the open files; the active tab has a line in the primary colour on top. With many
 * tabs they first get narrower (long names end in "…"), then the bar scrolls without a
 * visible scrollbar: the mouse wheel scrolls it sideways, the active tab is kept in view and
 * a fade at the edge shows that more tabs are there. Tabs stay at least 160 px wide so that
 * their names can be read (mockup E21). "▾ n" at the end lists all open files (Ctrl+Shift+L),
 * a right click on a tab opens its menu.
 */
export function Tabs(props: {
  paths: string[];
  active: string | null;
  /** Paths with unsaved changes: ● instead of ×, × on hover (mockup E6a). */
  dirty?: Set<string>;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
  /** Closes several files; those with unsaved changes ask one after the other. */
  onCloseMany: (paths: string[]) => void;
  onCopyPath: (path: string) => void;
  onReveal: (path: string) => void;
}) {
  const bar = useRef<HTMLDivElement>(null);
  const allButton = useRef<HTMLButtonElement>(null);
  const [more, setMore] = useState({ left: false, right: false });
  const [list, setList] = useState(false);
  const [menu, setMenu] = useState<{ path: string; x: number; y: number } | null>(null);
  const dirty = props.dirty ?? noDirty;

  // Ctrl+Shift+L opens the list of the open files (Ctrl+Shift+E is the larger expression editor, S35b).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && !e.altKey && e.key.toLowerCase() === "l") {
        e.preventDefault();
        setList((l) => !l);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Which edges hide tabs: after scrolling, resizing and when tabs come or go.
  const measure = () => {
    const el = bar.current;
    if (!el) return;
    const left = el.scrollLeft > 1;
    const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
    setMore((m) => (m.left === left && m.right === right ? m : { left, right }));
  };
  useLayoutEffect(measure, [props.paths]);
  useEffect(() => {
    const el = bar.current;
    if (!el) return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // The active tab is always in view, e.g. when a file opens at the end of a long bar.
  useEffect(() => {
    const tab = bar.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
    tab?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [props.active, props.paths]);

  return (
    <div className="relative flex h-9 min-w-0 shrink-0 bg-muted shadow-[inset_0_-1px_0_var(--border)]">
      {/* The line below the tabs is an inset shadow that the active tab covers: a tab reaching
          over a border would make the bar scroll vertically (overflow-x: auto implies y). */}
      <div
        ref={bar}
        role="tablist"
        aria-label="Open files"
        onScroll={measure}
        onWheel={(e) => {
          // An ordinary mouse wheel only scrolls vertically: turn it into scrolling sideways.
          if (bar.current && Math.abs(e.deltaY) > Math.abs(e.deltaX)) bar.current.scrollLeft += e.deltaY;
        }}
        className="no-scrollbar flex min-w-0 flex-1 overflow-x-auto overflow-y-hidden"
      >
        {props.paths.map((path) => {
          const active = path === props.active;
          const dirty = props.dirty?.has(path) ?? false;
          const name = tabName(path);
          return (
            <div
              key={path}
              role="tab"
              aria-selected={active}
              title={dirty ? `${path} – unsaved changes` : name === path ? undefined : path}
              data-dirty={dirty || undefined}
              tabIndex={0}
              onClick={() => props.onSelect(path)}
              onAuxClick={(e) => e.button === 1 && props.onClose(path)}
              onContextMenu={(e) => {
                e.preventDefault();
                setMenu({ path, x: e.clientX, y: e.clientY });
              }}
              onKeyDown={(e) => e.key === "Enter" && props.onSelect(path)}
              className={
                "flex max-w-60 min-w-40 shrink cursor-default items-center gap-2 border-r border-border pr-3 pl-4 " +
                (active ? "bg-background text-foreground shadow-[inset_0_2px_0_var(--primary)]" : "text-muted-foreground")
              }
            >
              <KindIcon kind={kindOf(path)} className="shrink-0" />
              <span className="min-w-0 flex-1 truncate">{name}</span>
              <button
                type="button"
                title="Close"
                aria-label={`Close ${name}`}
                onClick={(e) => {
                  e.stopPropagation();
                  props.onClose(path);
                }}
                className="group grid size-[18px] shrink-0 cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-secondary hover:text-foreground"
              >
                {dirty ? (
                  <>
                    <span className="text-[10px] text-foreground group-hover:hidden">●</span>
                    <CloseIcon width={12} height={12} className="hidden group-hover:block" />
                  </>
                ) : (
                  <CloseIcon width={12} height={12} />
                )}
              </button>
            </div>
          );
        })}
      </div>
      {more.left && <Fade side="left" />}
      {more.right && <Fade side="right" right={allButtonWidth} />}
      <button
        ref={allButton}
        type="button"
        aria-label={`All open files (${props.paths.length})`}
        aria-expanded={list}
        title="All open files (Ctrl+Shift+L)"
        onClick={() => setList((l) => !l)}
        className={
          "flex shrink-0 cursor-pointer items-center gap-1 border-0 border-l border-border px-2.5 text-xs " +
          (list ? "bg-background text-foreground" : "bg-muted text-muted-foreground hover:text-foreground")
        }
        style={{ width: allButtonWidth }}
      >
        <ChevronIcon open width={14} height={14} />
        <b className="font-semibold">{props.paths.length}</b>
      </button>
      {list && (
        <OpenFilesList
          paths={props.paths}
          active={props.active}
          dirty={dirty}
          button={allButton}
          onSelect={props.onSelect}
          onClose={props.onClose}
          onCloseMany={(paths) => {
            setList(false);
            props.onCloseMany(paths);
          }}
          onDismiss={() => setList(false)}
        />
      )}
      {menu && (
        <TabMenu
          {...menu}
          paths={props.paths}
          dirty={dirty}
          onCloseMany={props.onCloseMany}
          onCopyPath={props.onCopyPath}
          onReveal={props.onReveal}
          onDismiss={() => setMenu(null)}
        />
      )}
    </div>
  );
}

const noDirty = new Set<string>();
/** Width of "▾ n" at the end of the bar; the right fade ends before it. */
const allButtonWidth = 56;

/** A fade at the edge of the bar where more tabs are hidden. */
function Fade(props: { side: "left" | "right"; right?: number }) {
  return (
    <div
      aria-hidden="true"
      data-testid={`tabs-more-${props.side}`}
      className={"pointer-events-none absolute top-0 bottom-px w-8 " + (props.side === "left" ? "left-0" : "")}
      style={{ right: props.side === "right" ? (props.right ?? 0) : undefined, background: `linear-gradient(to ${props.side === "left" ? "right" : "left"}, var(--muted), transparent)` }}
    />
  );
}
