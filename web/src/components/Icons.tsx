import type { ReactNode, SVGProps } from "react";

// Line icons in the style of the mockups: 24 × 24, stroke = currentColor.
function Icon({ children, ...props }: SVGProps<SVGSVGElement> & { children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={16}
      height={16}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      {children}
    </svg>
  );
}

type P = SVGProps<SVGSVGElement>;

// Shapes shared by several icons of mockup E13.
const filePage = "M13 21H6V3h8l4 4v5M14 3v4h4";
const folderPath = "M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z";
const replyPath = "M9 7L4 12l5 5M4 12h9a7 7 0 0 1 7 7";

export const MonitorIcon = (p: P) => (
  <Icon {...p}>
    <rect x="3" y="4" width="18" height="12" rx="2" />
    <path d="M8 20h8M12 16v4" />
  </Icon>
);

export const SunIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </Icon>
);

export const MoonIcon = (p: P) => (
  <Icon {...p}>
    <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z" />
  </Icon>
);

export const FolderIcon = (p: P) => (
  <Icon {...p}>
    <path d={folderPath} />
  </Icon>
);

export const WorkflowIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="6" cy="6" r="2.5" />
    <circle cx="18" cy="18" r="2.5" />
    <path d="M8.5 6h3a3 3 0 0 1 3 3v6a3 3 0 0 0 3 3" />
  </Icon>
);

export const LinesIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 6h16M4 12h16M4 18h10" />
  </Icon>
);

export const ProjectIcon = (p: P) => (
  <Icon {...p}>
    <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z" />
    <path d="M12 12l8-4.5M12 12v9M12 12L4 7.5" />
  </Icon>
);

export const FileIcon = (p: P) => (
  <Icon {...p}>
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
    <path d="M14 3v5h5" />
  </Icon>
);

export const ChevronIcon = ({ open, ...p }: P & { open: boolean }) => (
  <Icon {...p}>
    <path d={open ? "M6 9l6 6 6-6" : "M9 6l6 6-6 6"} />
  </Icon>
);

export const ReloadIcon = (p: P) => (
  <Icon {...p}>
    <path d="M20 11a8 8 0 0 0-14.6-4.5L4 8M4 4v4h4M4 13a8 8 0 0 0 14.6 4.5L20 16M20 20v-4h-4" />
  </Icon>
);

export const CollapseAllIcon = (p: P) => (
  <Icon {...p}>
    <path d="M7 4l5 5 5-5M7 20l5-5 5 5" />
  </Icon>
);

export const CloseIcon = (p: P) => (
  <Icon {...p}>
    <path d="M6 6l12 12M18 6L6 18" />
  </Icon>
);

export const LockIcon = (p: P) => (
  <Icon {...p}>
    <rect x="5" y="11" width="14" height="10" rx="2" />
    <path d="M8 11V8a4 4 0 0 1 8 0v3" />
  </Icon>
);

export const CopyIcon = (p: P) => (
  <Icon {...p}>
    <rect x="8" y="8" width="12" height="12" rx="2" />
    <path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" />
  </Icon>
);

export const UndoIcon = (p: P) => (
  <Icon {...p}>
    <path d="M9 14L4 9l5-5" />
    <path d="M4 9h10a6 6 0 0 1 0 12h-3" />
  </Icon>
);

export const RedoIcon = (p: P) => (
  <Icon {...p}>
    <path d="M15 14l5-5-5-5" />
    <path d="M20 9H10a6 6 0 0 0 0 12h3" />
  </Icon>
);

export const SaveIcon = (p: P) => (
  <Icon {...p}>
    <path d="M5 3h11l3 3v13a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z" />
    <path d="M7 3v5h8V3M7 21v-7h10v7" />
  </Icon>
);

export const CodeIcon = (p: P) => (
  <Icon {...p}>
    <path d="M8 6l-5 6 5 6M16 6l5 6-5 6" />
  </Icon>
);

export const PlusIcon = (p: P) => (
  <Icon {...p}>
    <path d="M12 5v14M5 12h14" />
  </Icon>
);

export const MinusIcon = (p: P) => (
  <Icon {...p}>
    <path d="M5 12h14" />
  </Icon>
);

export const FitIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
  </Icon>
);

export const LayoutIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 6h16M4 12h10M4 18h6" />
  </Icon>
);

export const ArrowUpIcon = (p: P) => (
  <Icon {...p}>
    <path d="M12 19V5M6 11l6-6 6 6" />
  </Icon>
);

export const ArrowDownIcon = (p: P) => (
  <Icon {...p}>
    <path d="M12 5v14M6 13l6 6 6-6" />
  </Icon>
);

export const PipelineIcon = (p: P) => (
  <Icon {...p}>
    <path d="M3 7h5l3 5-3 5H3M11 12h10M17 8l4 4-4 4" />
  </Icon>
);

export const PanelIcon = (p: P) => (
  <Icon {...p}>
    <rect x="3" y="4" width="18" height="16" rx="2" />
    <path d="M15 4v16" />
  </Icon>
);

export const PlayIcon = (p: P) => (
  <Icon {...p}>
    <path d="M8 5l11 7-11 7z" fill="currentColor" stroke="none" />
  </Icon>
);

export const StopIcon = (p: P) => (
  <Icon {...p}>
    <rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor" stroke="none" />
  </Icon>
);

export const SearchIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="11" cy="11" r="6" />
    <path d="M20 20l-4.5-4.5" />
  </Icon>
);

/** Trigger files (mockup E11): a stopwatch. */
export const TriggerIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="12" cy="13" r="8" />
    <path d="M12 9v4l3 2M9 2h6" />
  </Icon>
);

/** Data preview (mockup S44): an eye. */
export const EyeIcon = (p: P) => (
  <Icon {...p}>
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
    <circle cx="12" cy="12" r="3" />
  </Icon>
);

/** Detect fields (mockup S46): lines of a list with a magnifier. */
export const DetectIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 6h10M4 12h7M4 18h5" />
    <circle cx="17" cy="15" r="3.5" />
    <path d="M19.5 17.5L22 20" />
  </Icon>
);

export const MoveIcon = (p: P) => (
  <Icon {...p}>
    <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    <path d="M10 13h6M13.5 10.5L16 13l-2.5 2.5" />
  </Icon>
);

export const CutIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="6" cy="18" r="2.5" />
    <circle cx="18" cy="18" r="2.5" />
    <path d="M8 16L18 4M16 16L6 4" />
  </Icon>
);

export const PasteIcon = (p: P) => (
  <Icon {...p}>
    <rect x="5" y="4" width="14" height="17" rx="2" />
    <path d="M9 4V3h6v1M9 10h6M9 14h6" />
  </Icon>
);

export const TrashIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
  </Icon>
);

// Icons of the built-in action types (white on the category tile of a node).
const actionIcons: Record<string, ReactNode> = {
  "core.start": <path d="M8 5l11 7-11 7z" fill="currentColor" />,
  "core.wait": (
    <>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 8v4l3 2" />
    </>
  ),
  "core.log": <path d="M5 6h14M5 12h14M5 18h9" />,
  // A step that does nothing: an empty, dashed place on the line (mockup E13).
  "core.dummy": (
    <>
      <path d="M2.5 12h4M17.5 12h4" />
      <circle cx="12" cy="12" r="5" strokeDasharray="2.6 2.2" />
    </>
  ),
  "core.success": (
    <>
      <circle cx="12" cy="12" r="8" />
      <path d="M8.5 12.5l2.5 2.5 4.5-5" />
    </>
  ),
  "core.abort": (
    <>
      <circle cx="12" cy="12" r="8" />
      <path d="M9 9l6 6M15 9l-6 6" />
    </>
  ),
  // Execution: the symbols of workflow and pipeline files in the explorer (mockup E13).
  "core.run-workflow": (
    <>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="18" cy="18" r="2.5" />
      <path d="M8.5 6h3a3 3 0 0 1 3 3v6a3 3 0 0 0 3 3" />
    </>
  ),
  "core.run-pipeline": <path d="M3 7h5l3 5-3 5H3M11 12h10M17 8l4 4-4 4" />,
  // Files: a page with a sign for files, a folder for folders and lists (mockup E13).
  "file.copy": <path d="M9 7V3h6l4 4v10h-4M5 7h6l4 4v10H5z" />,
  "file.delete": <path d={`${filePage}M15.5 15.5l5 5M20.5 15.5l-5 5`} />,
  "file.exists": <path d={`${filePage}M14.5 18l2.5 2.5 4.5-5`} />,
  // Reading and writing a file (S66, variant A chosen by the maintainer): an arrow out of the page, a pencil on it.
  "file.read": <path d={`${filePage}M14 17.5h6.5M18 15l2.5 2.5L18 20`} />,
  "file.write": <path d={`${filePage}M14.5 21l.5-2.5 4.5-4.5 2 2-4.5 4.5zM18.5 15l2 2`} />,
  "file.wait": (
    <>
      <path d={filePage} />
      <circle cx="18" cy="18" r="3.5" />
      <path d="M18 16.3V18l1.2 1" />
    </>
  ),
  // file.list: also the transform of the same name (S55, chosen by the maintainer), in the colour of its category.
  "file.list": <path d={`${folderPath}M7.5 11h9M7.5 14.5h6`} />,
  "file.mkdir": <path d={`${folderPath}M12 9.5v6M9 12.5h6`} />,
  "file.move": <path d={`${folderPath}M9 13h7M13.5 10.5L16 13l-2.5 2.5`} />,
  // Archives (S54, variant A chosen by the maintainer): a box; packing with an arrow in, unpacking with an arrow out.
  "archive.create": <path d="M3 4h18v4H3zM5 8v12h14V8M12 10.5v6.5M9.5 14.5L12 17l2.5-2.5" />,
  "archive.extract": <path d="M3 4h18v4H3zM5 8v12h14V8M12 17.5V11M9.5 13.5L12 11l2.5 2.5" />,
  // Checking a condition (S10, variant A chosen by the maintainer): a diamond as in flowcharts, with a check mark.
  "core.check": <path d="M12 3l9 9-9 9-9-9zM8.5 12l2.5 2.5 4.5-5" />,
  // HTTP request (S10, variant B chosen by the maintainer): the request out, the answer back.
  "http.request": <path d="M4 8h14M14 4l4 4-4 4M20 16H6M10 12l-4 4 4 4" />,
  // Network: answering the caller; the same sign for the action and the transform.
  "http.response": <path d={replyPath} />,
  // Transforms (design/mockups/s25a-pipeline.html); core.log and core.dummy as above.
  "csv.input": <path d="M6 3h8l4 4v14H6zM14 3v4h4M9 13h6M9 17h6" />,
  "csv.output": <path d="M6 3h8l4 4v14H6zM14 3v4h4M12 11v7M9 15l3 3 3-3" />,
  // XML files (S68a/S68b, variant A chosen by the maintainer): a page with angle brackets, as json.input with
  // braces; writing with an arrow down, as json.output.
  "xml.input": <path d="M6 3h8l4 4v14H6zM14 3v4h4M10.5 11l-2.5 2.5 2.5 2.5M13.5 11l2.5 2.5-2.5 2.5" />,
  "xml.output": <path d="M14 3H6v18h6M14 3v4h4v4M10 10l-2 2.5 2 2.5M12.5 10l2 2.5-2 2.5M18 14v7M15 18l3 3 3-3" />,
  // Excel workbooks (S67a/S67b, variant A chosen by the maintainer): a page with a table; writing
  // with an arrow down, as json.output.
  "excel.input": <path d="M6 3h8l4 4v14H6zM14 3v4h4M8.5 11h7v7h-7zM8.5 14.5h7M12 11v7" />,
  "excel.output": <path d="M14 3H6v18h6M14 3v4h4v4M8.5 10h5v5h-5zM8.5 12.5h5M11 10v5M18 14v7M15 18l3 3 3-3" />,
  // JSON files (S19, variant A chosen by the maintainer): a page with braces; writing with an arrow down, as csv.output.
  "json.input": (
    <path d="M6 3h8l4 4v14H6zM14 3v4h4M11 10.5c-1.2 0-1.7.5-1.7 1.6v.6c0 .7-.4 1.1-1.1 1.1.7 0 1.1.4 1.1 1.1v.6c0 1.1.5 1.6 1.7 1.6M13 10.5c1.2 0 1.7.5 1.7 1.6v.6c0 .7.4 1.1 1.1 1.1-.7 0-1.1.4-1.1 1.1v.6c0 1.1-.5 1.6-1.7 1.6" />
  ),
  "json.output": (
    <path d="M14 3H6v18h6M14 3v4h4v4M9.5 9.5c-1 0-1.4.4-1.4 1.3v.5c0 .6-.3.9-.9.9.6 0 .9.3.9.9v.5c0 .9.4 1.3 1.4 1.3M18 14v7M15 18l3 3 3-3" />
  ),
  "core.generate-rows": <path d="M4 6h16M4 12h16M4 18h16M19 3v6M16 6h6" />,
  "core.from-result": <path d="M4 12h11M11 8l4 4-4 4M15 4h5v16h-5" />,
  "core.to-result": <path d="M9 12h11M16 8l4 4-4 4M9 4H4v16h5" />,
  "core.convert": <path d="M5 8h11l-3-3M19 16H8l3 3" />,
  "core.filter": <path d="M4 5h16l-6 7v6l-4 2v-8z" />,
  "core.select-fields": <path d="M5 6h14M5 12h9M5 18h5M17 15l2 2 3-4" />,
  "core.constant": <path d="M12 5v14M5 12h14" />,
  // Mockup S16d (variant A): an expression {{ … }} that calculates; a record and its dropped copy; a list numbered from 1.
  "core.calculate": (
    <>
      <path d="M9 4c-2 0-3 1-3 3v2.5C6 11 5 12 3.5 12 5 12 6 13 6 14.5V17c0 2 1 3 3 3M15 4c2 0 3 1 3 3v2.5c0 1.5 1 2.5 2.5 2.5-1.5 0-2.5 1-2.5 2.5V17c0 2-1 3-3 3" />
      <path d="M10 12h4M12 10v4" />
    </>
  ),
  "core.dedupe": (
    <>
      <rect x="3" y="3" width="11" height="11" rx="2" />
      <path d="M10 17v2a2 2 0 0 0 2 2h7a2 2 0 0 0 2-2v-7a2 2 0 0 0-2-2h-2" strokeDasharray="2.4 2.2" />
    </>
  ),
  // Lookups (mockup S23b): a magnifier; for the database the cylinder of the category.
  "core.stream-lookup": (
    <>
      <circle cx="10" cy="10" r="5" />
      <path d="M14 14l6 6M8 10h4" />
    </>
  ),
  // Merge join (S23c, variant C chosen by the maintainer): two sets with their intersection, as for joins in SQL.
  "core.merge-join": (
    <>
      <circle cx="9" cy="12" r="6" />
      <circle cx="15" cy="12" r="6" />
      <path d="M12 7.2c1.2 1.1 1.9 2.8 1.9 4.8s-.7 3.7-1.9 4.8c-1.2-1.1-1.9-2.8-1.9-4.8s.7-3.7 1.9-4.8z" fill="currentColor" stroke="none" />
    </>
  ),
  "db.lookup": (
    <>
      <ellipse cx="10" cy="6" rx="6" ry="2.5" />
      <path d="M4 6v8c0 1.4 2.7 2.5 6 2.5M16 6v4" />
      <circle cx="17" cy="16" r="3" />
      <path d="M19.2 18.2L21 20" />
    </>
  ),
  // Database, shell and mail (proposal in the chat, chosen by the maintainer: variant B for
  // the first four, A for the others); the cylinder as in db.lookup.
  "db.execute": (
    <>
      <ellipse cx="10" cy="6" rx="6" ry="2.5" />
      <path d="M4 6v12c0 1.4 2.7 2.5 6 2.5M16 6v4" />
      <path d="M15 13v8l6-4z" />
    </>
  ),
  "db.query": (
    <>
      <ellipse cx="10" cy="6" rx="6" ry="2.5" />
      <path d="M4 6v12c0 1.4 2.7 2.5 6 2.5M16 6v4" />
      <path d="M15.5 14.5a2.5 2.5 0 1 1 3.2 2.4c-.5.2-.7.6-.7 1.1v.5M18 21.2v.1" />
    </>
  ),
  // Reading and writing with an arrow up and down, as csv.output writes with an arrow down.
  "db.table-input": (
    <>
      <ellipse cx="10" cy="6" rx="6" ry="2.5" />
      <path d="M4 6v12c0 1.4 2.7 2.5 6 2.5M16 6v4" />
      <path d="M18 21v-8M15 16l3-3 3 3" />
    </>
  ),
  "db.table-output": (
    <>
      <ellipse cx="10" cy="6" rx="6" ry="2.5" />
      <path d="M4 6v12c0 1.4 2.7 2.5 6 2.5M16 6v4" />
      <path d="M18 13v8M15 18l3 3 3-3" />
    </>
  ),
  "db.upsert": (
    <>
      <ellipse cx="10" cy="6" rx="6" ry="2.5" />
      <path d="M4 6v12c0 1.4 2.7 2.5 6 2.5M16 6v4" />
      <path d="M21 17a3 3 0 1 1-.9-2.1M20.5 12.5v2.4h-2.4" />
    </>
  ),
  "shell.run": (
    <>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M7 10l3 2-3 2M12 15h5" />
    </>
  ),
  "shell.powershell": <path d="M7 5h14l-4 14H3zM8.5 9.5l3 2.5-3 2.5M12.5 15h3" />,
  // JavaScript (S73a): braces around a dot.
  "core.script": <path d="M8 4c-2 0-2.5 1-2.5 3v2c0 1.5-.8 3-2.5 3 1.7 0 2.5 1.5 2.5 3v2c0 2 .5 3 2.5 3M16 4c2 0 2.5 1 2.5 3v2c0 1.5.8 3 2.5 3-1.7 0-2.5 1.5-2.5 3v2c0 2-.5 3-2.5 3M12 12h.01" />,
  // File transfer (S57a): a server with an arrow up (upload), down (download) or lines (list).
  "transfer.upload": <path d="M4 3h16v7H4zM7.5 6.5h.01M12 21v-8M8.5 16.5L12 13l3.5 3.5" />,
  "transfer.download": <path d="M4 3h16v7H4zM7.5 6.5h.01M12 13v8M8.5 17.5L12 21l3.5-3.5" />,
  "transfer.list": <path d="M4 3h16v7H4zM7.5 6.5h.01M6 14h12M6 17.5h12M6 21h8" />,
  "mail.send": (
    <>
      <rect x="3" y="5" width="14" height="11" rx="1.5" />
      <path d="M3 6.5l7 5 7-5M14 19.5h7.5M18.5 16.5l3 3-3 3" />
    </>
  ),
  // Map values and split field (S16f, icons chosen by the maintainer from variant A of the proposal).
  "core.map-values": (
    <>
      <path d="M3 6h5M3 12h5M3 18h5M10.5 6h3M10.5 12h3M10.5 18h3" />
      <path d="M12 4l2 2-2 2M12 10l2 2-2 2M12 16l2 2-2 2" />
      <path d="M16 6h5M16 12h5M16 18h5" />
    </>
  ),
  "core.split-field": (
    <>
      <rect x="3" y="8" width="18" height="8" rx="2" />
      <path d="M12 5v14" strokeDasharray="2 2" />
      <path d="M8 3L6 5l2 2M16 3l2 2-2 2" />
    </>
  ),
  "core.sequence": (
    <>
      <path d="M5 4l1.5-1v6M4.5 9h3" />
      <path d="M6 13v7M3.5 17.5L6 20l2.5-2.5" />
      <path d="M11 6h9M11 12h9M11 18h9" />
    </>
  ),
  "core.switch": <path d="M4 12h6M10 12l8-6M10 12l8 6M10 12h8" />,
  "core.sort": <path d="M7 4v16M4 17l3 3 3-3M14 6h6M14 12h4M14 18h2" />,
  "core.group-by": (
    <>
      <rect x="4" y="4" width="7" height="7" rx="1" />
      <rect x="13" y="4" width="7" height="7" rx="1" />
      <path d="M7.5 11v4h9v-4M12 15v5" />
    </>
  ),
};

/** Icon of an action or transform type; known types without an own icon get a generic one, unknown types "?". */
export function ActionIcon({ type, known, ...p }: P & { type: string; known: boolean }) {
  if (!known) {
    return (
      <Icon {...p}>
        <path d="M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-.7.3-1 1-1 1.7M12 17h.01" />
      </Icon>
    );
  }
  return (
    <Icon {...p}>
      {actionIcons[type] ?? (
        <>
          <rect x="5" y="5" width="14" height="14" rx="3" />
          <path d="M9 12h6" />
        </>
      )}
    </Icon>
  );
}

/** A document (Markdown, E17): a page with text lines. */
/** The list of runs (mockup S26d): a clock with an arrow back. */
export const HistoryIcon = (p: P) => (
  <Icon {...p}>
    <path d="M3 12a9 9 0 1 0 3-6.7L3 8" />
    <path d="M3 3v5h5M12 7v5l3 2" />
  </Icon>
);

/** Triggers of runs in the list (mockup S26d): schedule, http, runnel run, editor. */
export const ClockIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3 2" />
  </Icon>
);

export const GlobeIcon = (p: P) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
  </Icon>
);

export const TerminalIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 6l5 5-5 5M12 18h8" />
  </Icon>
);

/** Runs of the error workflow (trigger error, ADR 0027). */
export const AlertIcon = (p: P) => (
  <Icon {...p}>
    <path d="M12 3L2 20h20z" />
    <path d="M12 10v4M12 17v.01" />
  </Icon>
);

export const PencilIcon = (p: P) => (
  <Icon {...p}>
    <path d="M4 20h4L19 9l-4-4L4 16z" />
  </Icon>
);

export const DocumentIcon = (p: P) => (
  <Icon {...p}>
    <path d="M6 3h8l4 4v14H6z" />
    <path d="M14 3v4h4M9 12h6M9 16h4" />
  </Icon>
);

export const ImageIcon = (p: P) => (
  <Icon {...p}>
    <rect x="3" y="5" width="18" height="14" rx="2" />
    <circle cx="9" cy="10" r="1.8" />
    <path d="M21 16l-5-5-8 8" />
  </Icon>
);

/** Icon of a file by its kind (workflow, environment, project …). */
export function KindIcon({ kind, ...p }: P & { kind: string | undefined }) {
  switch (kind) {
    case "workflow":
      return <WorkflowIcon {...p} />;
    case "pipeline":
      return <PipelineIcon {...p} />;
    case "environment":
      return <LinesIcon {...p} />;
    case "project":
      return <ProjectIcon {...p} />;
    case "trigger":
      return <TriggerIcon {...p} />;
    case "document":
      return <DocumentIcon {...p} />;
    case "image":
      return <ImageIcon {...p} />;
    case "runs":
      return <HistoryIcon {...p} />;
    default:
      return <FileIcon {...p} />;
  }
}

/** The sign of a connection type (mockup S39b): cylinder for databases, a page for SQLite, globe for http, envelope for smtp, two servers for sftp. */
export function ConnectionIcon({ type, ...p }: P & { type: string }) {
  switch (type) {
    case "sqlite":
      return (
        <Icon {...p}>
          <path d="M6 3h8l4 4v14H6zM14 3v4h4" />
          <ellipse cx="12" cy="12" rx="3.5" ry="1.4" />
          <path d="M8.5 12v4c0 .8 1.6 1.4 3.5 1.4s3.5-.6 3.5-1.4v-4" />
        </Icon>
      );
    case "http":
      return (
        <Icon {...p}>
          <circle cx="12" cy="12" r="9" />
          <path d="M3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.6-3.8-9S9.5 5.6 12 3z" />
        </Icon>
      );
    case "sftp":
      return (
        <Icon {...p}>
          <path d="M4 4h16v6H4zM4 14h16v6H4zM7.5 7h.01M7.5 17h.01M12 10v4" />
        </Icon>
      );
    case "smtp":
      return (
        <Icon {...p}>
          <rect x="3" y="5" width="18" height="14" rx="2" />
          <path d="M3 7l9 6 9-6" />
        </Icon>
      );
  }
  return (
    <Icon {...p}>
      <ellipse cx="12" cy="6" rx="7" ry="2.8" />
      <path d="M5 6v12c0 1.5 3.1 2.8 7 2.8s7-1.3 7-2.8V6M5 12c0 1.5 3.1 2.8 7 2.8s7-1.3 7-2.8" />
    </Icon>
  );
}

/** Test connection (mockup S39b): a plug. */
export const PlugIcon = (p: P) => (
  <Icon {...p}>
    <path d="M9 7V3M15 7V3M6 7h12v4a6 6 0 0 1-12 0zM12 17v4" />
  </Icon>
);

/** Trusting a plugin (mockup S52): a shield with a tick. */
export const ShieldIcon = (p: P) => (
  <Icon {...p}>
    <path d="M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6z" />
    <path d="M8.5 12l2.5 2.5 4.5-5" />
  </Icon>
);

/** The form view of a connection (mockup S39b). */
export const FormIcon = (p: P) => (
  <Icon {...p}>
    <rect x="4" y="4" width="16" height="16" rx="2" />
    <path d="M8 9h8M8 13h8M8 17h5" />
  </Icon>
);

/** Kinds of triggers (mockup E20): calendar (cron), circular arrow (interval), folder with arrow (file), arrow into a box (http). */
export const TriggerTypeIcon = ({ type, ...p }: P & { type: string }) => (
  <Icon {...p}>
    {type === "cron" ? (
      <path d="M6 5h12a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2zM4 10h16M9 3v4M15 3v4" />
    ) : type === "interval" ? (
      <path d="M20 11a8 8 0 1 0-2.3 5.7M20 5v6h-6" />
    ) : type === "file" ? (
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM12 10v6M9.5 13.5L12 16l2.5-2.5" />
    ) : (
      <path d="M4 12h11M11 8l4 4-4 4M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3" />
    )}
  </Icon>
);
