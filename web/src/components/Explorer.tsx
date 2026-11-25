import { useEffect, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import {
  createFile,
  createFolder,
  deleteFile,
  deleteFolder,
  fetchReferences,
  movedPath,
  newKinds,
  projectFile,
  projectTemplate,
  renamePath,
  suffixOf,
  template,
  type Entry,
  type NewKind,
  type Reference,
} from "../files";
import { NewConnectionDialog } from "./NewConnection";
import { NewTriggerDialog } from "./NewTrigger";
import { useTriggers } from "../forms";
import { useConnections } from "../reference";
import { Dialog } from "./Dialog";
import { ChevronIcon, ConnectionIcon, CopyIcon, FolderIcon, KindIcon, MoveIcon, PipelineIcon, PlugIcon, PlusIcon, ProjectIcon, ReloadIcon, TrashIcon, WorkflowIcon } from "./Icons";
import { usePlugins, type PluginInfo } from "../plugins";

/** Changes of the project files made in the explorer, for the tabs (E11). */
export interface ExplorerChanges {
  onCreated: (path: string) => void;
  onRenamed: (from: string, to: string) => void;
  onDeleted: (path: string) => void;
  /** Files changed on the server (references updated, E12): open tabs load them again. */
  onChanged: (paths: string[]) => void;
  /** Whether a file has unsaved changes (it cannot be renamed then). */
  isDirty: (path: string) => boolean;
  /** Absolute path of the project, for "Copy path". */
  projectDir: string | null;
}

/** A name being typed in the tree: for a new file or folder in dir, or to rename path. */
type Editing = { create: NewKind; dir: string } | { rename: Entry };

type Menu = { x: number; y: number; entry: Entry | null };

/** A rename or move waiting for the answer to "Update the references?" (mockup E12). */
type Moving = { from: string; to: string; refs: Reference[]; chosen: Set<number>; rename: boolean };

/** A file or folder being dragged, and the folder it would be dropped into ("" = project). */
type Drag = { entry: Entry; over: string | null; x: number; y: number };

const parentOf = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "");
const baseName = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/**
 * Project explorer: the project tree, folders open by default (mockup E4). Files and
 * folders are created, renamed and deleted here (mockup design/mockups/e11-files.html)
 * and moved by dragging or with "Move to…", with an offer to update the references of
 * other files (mockup design/mockups/e12-organize.html).
 */
export function Explorer(props: {
  tree: Entry | null;
  error: string | null;
  selected: string | null;
  onOpen: (path: string) => void;
  onReload: () => void;
  changes?: ExplorerChanges;
  /** Shows a file: opens its folders and scrolls its row into view ("Reveal in explorer", E21). */
  reveal?: { path: string; seq: number } | null;
}) {
  const [menu, setMenu] = useState<Menu | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [deleting, setDeleting] = useState<{ entry: Entry; refs: Reference[] | null } | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [moveTo, setMoveTo] = useState<Entry | null>(null);
  const [moving, setMoving] = useState<Moving | null>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [newConnection, setNewConnection] = useState(false);
  const [newTrigger, setNewTrigger] = useState<string | null>(null);
  const { changes } = props;
  const connections = useConnections(props.tree);
  const triggers = useTriggers(props.tree);
  // Connections and triggers show their type (mockups S39b, E20).
  const connectionTypes = new Map([...(connections ?? []).map((c) => [c.file, c.type] as const), ...(triggers?.triggers ?? []).map((t) => [t.file, t.type] as const)]);
  // Plugins show their version and state (mockup S52).
  const pluginsInfo = usePlugins(props.tree).info;
  const plugins = new Map((pluginsInfo?.plugins ?? []).map((p) => [p.folder, p] as const));

  const all = flatten(props.tree);
  const exists = (path: string) => all.some((e) => e.path.toLowerCase() === path.toLowerCase());
  const startCreate = (create: NewKind, dir?: string) => {
    setMenu(null);
    if (create === "trigger") return setNewTrigger(dir ?? newKinds.trigger.folder);
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
      setEditing(null);
      void startMove(from, to, true);
      return null;
    } catch (err) {
      return err instanceof Error ? err.message : String(err);
    }
  };
  const hasUnsaved = (entry: Entry) => {
    const dirty = entry.type === "file" ? changes?.isDirty(entry.path) : all.some((e) => e.path.startsWith(entry.path + "/") && changes?.isDirty(e.path));
    if (dirty) setProblem(`Save or close ${entry.name} first: it has unsaved changes`);
    return !!dirty;
  };
  const startRename = (entry: Entry) => {
    setMenu(null);
    if (entry.path === projectFile) return setProblem(`${projectFile} cannot be renamed: the project needs it`);
    if (!hasUnsaved(entry)) setEditing({ rename: entry });
  };
  /** Creates project.yaml again, named after the project folder (template in files.ts). */
  const createProject = async () => {
    setMenu(null);
    if (!changes) return;
    try {
      await createFile(projectFile, projectTemplate(changes.projectDir));
      props.onReload();
      changes.onCreated(projectFile);
    } catch (err) {
      setProblem(err instanceof Error ? err.message : String(err));
    }
  };
  const startMoveTo = (entry: Entry) => {
    setMenu(null);
    if (!hasUnsaved(entry)) setMoveTo(entry);
  };
  /**
   * Renames or moves from to to. If other files refer to it, the dialog "Update the
   * references?" asks first; files with unsaved changes are not changed (mockup E12).
   */
  const startMove = async (from: string, to: string, rename: boolean) => {
    if (!changes) return;
    let refs: Reference[] = [];
    try {
      refs = await fetchReferences(from);
    } catch {
      // Without the list the move goes ahead; the check reports missing files.
    }
    if (refs.length === 0) return move(from, to, []);
    const chosen = new Set(refs.flatMap((r, i) => (changes.isDirty(r.file) ? [] : [i])));
    setMoving({ from, to, refs, chosen, rename });
  };
  const move = async (from: string, to: string, refs: Reference[]) => {
    if (!changes) return;
    setMoving(null);
    try {
      const res = await renamePath(from, to, refs.map((r) => ({ file: r.file, line: r.line })));
      props.onReload();
      changes.onRenamed(from, to);
      if (res.updated.length > 0) changes.onChanged(res.updated);
      if (res.notUpdated.length > 0) {
        const first = res.notUpdated[0];
        setProblem(
          `${res.notUpdated.length === 1 ? "1 reference was" : `${res.notUpdated.length} references were`} not updated: ${first.file} (line ${first.line}): ${first.reason}`,
        );
      }
    } catch (err) {
      setProblem(err instanceof Error ? err.message : String(err));
    }
  };
  /** The folder a dragged entry can be dropped into, or null (its own folder, itself or a folder inside it). */
  const dropTarget = (entry: Entry, dir: string): string | null =>
    dir === parentOf(entry.path) || dir === entry.path || dir.startsWith(entry.path + "/") ? null : dir;
  const drop = (entry: Entry, dir: string) => {
    setDrag(null);
    const to = (dir ? dir + "/" : "") + entry.name;
    if (dropTarget(entry, dir) === null || hasUnsaved(entry)) return;
    if (exists(to)) return setProblem(`${to} already exists`);
    void startMove(entry.path, to, false);
  };
  const dragHandlers = changes
    ? {
        onStart: (entry: Entry, e: React.DragEvent) => {
          e.dataTransfer.effectAllowed = "move";
          e.dataTransfer.setData("text/plain", entry.path);
          // The hint next to the pointer replaces the picture of the row.
          const img = new Image();
          img.src = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
          e.dataTransfer.setDragImage(img, 0, 0);
          setDrag({ entry, over: null, x: e.clientX, y: e.clientY });
        },
        onOver: (dir: string, e: React.DragEvent) => {
          if (!drag) return;
          e.stopPropagation();
          const target = dropTarget(drag.entry, dir);
          if (target !== null) e.preventDefault();
          e.dataTransfer.dropEffect = target === null ? "none" : "move";
          if (drag.over !== target || Math.abs(drag.x - e.clientX) + Math.abs(drag.y - e.clientY) > 2) setDrag({ ...drag, over: target, x: e.clientX, y: e.clientY });
        },
        onDrop: (dir: string, e: React.DragEvent) => {
          e.preventDefault();
          e.stopPropagation();
          if (drag) drop(drag.entry, dir);
        },
        onEnd: () => setDrag(null),
      }
    : undefined;
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
      <ul
        role="tree"
        aria-label="Project files"
        className={"m-0 min-h-10 list-none overflow-auto rounded-sm px-2 pb-2 " + (drag?.over === "" ? "outline-1 outline-ring outline-dashed -outline-offset-1" : "")}
        onDragOver={(e) => dragHandlers?.onOver("", e)}
        onDrop={(e) => dragHandlers?.onDrop("", e)}
      >
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
            drag={dragHandlers}
            dropOver={drag?.over ?? null}
            dragging={drag?.entry.path ?? null}
            editing={editing}
            nameRow={dirExists ? nameRow : null}
            onSubmit={submit}
            onCancel={() => setEditing(null)}
            connectionTypes={connectionTypes}
            plugins={plugins}
            reveal={props.reveal ?? null}
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
          onMove={startMoveTo}
          onDelete={startDelete}
          projectMissing={!!props.tree && !exists(projectFile)}
          onCreateProject={() => void createProject()}
          onNewConnection={() => {
            setMenu(null);
            setNewConnection(true);
          }}
        />
      )}
      {newConnection && changes && (
        <NewConnectionDialog
          exists={exists}
          onCancel={() => setNewConnection(false)}
          onCreate={async (path, text) => {
            try {
              await createFile(path, text);
            } catch (err) {
              return err instanceof Error ? err.message : String(err);
            }
            setNewConnection(false);
            props.onReload();
            changes.onCreated(path);
            return null;
          }}
        />
      )}
      {newTrigger !== null && changes && (
        <NewTriggerDialog
          dir={newTrigger}
          targets={[...all.filter((e) => e.kind === "workflow"), ...all.filter((e) => e.kind === "pipeline")].map((e) => e.path)}
          exists={exists}
          onCancel={() => setNewTrigger(null)}
          onCreate={async (path, text) => {
            try {
              await createFile(path, text);
            } catch (err) {
              return err instanceof Error ? err.message : String(err);
            }
            setNewTrigger(null);
            props.onReload();
            changes.onCreated(path);
            return null;
          }}
        />
      )}
      {drag?.over != null && (
        <div
          aria-hidden="true"
          style={{ left: drag.x + 14, top: drag.y + 10 }}
          className="pointer-events-none fixed z-50 inline-flex items-center gap-1.5 rounded-md border border-border bg-popover px-2.5 py-1 text-xs text-popover-foreground shadow-md"
        >
          {drag.entry.type === "dir" ? <FolderIcon width={14} height={14} /> : <KindIcon kind={drag.entry.kind} width={14} height={14} />}
          {drag.entry.name}
          <small className="text-muted-foreground">→ {drag.over === "" ? "project" : drag.over + "/"}</small>
        </div>
      )}
      {moveTo && (
        <MoveDialog
          entry={moveTo}
          folders={all.filter((e) => e.type === "dir").map((e) => e.path)}
          exists={exists}
          onCancel={() => setMoveTo(null)}
          onReload={props.onReload}
          onMove={(dir) => {
            const entry = moveTo;
            setMoveTo(null);
            void startMove(entry.path, (dir ? dir + "/" : "") + entry.name, false);
          }}
        />
      )}
      {moving && (
        <ReferencesDialog
          moving={moving}
          isDirty={(f) => !!changes?.isDirty(f)}
          onChoose={(chosen) => setMoving({ ...moving, chosen })}
          onCancel={() => setMoving(null)}
          onMove={(all) => void move(moving.from, moving.to, all ? moving.refs.filter((_, i) => moving.chosen.has(i)) : [])}
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
  /** Dragging rows onto folders moves them (E12); none: rows cannot be dragged. */
  drag?: {
    onStart: (entry: Entry, e: React.DragEvent) => void;
    onOver: (dir: string, e: React.DragEvent) => void;
    onDrop: (dir: string, e: React.DragEvent) => void;
    onEnd: () => void;
  };
  /** The folder a dragged row would be dropped into, and the path of the dragged row. */
  dropOver: string | null;
  dragging: string | null;
  editing: Editing | null;
  nameRow: React.ReactNode;
  onSubmit: (name: string) => Promise<string | null>;
  onCancel: () => void;
  /** Types of the connection and trigger files by path, for their sign and tag (mockups S39b, E20). */
  connectionTypes: Map<string, string>;
  /** The plugins by folder (plugins/<name>), with version and state (mockup S52). */
  plugins: Map<string, PluginInfo>;
  reveal: { path: string; seq: number } | null;
}) {
  const { entry, depth, editing } = props;
  const connectionType = entry.kind === "connection" ? props.connectionTypes.get(entry.path) : undefined;
  const typeTag = entry.kind === "connection" || entry.kind === "trigger" ? props.connectionTypes.get(entry.path) : undefined;
  const [open, setOpen] = useState(true);
  const isDir = entry.type === "dir";
  const row = useRef<HTMLDivElement>(null);
  // "Reveal in explorer": the folders of the file open, its row scrolls into view.
  const { reveal } = props;
  useEffect(() => {
    if (!reveal) return;
    if (isDir && reveal.path.startsWith(entry.path + "/")) setOpen(true);
    if (reveal.path === entry.path) {
      row.current?.scrollIntoView?.({ block: "nearest" });
      row.current?.focus({ preventScroll: true });
    }
  }, [reveal, isDir, entry.path]);
  // A plugin folder opens the overview of its plugin; its chevron opens the folder.
  const plugin = isDir ? props.plugins.get(entry.path) : undefined;
  const pluginFile = plugin ? entry.path + "/plugin.yaml" : null;
  const selected = (!isDir && entry.path === props.selected) || (pluginFile !== null && pluginFile === props.selected);
  const activate = () => (pluginFile ? props.onOpen(pluginFile) : isDir ? setOpen(!open) : props.onOpen(entry.path));
  const renaming = editing && "rename" in editing && editing.rename.path === entry.path;
  const creatingHere = editing && "create" in editing && editing.dir === entry.path;
  // A row dropped on a file goes into the folder of that file.
  const dropDir = isDir ? entry.path : parentOf(entry.path);

  if (renaming) {
    const suffix = isDir ? "" : suffixOf(entry.path);
    return (
      <NameRow
        kind={null}
        entry={entry}
        initial={entry.name.slice(0, entry.name.length - suffix.length)}
        suffix={suffix}
        hint={() => "Enter renames · Esc cancels"}
        depth={depth}
        onSubmit={props.onSubmit}
        onCancel={props.onCancel}
      />
    );
  }
  return (
    <li role="treeitem" aria-label={entry.name} aria-expanded={isDir ? open : undefined} aria-selected={isDir ? undefined : selected}>
      <div
        ref={row}
        tabIndex={0}
        title={entry.path}
        draggable={!!props.drag && entry.path !== projectFile}
        onDragStart={(e) => props.drag?.onStart(entry, e)}
        onDragEnd={() => props.drag?.onEnd()}
        onDragOver={(e) => props.drag?.onOver(dropDir, e)}
        onDrop={(e) => props.drag?.onDrop(dropDir, e)}
        data-drop={isDir && props.dropOver === entry.path ? "" : undefined}
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
          (selected ? "bg-accent font-medium text-accent-foreground" : props.menuOpen === entry.path ? "bg-sidebar-accent" : "hover:bg-sidebar-accent") +
          (isDir && props.dropOver === entry.path ? " bg-accent outline-1 outline-ring outline-dashed -outline-offset-1" : "") +
          (props.dragging === entry.path ? " opacity-55" : "")
        }
      >
        {isDir ? (
          <ChevronIcon
            open={open}
            width={12}
            height={12}
            className="text-muted-foreground"
            onClick={
              plugin
                ? (e) => {
                    e.stopPropagation();
                    setOpen(!open);
                  }
                : undefined
            }
          />
        ) : (
          <span className="w-3" />
        )}
        <span className={selected ? "text-accent-foreground" : "text-muted-foreground"}>
          {plugin ? <PlugIcon /> : isDir ? <FolderIcon /> : connectionType ? <ConnectionIcon type={connectionType} /> : <KindIcon kind={entry.kind} />}
        </span>
        <span className="truncate">{entry.name}</span>
        <span className="ml-auto inline-flex items-center gap-1">
          {typeTag && <span className="font-mono text-[10px] text-muted-foreground">{typeTag}</span>}
          {plugin ? <PluginState plugin={plugin} /> : isDir && <span className="text-2xs text-muted-foreground">{entry.children?.length ?? 0}</span>}
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
  onMove: (e: Entry) => void;
  onDelete: (e: Entry) => void;
  /** project.yaml is missing (deleted outside the editor): "New" offers to create it. */
  projectMissing: boolean;
  onCreateProject: () => void;
  onNewConnection: () => void;
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
    (["workflow", "pipeline", "trigger", "environment", "document"] as const).map((k) =>
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
    content = [
      <div key="l" className="label-caps px-2 pt-1.5 pb-0.5">New</div>,
      ...kindItems(),
      item("Connection", <ConnectionIcon type="" />, props.onNewConnection, { hint: "connections/" }),
      sep("s1"),
      item("Folder", <FolderIcon />, () => props.onCreate("folder", "")),
      ...(props.projectMissing ? [item("Project file", <ProjectIcon />, props.onCreateProject, { hint: projectFile })] : []),
    ];
  } else if (entry.type === "dir") {
    const empty = (entry.children?.length ?? 0) === 0;
    content = [
      <div key="l" className="label-caps px-2 pt-1.5 pb-0.5">{entry.name}</div>,
      ...kindItems(entry.path).filter((_, i) => i < 2 || i === 4),
      item("New file…", <KindIcon kind="other" />, () => props.onCreate("file", entry.path)),
      item("New folder…", <FolderIcon />, () => props.onCreate("folder", entry.path)),
      sep("s1"),
      item("Rename", <span className="text-xs">✎</span>, () => props.onRename(entry), { hint: "F2" }),
      item("Move to…", <MoveIcon />, () => props.onMove(entry)),
      item("Delete folder", <TrashIcon />, () => props.onDelete(entry), { danger: true, disabled: !empty, hint: empty ? undefined : "not empty" }),
      sep("s2"),
      item("Copy path", <CopyIcon />, () => copy(entry.path)),
    ];
  } else {
    // project.yaml stays where it is: without it the project is not found (runnel serve).
    const fixed = entry.path === projectFile ? { disabled: true, hint: "required" } : {};
    content = [
      item("Rename", <span className="text-xs">✎</span>, () => props.onRename(entry), { hint: "F2", ...fixed }),
      item("Move to…", <MoveIcon />, () => props.onMove(entry), fixed),
      item("Delete", <TrashIcon />, () => props.onDelete(entry), { danger: true, ...fixed }),
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

const segments = (a: string, b: string) => {
  const x = a.split("/");
  const y = b.split("/");
  for (let i = 0; i < Math.min(x.length, y.length); i++) if (x[i] !== y[i]) return x[i] < y[i] ? -1 : 1;
  return x.length - y.length;
};

/** "Move to…": choose the folder, a new one can be created there (mockup E12, state "moveto"). */
function MoveDialog(props: {
  entry: Entry;
  folders: string[];
  exists: (path: string) => boolean;
  onCancel: () => void;
  onReload: () => void;
  onMove: (dir: string) => void;
}) {
  const { entry } = props;
  const current = parentOf(entry.path);
  const [created, setCreated] = useState<string[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [naming, setNaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inside = (dir: string) => entry.type === "dir" && (dir === entry.path || dir.startsWith(entry.path + "/"));
  const folders = ["", ...[...new Set([...props.folders, ...created])].filter((d) => !inside(d)).sort(segments)];
  // The current folder can be chosen to create a folder in it, not to move there.
  const to = selected === null || selected === current ? null : (selected ? selected + "/" : "") + entry.name;
  const taken = to !== null && props.exists(to);
  const createFolderIn = async (name: string) => {
    const path = (selected ? selected + "/" : "") + name.trim();
    if (!name.trim() || name.includes("/") || name.includes("\\")) return setError("The name must not be empty or contain / or \\");
    if (props.exists(path) || created.includes(path)) return setError(`${path} already exists`);
    try {
      await createFolder(path);
      setCreated((c) => [...c, path]);
      setSelected(path);
      setNaming(false);
      setError(null);
      props.onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };
  return (
    <Dialog
      title={`Move ${entry.name}`}
      form
      icon={<MoveIcon width={15} height={15} />}
      iconClass="bg-accent text-accent-foreground"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        { label: "Move", tone: "primary", disabled: to === null || taken, onClick: () => to !== null && selected !== null && props.onMove(selected) },
      ]}
    >
      <p>Choose the folder; a new one can be created there.</p>
      <div role="listbox" aria-label="Folders" className="mb-2.5 max-h-[260px] overflow-auto rounded-md border border-border p-1">
        {folders.map((dir) => {
          const isCurrent = dir === current;
          const depth = dir === "" ? 0 : dir.split("/").length;
          return (
            <button
              key={dir || "."}
              type="button"
              role="option"
              aria-selected={selected === dir}
              aria-label={dir === "" ? "(project)" : dir}
              onClick={() => setSelected(dir)}
              style={{ paddingLeft: 8 + depth * 18 }}
              className={
                "flex h-7 w-full cursor-pointer items-center gap-2 rounded-sm border-0 bg-transparent pr-2 text-left text-sm hover:bg-muted " +
                (selected === dir ? "bg-accent text-accent-foreground hover:bg-accent" : isCurrent ? "text-muted-foreground" : "text-foreground")
              }
            >
              {dir === "" ? <ProjectIcon width={15} height={15} /> : <FolderIcon width={15} height={15} />}
              {dir === "" ? "(project)" : baseName(dir)}
              {isCurrent && <small className="ml-auto text-2xs text-muted-foreground">current folder</small>}
            </button>
          );
        })}
      </div>
      {naming && (
        <input
          autoFocus
          aria-label="Name of the new folder"
          placeholder={`New folder in ${selected ? selected + "/" : "the project"}`}
          onKeyDown={(e) => {
            if (e.key === "Enter") void createFolderIn(e.currentTarget.value);
            if (e.key === "Escape") {
              e.stopPropagation();
              setNaming(false);
            }
          }}
          className="mb-2 h-7 w-full rounded-md border border-input bg-background px-2 text-sm text-foreground"
        />
      )}
      {error && <p className="text-xs text-destructive">✖ {error}</p>}
      <p className="flex items-center gap-2 text-xs">
        <button
          type="button"
          onClick={() => {
            setNaming(true);
            setError(null);
          }}
          className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-muted"
        >
          <PlusIcon width={14} height={14} />
          New folder
        </button>
        {to !== null &&
          (taken ? (
            <span className="text-destructive">✖ {to} already exists</span>
          ) : (
            <span>
              New path: <code className="font-mono text-foreground">{to}</code>
            </span>
          ))}
      </p>
    </Dialog>
  );
}

/** "Update the references?" before a rename or move (mockup E12, state "refs"). */
function ReferencesDialog(props: {
  moving: Moving;
  isDirty: (file: string) => boolean;
  onChoose: (chosen: Set<number>) => void;
  onCancel: () => void;
  onMove: (update: boolean) => void;
}) {
  const { from, to, refs, chosen, rename } = props.moving;
  const files = new Set(refs.map((r) => r.file)).size;
  const verb = rename ? "Rename" : "Move";
  const toggle = (i: number) => {
    const next = new Set(chosen);
    if (next.has(i)) next.delete(i);
    else next.add(i);
    props.onChoose(next);
  };
  return (
    <Dialog
      title="Update the references?"
      wide
      form
      icon={<MoveIcon width={15} height={15} />}
      iconClass="bg-accent text-accent-foreground"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        { label: `${verb} only`, onClick: () => props.onMove(false) },
        {
          label: `${verb} and update ${chosen.size} ${chosen.size === 1 ? "reference" : "references"}`,
          tone: "primary",
          disabled: chosen.size === 0,
          onClick: () => props.onMove(true),
        },
      ]}
    >
      <p>
        {baseName(from)} {rename ? "is renamed to" : "moves to"} <code className="font-mono text-foreground">{to}</code>.{" "}
        {files === 1 ? "1 file refers" : `${files} files refer`} to it by its old path:
      </p>
      <ul data-testid="references" className="mb-2.5! list-none! rounded-md bg-muted px-3! py-2">
        {refs.map((r, i) => {
          const dirty = props.isDirty(r.file);
          return (
            <li key={i} className="grid grid-cols-[20px_1fr] gap-2 py-1 text-xs text-foreground">
              <input
                type="checkbox"
                aria-label={`Update ${r.file}, ${r.what}`}
                checked={chosen.has(i)}
                disabled={dirty}
                onChange={() => toggle(i)}
                className="mt-0.5 size-3.5 accent-primary"
              />
              <span>
                <code className="font-mono text-2xs">{r.file}</code> – {r.what}
                {dirty && <span className="text-warning"> · unsaved changes – not changed</span>}
                <span className="block font-mono text-2xs text-muted-foreground">
                  {r.field}: <del className="text-destructive">{r.value}</del> <ins className="text-success no-underline">{movedPath(r.target, from, to)}</ins>
                </span>
              </span>
            </li>
          );
        })}
      </ul>
      <p className="mb-0!">Only these lines change; the files are saved. Unchecked files keep the old path and report it as missing.</p>
    </Dialog>
  );
}

/** Version and state of a plugin in its row (mockup S52): the sign never without its meaning. */
function PluginState({ plugin }: { plugin: PluginInfo }) {
  const [sign, tone, title] =
    plugin.state === "trusted"
      ? ["✓", "bg-success", "Trusted"]
      : plugin.state === "error"
        ? ["✕", "bg-destructive", "Cannot start"]
        : ["!", "bg-warning", plugin.state === "changed" ? "Changed since trusted" : "Not trusted"];
  return (
    <>
      <span className="font-mono text-[10px] text-muted-foreground">{plugin.version}</span>
      <span title={title} aria-label={title} data-testid={`plugin-state-${plugin.name}`} className={`inline-grid size-4 place-items-center rounded-full text-[9px] font-bold text-white ${tone}`}>
        {sign}
      </span>
    </>
  );
}
