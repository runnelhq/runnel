import { useEffect, useState } from "react";
import { APIError, fetchBlob, kindLabel } from "../files";
import { formatBytes } from "../plugins";
import { Badge, FileNotice } from "./FileView";
import { LockIcon } from "./Icons";

type State =
  | { status: "loading" }
  | { status: "loaded"; url: string; size: number }
  | { status: "error"; code?: number; message: string };

/**
 * Shows an image of the project (R8): loaded through the file API as a blob, centered and
 * scaled down to the view (never up), with its dimensions and file size. SVG is shown only
 * as <img>, which runs no scripts. The view stays mounted while its tab is open.
 */
export function ImageView(props: {
  path: string;
  active: boolean;
  epoch: number;
  onSummary: (summary: string | null) => void;
  /** No run, palette or reference for an image: cleared while its tab is active. */
  onRun: (run: null) => void;
  onPalette: (target: null) => void;
  onReference: (target: null) => void;
  onReloadExplorer: () => void;
}) {
  const { path, active, epoch, onSummary, onRun, onPalette, onReference } = props;
  const [state, setState] = useState<State>({ status: "loading" });
  const [dims, setDims] = useState<{ width: number; height: number } | null>(null);

  // Again after a reconnect: the file may have changed in the meantime.
  useEffect(() => {
    let cancelled = false;
    let url: string | null = null;
    fetchBlob(path).then(
      (blob) => {
        if (cancelled) return;
        url = URL.createObjectURL(blob);
        setState({ status: "loaded", url, size: blob.size });
      },
      (err: unknown) =>
        !cancelled &&
        setState({ status: "error", code: err instanceof APIError ? err.status : undefined, message: err instanceof Error ? err.message : String(err) }),
    );
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [path, epoch]);

  const info = state.status === "loaded" && dims ? `${dims.width} × ${dims.height} px · ${formatBytes(state.size)}` : null;
  useEffect(() => {
    if (active) onSummary(info && `${kindLabel.image} · ${info}`);
  }, [active, info, onSummary]);

  useEffect(() => {
    if (!active) return;
    onRun(null);
    onPalette(null);
    onReference(null);
  }, [active, onRun, onPalette, onReference]);

  if (!active) return null;
  if (state.status === "error") return <FileNotice path={path} code={state.code} message={state.message} onReloadExplorer={props.onReloadExplorer} />;

  const slash = path.lastIndexOf("/");
  const name = path.slice(slash + 1);
  return (
    <>
      <div className="flex h-10 shrink-0 items-center gap-3 border-b border-border px-4">
        <span className="font-mono text-xs whitespace-nowrap text-muted-foreground" data-testid="crumbs" title={path}>
          {slash >= 0 && `${path.slice(0, slash).split("/").join(" / ")} / `}
          <b className="font-medium text-foreground">{name}</b>
        </span>
        <Badge testId="kind">{kindLabel.image}</Badge>
        <Badge title="This file can be changed in your image editor">
          <LockIcon width={12} height={12} />
          Read-only
        </Badge>
      </div>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 overflow-hidden p-6">
        {state.status === "loaded" && (
          <img
            src={state.url}
            alt={name}
            data-testid="image"
            className="min-h-0 max-w-full object-contain"
            onLoad={(e) => setDims({ width: e.currentTarget.naturalWidth, height: e.currentTarget.naturalHeight })}
            onError={() => setState({ status: "error", message: "it is not a valid image" })}
          />
        )}
        <span className="shrink-0 text-xs text-muted-foreground" data-testid="image-info">
          {info ?? "Loading…"}
        </span>
      </div>
    </>
  );
}
