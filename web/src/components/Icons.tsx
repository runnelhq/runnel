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
    <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
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
  "core.dummy": <path d="M4 6h5l4 6-4 6H4M13 12h7" />,
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
};

/** Icon of an action type; known types without an own icon get a generic one, unknown types "?". */
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

/** Icon of a file by its kind (workflow, environment, project …). */
export function KindIcon({ kind, ...p }: P & { kind: string | undefined }) {
  switch (kind) {
    case "workflow":
    case "pipeline":
      return <WorkflowIcon {...p} />;
    case "environment":
      return <LinesIcon {...p} />;
    case "project":
      return <ProjectIcon {...p} />;
    default:
      return <FileIcon {...p} />;
  }
}
