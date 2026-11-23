import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { kindOf } from "../files";
import { CloseIcon, KindIcon } from "./Icons";

/**
 * Tabs of the open files; the active tab has a line in the primary colour on top. With many
 * tabs they first get narrower (long names end in "…"), then the bar scrolls without a
 * visible scrollbar: the mouse wheel scrolls it sideways, the active tab is kept in view and
 * a fade at the edge shows that more tabs are there.
 */
export function Tabs(props: {
  paths: string[];
  active: string | null;
  /** Paths with unsaved changes: ● instead of ×, × on hover (mockup E6a). */
  dirty?: Set<string>;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
}) {
  const bar = useRef<HTMLDivElement>(null);
  const [more, setMore] = useState({ left: false, right: false });

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
          const name = path.slice(path.lastIndexOf("/") + 1);
          return (
            <div
              key={path}
              role="tab"
              aria-selected={active}
              title={dirty ? `${path} – unsaved changes` : path}
              data-dirty={dirty || undefined}
              tabIndex={0}
              onClick={() => props.onSelect(path)}
              onAuxClick={(e) => e.button === 1 && props.onClose(path)}
              onKeyDown={(e) => e.key === "Enter" && props.onSelect(path)}
              className={
                "flex max-w-56 min-w-28 shrink cursor-default items-center gap-2 border-r border-border pr-3 pl-4 " +
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
      {more.right && <Fade side="right" />}
    </div>
  );
}

/** A fade at the edge of the bar where more tabs are hidden. */
function Fade(props: { side: "left" | "right" }) {
  return (
    <div
      aria-hidden="true"
      data-testid={`tabs-more-${props.side}`}
      className={"pointer-events-none absolute top-0 bottom-px w-8 " + (props.side === "left" ? "left-0" : "right-0")}
      style={{ background: `linear-gradient(to ${props.side === "left" ? "right" : "left"}, var(--muted), transparent)` }}
    />
  );
}
