import { Component, type ErrorInfo, type ReactNode } from "react";
import { ReloadIcon } from "./Icons";

/**
 * ErrorBoundary keeps an error while drawing one part of the editor (a file, the sidebar)
 * in that part: the rest stays usable, and "Try again" draws the part anew. Without it,
 * React drops the whole page and only reloading helps.
 */
export class ErrorBoundary extends Component<
  { children: ReactNode; fallback: (error: Error, retry: () => void) => ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: unknown) {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error("Runnel editor:", error, info.componentStack);
  }

  render() {
    const { error } = this.state;
    return error ? this.props.fallback(error, () => this.setState({ error: null })) : this.props.children;
  }
}

function Button(props: { children: ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
    >
      {props.children}
    </button>
  );
}

const reload = () => window.location.reload();

/** The card of an error, as for a workflow that "can't be shown as a graph". */
function Card(props: { title: string; text: string; detail: string; children: ReactNode }) {
  return (
    <div className="max-w-[440px] rounded-xl border border-border bg-card px-7 py-6 text-center shadow-md">
      <span className="inline-grid size-10 place-items-center rounded-full bg-danger-soft text-lg font-bold text-destructive">✖</span>
      <h2 className="mt-3 mb-1 text-md font-semibold">{props.title}</h2>
      <p className="mt-0 mb-3 text-muted-foreground">{props.text}</p>
      <pre className="mt-0 mb-4 rounded-md bg-muted px-2.5 py-2 text-left text-xs whitespace-pre-wrap">{props.detail}</pre>
      <div className="flex justify-center gap-2">{props.children}</div>
    </div>
  );
}

/** In place of a file whose view failed; the other tabs stay open. */
export function FileError(props: { path: string; error: Error; onRetry: () => void }) {
  return (
    <div className="grid flex-1 place-items-center p-8" role="alert" data-testid="file-error">
      <Card
        title="This file can’t be shown"
        text="An error in the editor stopped this view. Other files stay open; unsaved changes of this file may be lost."
        detail={`${props.path}\n${props.error.message}`}
      >
        <Button onClick={props.onRetry}>
          <ReloadIcon width={14} height={14} />
          Try again
        </Button>
        <Button onClick={reload}>Reload page</Button>
      </Card>
    </div>
  );
}

/** In place of the palette or the reference in the sidebar. */
export function PanelError(props: { error: Error; onRetry: () => void }) {
  return (
    <section role="alert" aria-label="Error" className="flex min-h-[160px] flex-1 flex-col gap-2 border-t border-sidebar-border px-4 py-3">
      <p className="m-0 text-xs">
        <span className="font-bold text-destructive">✖</span> This panel can’t be shown: {props.error.message}
      </p>
      <div>
        <Button onClick={props.onRetry}>
          <ReloadIcon width={14} height={14} />
          Try again
        </Button>
      </div>
    </section>
  );
}

/** The last resort around the whole editor, for an error outside a file or panel. */
export function PageError(props: { error: Error }) {
  return (
    <div className="grid h-screen place-items-center bg-background p-8 text-foreground" role="alert">
      <Card title="The editor stopped because of an error" text="Saved files are not affected. Reload the page to continue." detail={props.error.message}>
        <Button onClick={reload}>
          <ReloadIcon width={14} height={14} />
          Reload page
        </Button>
      </Card>
    </div>
  );
}
