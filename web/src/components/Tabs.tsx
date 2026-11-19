import { kindOf } from "../files";
import { CloseIcon, KindIcon } from "./Icons";

/** Tabs of the open files; the active tab has a line in the primary colour on top. */
export function Tabs(props: {
  paths: string[];
  active: string | null;
  /** Paths with unsaved changes: ● instead of ×, × on hover (mockup E6a). */
  dirty?: Set<string>;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
}) {
  return (
    <div role="tablist" aria-label="Open files" className="flex h-9 shrink-0 overflow-x-auto border-b border-border bg-muted">
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
              "flex shrink-0 cursor-default items-center gap-2 border-r border-border pr-3 pl-4 " +
              (active
                ? "-mb-px bg-background text-foreground shadow-[inset_0_2px_0_var(--primary)]"
                : "text-muted-foreground")
            }
          >
            <KindIcon kind={kindOf(path)} />
            {name}
            <button
              type="button"
              title="Close"
              aria-label={`Close ${name}`}
              onClick={(e) => {
                e.stopPropagation();
                props.onClose(path);
              }}
              className="group grid size-[18px] cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-secondary hover:text-foreground"
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
  );
}
