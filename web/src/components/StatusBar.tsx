import { useEffect, useState } from "react";
import type { Connection } from "../api";
import { formatDuration, type RunView } from "../run";

const states: Record<Connection, { symbol: string; text: string; className: string }> = {
  connecting: { symbol: "…", text: "Connecting", className: "text-muted-foreground" },
  connected: { symbol: "●", text: "Connected", className: "text-success" },
  disconnected: { symbol: "⚠", text: "Disconnected", className: "text-warning" },
};

export function StatusBar(props: {
  connection: Connection;
  version: string | null;
  summary: string | null;
  dirty?: boolean;
  /** The current or last run of the open workflow (mockup E8). */
  run?: RunView | null;
}) {
  const s = states[props.connection];
  return (
    <footer className="flex items-center gap-4 border-t border-border bg-muted px-3 text-xs text-muted-foreground">
      <span className={s.className} data-testid="connection">
        {s.symbol} {s.text}
      </span>
      <span className="font-mono text-2xs">{window.location.host}</span>
      {props.summary && <span data-testid="file-summary">{props.summary}</span>}
      {props.run && <RunStatus run={props.run} />}
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

const runText: Record<string, { symbol: string; text: string; className: string }> = {
  running: { symbol: "●", text: "Running", className: "text-status-running" },
  succeeded: { symbol: "✔", text: "Last run succeeded", className: "text-status-success" },
  failed: { symbol: "✖", text: "Last run failed", className: "text-status-failed" },
  cancelled: { symbol: "■", text: "Last run cancelled", className: "" },
};

/** "● Running (dev) · 1.4 s" or "✔ Last run succeeded · 2.0 s". */
function RunStatus({ run }: { run: RunView }) {
  const [now, setNow] = useState(() => Date.now());
  const running = run.status === "running";
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => setNow(Date.now()), 200);
    return () => clearInterval(t);
  }, [running]);
  const t = runText[run.status];
  const ms = run.durationMs ?? (running ? Math.max(0, now - run.started) : undefined);
  return (
    <span data-testid="run-status" className={t.className} title={run.error}>
      {t.symbol} {t.text}
      {running && run.environment ? ` (${run.environment})` : ""}
      {ms !== undefined ? ` · ${formatDuration(ms)}` : ""}
    </span>
  );
}
