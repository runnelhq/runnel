import { useEffect, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import {
  createFile,
  createFolder,
  deleteFile,
  deleteFolder,
  fetchReferences,
  newKinds,
  renamePath,
  suffixOf,
  template,
  type Entry,
  type NewKind,
  type Reference,
} from "../files";
import { Dialog } from "./Dialog";
import { ChevronIcon, CopyIcon, FolderIcon, KindIcon, PipelineIcon, PlusIcon, ReloadIcon, TrashIcon, WorkflowIcon } from "./Icons";

/** Changes of the project files made in the explorer, for the tabs (E11). */
export interface ExplorerChanges {
  onCreated: (path: string) => void;
  onRenamed: (from: string, to: string) => void;
  onDeleted: (path: string) => void;
  /** Whether a file has unsaved changes (it cannot be renamed then). */
  isDirty: (path: string) => boolean;
  /** Absolute path of the project, for "Copy path". */
  projectDir: string | null;
}

/** A name being typed in the tree: for a new file or folder in dir, or to rename path. */
type Editing = { create: NewKind; dir: string } | { rename: Entry };

type Menu = { x: number; y: number; entry: Entry | null };

/**
 * Project explorer: the project tree, folders open by default (mockup E4). Files and
 * folders are created, renamed and deleted here (mockup design/mockups/e11-files.html).
 */
export function Explorer(props: {
  tree: Entry | null;
  error: string | null;
  selected: string | null;
  onOpen: (path: string) => void;
  onReload: () => void;
  changes?: ExplorerChanges;
}) {
  const [menu, setMenu] = useState<Menu | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [deleting, setDeleting] = useState<{ entry: Entry; refs: Reference[] | null } | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const { changes } = props;

  const all = flatten(props.tree);
  const exists = (path: string) => all.some((e) => e.path.toLowerCase() === path.toLowerCase());
  const startCreate = (create: NewKind, dir?: string) => {
    setMenu(null);
    setEditing({ create, dir: dir ?? newKinds[create].folder });
  };
  const submit = async (name: string): Promise<string | null> => {
    if (!editing || !changes) return null;
    try {
      if ("create" in editing) {
        const kind = newKinds[editing.create];
        const path = (editing.dir ? editing.dir + "/" : "") + name + kind.suffix;
        if (exists(path)) return `${path} already exists`;
        if (editing.create === "folder") await createFolder(path);
        else {
          const firstWorkflow = all.find((e) => e.kind === "workflow")?.path;
          await createFile(path, template(editing.create, name, firstWorkflow));
        }
        setEditing(null);
        props.onReload();
        if (editing.create !== "folder") changes.onCreated(path);
        return null;
      }
      const from = editing.rename.path;
      const dir = from.includes("/") ? from.slice(0, from.lastIndexOf("/") + 1) : "";
      const to = dir + name + (editing.rename.type === "file" ? suffixOf(from) : "");
      if (to === from) {
        setEditing(null);
        return null;
      }
      if (exists(to) && to.toLowerCase() !== from.toLowerCase()) return `${to} already exists`;
      await renamePath(from, to);
      setEditing(null);
      props.onReload();
      changes.onRenamed(from, to);
      return null;
    } catch (err) {
      return err instanceof Error ? err.message : String(err);
    }
  };
  const startRename = (entry: Entry) => {
    setMenu(null);
    const dirty = entry.type === "file" ? changes?.isDirty(entry.path) : all.some((e) => e.path.startsWith(entry.path + "/") && changes?.isDirty(e.path));
    if (dirty) {
      setProblem(`Save or close ${entry.name} first: it has unsaved changes`);
      return;
    }
    setEditing({ rename: entry });
  };
  const startDelete = (entry: Entry) => {
    setMenu(null);
    setDeleting({ entry, refs: entry.type === "file" && (entry.kind === "workflow" || entry.kind === "pipeline") ? null : [] });
    if (entry.type === "file" && (entry.kind === "workflow" || entry.kind === "pipeline")) {
      fetchReferences(entry.path).then(
        (refs) => setDeleting((d) => (d?.entry === entry ? { entry, refs } : d)),
        () => setDeleting((d) => (d?.entry === entry ? { entry, refs: [] } : d)),
      );
    }
  };
  const confirmDelete = async () => {
    if (!deleting || !changes) return;
    const { entry } = deleting;
    try {
      if (entry.type === "dir") await deleteFolder(entry.path);
      else await deleteFile(entry.path);
      setDeleting(null);
      props.onReload();
      changes.onDeleted(entry.path);
    } catch (err) {
      setDeleting(null);
      setProblem(err instanceof Error ? err.message : String(err));
    }
  };
  const openMenu = (e: ReactMouseEvent, entry: Entry | null) => {
    if (!changes) return;
    e.preventDefault();
    e.stopPropagation();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    setMenu({ x: e.type === "contextmenu" ? e.clientX : r.left, y: e.type === "contextmenu" ? e.clientY : r.bottom + 4, entry });
  };

  const creatingAt = editing && "create" in editing ? editing.dir : null;
  const nameRow = editing && "create" in editing && (
    <NameRow
      key="new"
      kind={editing.create}
      initial={newKinds[editing.create].name}
      suffix={newKinds[editing.create].suffix}
      hint={(name) => {
        const path = (editing.dir ? editing.dir + "/" : "") + name + newKinds[editing.create].suffix;
        return editing.create === "folder" ? `Enter creates the folder ${path} · Esc cancels` : `Enter creates ${path} · Esc cancels`;
      }}
      depth={editing.dir ? editing.dir.split("/").length : 0}
      onSubmit={submit}
      onCancel={() => setEditing(null)}
    />
  );
  const dirExists = creatingAt === "" || (creatingAt !== null && all.some((e) => e.type === "dir" && e.path === creatingAt));

  return (
    <section aria-label="Explorer" className="flex min-h-[120px] flex-[0_1_auto] flex-col">
      <div className="flex h-9 items-center justify-between pr-2 pl-4">
        <span className="label-caps">Explorer</span>
        <span className="inline-flex gap-0.5">
          {changes && (
            <button
              type="button"
              title="New file or folder"
              aria-label="New"
              aria-haspopup="menu"
              onClick={(e) => openMenu(e, null)}
              className="grid size-6 cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
            >
              <PlusIcon width={15} height={15} />
            </button>
          )}
          <button
            type="button"
            title="Reload"
            aria-label="Reload explorer"
            onClick={props.onReload}
            className="grid size-6 cursor-pointer place-items-center rounded-sm text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          >
            <ReloadIcon />
          </button>
        </span>
      </div>
      {props.error && <p className="m-0 px-4 text-xs text-muted-foreground">{props.error}</p>}
      {problem && (
        <p role="alert" className="mx-2 mt-0 mb-1 flex gap-2 rounded-sm bg-danger-soft px-2 py-1 text-2xs text-destructive">
          ✖ {problem}
          <button type="button" className="ml-auto cursor-pointer border-0 bg-transparent p-0 text-muted-foreground" onClick={() => setProblem(null)}>
            ×
          </button>
        </p>
      )}
      <ul role="tree" aria-label="Project files" className="m-0 list-none overflow-auto px-2 pb-2">
        {(creatingAt === "" || (creatingAt !== null && !dirExists)) && nameRow}
        {props.tree?.children?.map((e) => (
          <Node
            key={e.path}
            entry={e}
            depth={0}
            selected={props.selected}
            onOpen={props.onOpen}
            menuOpen={menu?.entry?.path}
            onMenu={openMenu}
            onRename={changes ? startRename : undefined}
            editing={editing}
            nameRow={dirExists ? nameRow : null}
            onSubmit={submit}
            onCancel={() => setEditing(null)}
          />
        ))}
      </ul>
      {menu && changes && (
        <ContextMenu
          menu={menu}
          projectDir={changes.projectDir}
          onClose={() => setMenu(null)}
          onCreate={startCreate}
          onRename={startRename}
          onDelete={startDelete}
        />
      )}
      {deleting && (
        <Dialog
          title={`Delete ${deleting.entry.name}?`}
          icon={<TrashIcon width={15} height={15} />}
          iconClass="bg-danger-soft text-destructive"
          onCancel={() => setDeleting(null)}
          actions={[
            { label: "Cancel", onClick: () => setDeleting(null) },
            { label: "Delete", tone: "danger", onClick: () => void confirmDelete(), disabled: deleting.refs === null },
          ]}
        >
          <p>{deleting.entry.type === "dir" ? "The empty folder is deleted." : "The file is deleted from the project folder; this cannot be undone in the editor."}</p>
          {deleting.refs && deleting.refs.length > 0 && (
            <div data-testid="references" className="mb-3 rounded-md bg-warning-soft px-2.5 py-2 text-xs text-foreground">
              <b className="text-warning">
                ⚠ {deleting.refs.length} {deleting.refs.length === 1 ? "file refers" : "files refer"} to it
              </b>{" "}
              and will report it as missing:
              <ul className="mt-1 mb-0 list-disc pl-[18px]">
                {deleting.refs.map((r, i) => (
                  <li key={i}>
                    <span className="font-mono text-2xs">{r.file}</span> – {r.what}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {deleting.entry.type === "file" && <p className="mb-0">Its tab is closed. Unsaved changes in it are lost.</p>}
        </Dialog>
      )}
    </section>
  );
}

function flatten(e: Entry | null): Entry[] {
  if (!e) return [];
  return (e.children ?? []).flatMap((c) => [c, ...flatten(c)]);
}

function Node(props: {
  entry: Entry;
  depth: number;
  selected: string | null;
  onOpen: (path: string) => void;
  menuOpen?: string;
  onMenu: (e: ReactMouseEvent, entry: Entry) => void;
  /** F2 on a row renames it. */
  onRename?: (entry: Entry) => void;
  editing: Editing | null;
  nameRow: React.ReactNode;
  onSubmit: (name: string) => Promise<string | null>;
  onCancel: () => void;
}) {
  const { entry, depth, editing } = props;
  const [open, setOpen] = useState(true);
  const isDir = entry.type === "dir";
  const selected = !isDir && entry.path === props.selected;
  const activate = () => (isDir ? setOpen(!open) : props.onOpen(entry.path));
  const renaming = editing && "rename" in editing && editing.rename.path === entry.path;
  const creatingHere = editing && "create" in editing && editing.dir === entry.path;

  if (renaming) {
    const suffix = isDir ? "" : suffixOf(entry.path);
    return (
      <NameRow
        kind={null}
        entry={entry}
        initial={entry.name.slice(0, entry.name.length - suffix.length)}
        suffix={suffix}
        hint={() => "Enter renames · Esc cancels · references in other files are not changed"}
        depth={depth}
        onSubmit={props.onSubmit}
        onCancel={props.onCancel}
      />
    );
  }
  return (
    <li role="treeitem" aria-label={entry.name} aria-expanded={isDir ? open : undefined} aria-selected={isDir ? undefined : selected}>
      <div
        tabIndex={0}
        title={entry.path}
        onClick={activate}
        onContextMenu={(e) => props.onMenu(e, entry)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            activate();
          }
          if (e.key === "F2" && props.onRename) {
            e.preventDefault();
            props.onRename(entry);
          }
        }}
        style={{ paddingLeft: 8 + depth * 18 }}
        className={
          "group flex h-[26px] cursor-default items-center gap-1.5 rounded-sm pr-1 whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring " +
          (selected ? "bg-accent font-medium text-accent-foreground" : props.menuOpen === entry.path ? "bg-sidebar-accent" : "hover:bg-sidebar-accent")
        }
      >
        {isDir ? <ChevronIcon open={open} width={12} height={12} className="text-muted-foreground" /> : <span className="w-3" />}
        <span className={selected ? "text-accent-foreground" : "text-muted-foreground"}>{isDir ? <FolderIcon /> : <KindIcon kind={entry.kind} />}</span>
        <span className="truncate">{entry.name}</span>
        <span className="ml-auto inline-flex items-center gap-1">
          {isDir && <span className="text-2xs text-muted-foreground">{entry.children?.length ?? 0}</span>}
          <button
            type="button"
            aria-label={`Actions for ${entry.name}`}
            title="More actions"
            onClick={(e) => props.onMenu(e, entry)}
            className={
              "grid size-5 cursor-pointer place-items-center rounded-sm border-0 bg-transparent p-0 text-muted-foreground hover:text-foreground " +
              (props.menuOpen === entry.path ? "visible" : "invisible group-hover:visible group-focus-visible:visible")
            }
          >
            ⋯
          </button>
        </span>
      </div>
      {isDir && (open || creatingHere) && (
        <ul role="group" className="m-0 list-none p-0">
          {creatingHere && props.nameRow}
          {entry.children?.map((c) => (
            <Node key={c.path} {...props} entry={c} depth={depth + 1} />
          ))}
        </ul>
      )}
    </li>
  );
}

/** The name of a new or renamed file typed in the tree (mockup E11, state "name"). */
function NameRow(props: {
  kind: NewKind | null;
  entry?: Entry;
  initial: string;
  suffix: string;
  hint: (name: string) => string;
  depth: number;
  onSubmit: (name: string) => Promise<string | null>;
  onCancel: () => void;
}) {
  const [name, setName] = useState(props.initial);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);
  const local = (n: string) => (n.trim() === "" ? "The name is empty" : n.includes("/") || n.includes("\\") ? "The name must not contain / or \\" : null);
  const submit = async () => {
    const problem = local(name);
    if (problem) return setError(problem);
    setBusy(true);
    const err = await props.onSubmit(name.trim());
    setBusy(false);
    setError(err);
  };
  const kindIcon =
    props.kind === "folder" || props.entry?.type === "dir" ? (
      <FolderIcon />
    ) : (
      <KindIcon kind={props.kind ?? props.entry?.kind} />
    );
  return (
    <li role="treeitem" aria-selected={false}>
      <div style={{ paddingLeft: 8 + props.depth * 18 }} className="flex h-[26px] items-center gap-1.5 pr-2 whitespace-nowrap">
        <span className="w-3" />
        <span className="text-muted-foreground">{kindIcon}</span>
        <span
          className={
            "inline-flex h-[22px] min-w-0 flex-1 items-center overflow-hidden rounded-sm border bg-background px-1 outline-2 " +
            (error ? "border-destructive outline-danger-soft" : "border-ring outline-ring/25")
          }
        >
          <input
            ref={ref}
            aria-label={props.kind ? `Name of the new ${props.kind}` : `New name of ${props.entry?.name}`}
            aria-invalid={!!error}
            value={name}
            disabled={busy}
            onChange={(e) => {
              setName(e.target.value);
              setError(null);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") void submit();
              if (e.key === "Escape") props.onCancel();
            }}
            onBlur={() => !busy && props.onCancel()}
            // As wide as the name, so that it is shown from its start; the ending gives way.
            style={{ width: `${Math.max(name.length, 4) + 1}ch` }}
            className="max-w-full min-w-0 shrink-0 border-0 bg-transparent p-0 text-sm text-foreground outline-none"
          />
          {props.suffix && <span className="min-w-0 truncate text-sm text-muted-foreground">{props.suffix}</span>}
        </span>
      </div>
      <div className={"mt-0.5 mb-1 text-2xs " + (error ? "text-destructive" : "text-muted-foreground")} style={{ paddingLeft: 46 + props.depth * 18 }}>
        {error ? `✖ ${error}` : props.hint(name)}
      </div>
    </li>
  );
}

function ContextMenu(props: {
  menu: Menu;
  projectDir: string | null;
  onClose: () => void;
  onCreate: (kind: NewKind, dir?: string) => void;
  onRename: (e: Entry) => void;
  onDelete: (e: Entry) => void;
}) {
  const { menu } = props;
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(props.onClose);
  close.current = props.onClose;
  useEffect(() => {
    ref.current?.querySelector<HTMLButtonElement>("button:not([disabled])")?.focus();
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && close.current();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    window.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, []);
  const entry = menu.entry;
  const item = (label: string, icon: React.ReactNode, onClick: () => void, opts: { hint?: string; danger?: boolean; disabled?: boolean } = {}) => (
    <button
      key={label}
      type="button"
      role="menuitem"
      disabled={opts.disabled}
      onClick={onClick}
      className={
        "flex h-[30px] w-full cursor-pointer items-center gap-2.5 rounded-sm border-0 bg-transparent px-2 text-left text-sm hover:bg-accent focus-visible:bg-accent disabled:cursor-default disabled:opacity-45 disabled:hover:bg-transparent " +
        (opts.danger ? "text-destructive" : "text-popover-foreground")
      }
    >
      <span aria-hidden="true" className="inline-flex text-muted-foreground">{icon}</span>
      {label}
      {opts.hint && <small className="ml-auto font-mono text-[10px] text-muted-foreground">{opts.hint}</small>}
    </button>
  );
  const kindItems = (dir?: string) =>
    (["workflow", "pipeline", "trigger", "environment"] as const).map((k) =>
      item(
        dir === undefined ? newKinds[k].label : `New ${k}…`,
        k === "workflow" ? <WorkflowIcon /> : k === "pipeline" ? <PipelineIcon /> : <KindIcon kind={k} />,
        () => props.onCreate(k, dir),
        { hint: dir === undefined ? newKinds[k].folder + "/" : undefined },
      ),
    );
  const copy = (path: string) => {
    const sep = props.projectDir?.includes("\\") ? "\\" : "/";
    void navigator.clipboard?.writeText(props.projectDir ? props.projectDir.replace(/[\\/]+$/, "") + sep + path.split("/").join(sep) : path);
    props.onClose();
  };
  const sep = (key: string) => <hr key={key} className="my-1 border-0 border-t border-border" />;
  let content;
  if (!entry) {
    content = [<div key="l" className="label-caps px-2 pt-1.5 pb-0.5">New</div>, ...kindItems(), sep("s1"), item("Folder", <FolderIcon />, () => props.onCreate("folder", ""))];
  } else if (entry.type === "dir") {
    const empty = (entry.children?.length ?? 0) === 0;
    content = [
      <div key="l" className="label-caps px-2 pt-1.5 pb-0.5">{entry.name}</div>,
      ...kindItems(entry.path).slice(0, 2),
      item("New file…", <KindIcon kind="other" />, () => props.onCreate("file", entry.path)),
      item("New folder…", <FolderIcon />, () => props.onCreate("folder", entry.path)),
      sep("s1"),
      item("Rename", <span className="text-xs">✎</span>, () => props.onRename(entry), { hint: "F2" }),
      item("Delete folder", <TrashIcon />, () => props.onDelete(entry), { danger: true, disabled: !empty, hint: empty ? undefined : "not empty" }),
      sep("s2"),
      item("Copy path", <CopyIcon />, () => copy(entry.path)),
    ];
  } else {
    content = [
      item("Rename", <span className="text-xs">✎</span>, () => props.onRename(entry), { hint: "F2" }),
      item("Delete", <TrashIcon />, () => props.onDelete(entry), { danger: true }),
      sep("s1"),
      item("Copy path", <CopyIcon />, () => copy(entry.path)),
    ];
  }
  return (
    <div
      ref={ref}
      role="menu"
      aria-label={entry ? `Actions for ${entry.name}` : "New"}
      style={{ left: menu.x, top: menu.y }}
      className="fixed z-40 w-[236px] rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {content}
    </div>
  );
}
