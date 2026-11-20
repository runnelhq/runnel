import { useCallback, useState, type KeyboardEvent, type PointerEvent, type RefObject } from "react";

const storageKey = "runnel.layout.explorerHeight";

/** Height of the explorer in pixels, remembered in the browser; null: the default split. */
export function useExplorerHeight(): [number | null, (h: number | null) => void] {
  const [height, setHeight] = useState<number | null>(() => {
    try {
      const v = Number(localStorage.getItem(storageKey));
      return Number.isFinite(v) && v > 0 ? v : null;
    } catch {
      return null;
    }
  });
  const set = useCallback((h: number | null) => {
    setHeight(h);
    try {
      if (h === null) localStorage.removeItem(storageKey);
      else localStorage.setItem(storageKey, String(Math.round(h)));
    } catch {
      // storage not available: the height applies until the page is reloaded
    }
  }, []);
  return [height, set];
}

/** Smallest heights of the explorer and of the palette below it. */
export const minExplorer = 120;
export const minPalette = 160;

/**
 * The line between explorer and palette: dragging it (or ↑/↓ while it has the focus)
 * changes the height of the explorer, a double-click returns to the default split.
 */
export function Splitter(props: {
  /** The sidebar holding explorer and palette, for the limits. */
  container: RefObject<HTMLElement | null>;
  /** The explorer, for its current height. */
  explorer: RefObject<HTMLElement | null>;
  height: number | null;
  onHeight: (h: number | null) => void;
}) {
  const { container, explorer, onHeight } = props;
  const [dragging, setDragging] = useState(false);
  const clamp = useCallback(
    (h: number) => {
      const total = container.current?.clientHeight ?? 0;
      return Math.round(Math.max(minExplorer, Math.min(h, total - minPalette)));
    },
    [container],
  );
  const current = () => props.height ?? explorer.current?.offsetHeight ?? minExplorer;

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const startY = e.clientY;
    const start = current();
    e.currentTarget.setPointerCapture(e.pointerId);
    setDragging(true);
    const move = (ev: globalThis.PointerEvent) => onHeight(clamp(start + ev.clientY - startY));
    const up = () => {
      setDragging(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.shiftKey ? 64 : 16;
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault();
      onHeight(clamp(current() + (e.key === "ArrowUp" ? -step : step)));
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      onHeight(clamp(e.key === "Home" ? minExplorer : Number.MAX_SAFE_INTEGER));
    }
  };

  return (
    <div className="relative h-0 shrink-0">
      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label="Resize explorer and palette"
        aria-valuenow={props.height ?? undefined}
        aria-valuemin={minExplorer}
        tabIndex={0}
        title="Drag to resize · double-click: default"
        onPointerDown={onPointerDown}
        onDoubleClick={() => onHeight(null)}
        onKeyDown={onKeyDown}
        className={
          "absolute inset-x-0 -top-[3px] z-10 h-[6px] cursor-row-resize outline-none hover:bg-ring/40 focus-visible:bg-ring/60" +
          (dragging ? " bg-ring/60" : "")
        }
      />
    </div>
  );
}
