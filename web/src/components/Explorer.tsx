import { useState } from "react";
import type { Entry } from "../files";
import { ChevronIcon, FolderIcon, KindIcon, ReloadIcon } from "./Icons";

/** Project explorer: the project tree, folders open by default (mockup E4). */
export function Explorer(props: {
  tree: Entry | null;
  error: string | null;
  selected: string | null;
  onOpen: (path: string) => void;
  onReload: () => void;
}) {
  return (
    <section aria-label="Explorer" className="flex min-h-[120px] flex-[0_1_auto] flex-col">
      <div className="flex h-9 items-center justify-between pr-2 pl-4">
        <span className="label-caps">Explorer</span>
        <button
          type="button"
          title="Reload"
          aria-label="Reload explorer"
          onClick={props.onReload}
          className="grid size-6 cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
        >
          <ReloadIcon />
        </button>
      </div>
      {props.error && <p className="m-0 px-4 text-xs text-muted-foreground">{props.error}</p>}
      <ul role="tree" aria-label="Project files" className="m-0 list-none overflow-auto px-2 pb-2">
        {props.tree?.children?.map((e) => (
          <Node key={e.path} entry={e} depth={0} selected={props.selected} onOpen={props.onOpen} />
        ))}
      </ul>
    </section>
  );
}

function Node(props: { entry: Entry; depth: number; selected: string | null; onOpen: (path: string) => void }) {
  const { entry, depth } = props;
  const [open, setOpen] = useState(true);
  const isDir = entry.type === "dir";
  const selected = !isDir && entry.path === props.selected;
  const activate = () => (isDir ? setOpen(!open) : props.onOpen(entry.path));

  return (
    <li role="treeitem" aria-expanded={isDir ? open : undefined} aria-selected={isDir ? undefined : selected}>
      <div
        tabIndex={0}
        title={entry.path}
        onClick={activate}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            activate();
          }
        }}
        style={{ paddingLeft: 8 + depth * 18 }}
        className={
          "flex h-[26px] cursor-default items-center gap-1.5 rounded-sm pr-2 whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring " +
          (selected
            ? "bg-accent font-medium text-accent-foreground"
            : "hover:bg-sidebar-accent")
        }
      >
        {isDir ? (
          <ChevronIcon open={open} width={12} height={12} className="text-muted-foreground" />
        ) : (
          <span className="w-3" />
        )}
        <span className={selected ? "text-accent-foreground" : "text-muted-foreground"}>
          {isDir ? <FolderIcon /> : <KindIcon kind={entry.kind} />}
        </span>
        <span className="truncate">{entry.name}</span>
        {isDir && (
          <span className="ml-auto text-2xs text-muted-foreground">{entry.children?.length ?? 0}</span>
        )}
      </div>
      {isDir && open && entry.children && (
        <ul role="group" className="m-0 list-none p-0">
          {entry.children.map((c) => (
            <Node key={c.path} entry={c} depth={depth + 1} selected={props.selected} onOpen={props.onOpen} />
          ))}
        </ul>
      )}
    </li>
  );
}
