import icon from "../../../design/logo/runnel-icon.svg";
import wordmark from "../../../design/logo/runnel-wordmark.svg";
import wordmarkInverse from "../../../design/logo/runnel-wordmark-inverse.svg";
import type { ProjectInfo } from "../api";
import type { ThemeChoice } from "../theme";
import { MonitorIcon, MoonIcon, SunIcon } from "./Icons";

const themes: { value: ThemeChoice; label: string; title: string; Icon: typeof SunIcon }[] = [
  { value: "system", label: "System", title: "Follow Windows setting", Icon: MonitorIcon },
  { value: "light", label: "Light", title: "Light", Icon: SunIcon },
  { value: "dark", label: "Dark", title: "Dark", Icon: MoonIcon },
];

export function Header(props: {
  project: ProjectInfo | null;
  theme: ThemeChoice;
  onTheme: (t: ThemeChoice) => void;
}) {
  return (
    <header className="flex items-center gap-4 border-b border-border bg-background px-4">
      <div className="flex items-center gap-2">
        <img src={icon} alt="" width={20} height={20} />
        <img src={wordmark} alt="Runnel" className="h-[15px] [[data-theme=dark]_&]:hidden" />
        <img src={wordmarkInverse} alt="Runnel" className="hidden h-[15px] [[data-theme=dark]_&]:inline" />
      </div>
      <div className="h-6 w-px bg-border" />
      <div className="flex min-w-0 items-baseline gap-2" data-testid="project">
        <span className="label-caps">Project</span>
        <span className="font-semibold">{props.project?.name || "…"}</span>
        <span className="truncate font-mono text-xs text-muted-foreground">{props.project?.dir}</span>
      </div>
      <div className="flex-1" />
      <div
        role="group"
        aria-label="Theme"
        className="inline-flex gap-0.5 rounded-md border border-border bg-muted p-0.5"
      >
        {themes.map(({ value, label, title, Icon }) => (
          <button
            key={value}
            type="button"
            title={title}
            aria-pressed={props.theme === value}
            onClick={() => props.onTheme(value)}
            className="inline-flex cursor-pointer items-center gap-1.5 rounded-sm px-2 py-[3px] text-xs text-muted-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm"
          >
            <Icon width={14} height={14} />
            {label}
          </button>
        ))}
      </div>
    </header>
  );
}
