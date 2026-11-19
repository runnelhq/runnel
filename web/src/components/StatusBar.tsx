import type { Connection } from "../api";

const states: Record<Connection, { symbol: string; text: string; className: string }> = {
  connecting: { symbol: "…", text: "Connecting", className: "text-muted-foreground" },
  connected: { symbol: "●", text: "Connected", className: "text-success" },
  disconnected: { symbol: "⚠", text: "Disconnected", className: "text-warning" },
};

export function StatusBar(props: { connection: Connection; version: string | null; summary: string | null; dirty?: boolean }) {
  const s = states[props.connection];
  return (
    <footer className="flex items-center gap-4 border-t border-border bg-muted px-3 text-xs text-muted-foreground">
      <span className={s.className} data-testid="connection">
        {s.symbol} {s.text}
      </span>
      <span className="font-mono text-2xs">{window.location.host}</span>
      {props.summary && <span data-testid="file-summary">{props.summary}</span>}
      {props.dirty && (
        <span className="text-foreground" data-testid="unsaved">
          ● Unsaved changes
        </span>
      )}
      <span className="flex-1" />
      {props.version && <span>Runnel {props.version}</span>}
    </footer>
  );
}
