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
  "file.wait": (
    <>
      <path d={filePage} />
      <circle cx="18" cy="18" r="3.5" />
      <path d="M18 16.3V18l1.2 1" />
    </>
  ),
  "file.list": <path d={`${folderPath}M7.5 11h9M7.5 14.5h6`} />,
  "file.mkdir": <path d={`${folderPath}M12 9.5v6M9 12.5h6`} />,
  "file.move": <path d={`${folderPath}M9 13h7M13.5 10.5L16 13l-2.5 2.5`} />,
  // Network: answering the caller; the same sign for the action and the transform.
  "http.respond": <path d={replyPath} />,
  "http.response": <path d={replyPath} />,
  // Transforms (design/mockups/s25a-pipeline.html); core.log and core.dummy as above.
  "csv.input": <path d="M6 3h8l4 4v14H6zM14 3v4h4M9 13h6M9 17h6" />,
  "csv.output": <path d="M6 3h8l4 4v14H6zM14 3v4h4M12 11v7M9 15l3 3 3-3" />,
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
    default:
      return <FileIcon {...p} />;
  }
}
