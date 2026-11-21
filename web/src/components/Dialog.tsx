import { useEffect, useRef, type ReactNode } from "react";

export interface DialogAction {
  label: ReactNode;
  onClick: () => void;
  tone?: "primary" | "danger";
  disabled?: boolean;
}

/**
 * Modal dialog (mockup E6a, conflict dialog): Escape and the backdrop cancel. A form
 * dialog (run dialog, mockup E8) focuses its first field instead of the first button.
 */
export function Dialog(props: {
  title: string;
  children: ReactNode;
  actions: DialogAction[];
  onCancel: () => void;
  /** Symbol before the title; a warning sign by default. */
  icon?: ReactNode;
  iconClass?: string;
  form?: boolean;
  /** Wider, e.g. for lists of paths. */
  wide?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const cancel = useRef(props.onCancel);
  cancel.current = props.onCancel;
  const form = props.form;
  useEffect(() => {
    const first = form ? ref.current?.querySelector<HTMLElement>("input, select") : null;
    (first ?? ref.current?.querySelector<HTMLButtonElement>("button"))?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && cancel.current();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [form]);

  const tone = (t?: string) =>
    t === "primary"
      ? "border-primary bg-primary text-primary-foreground"
      : t === "danger"
        ? "border-destructive bg-background text-destructive hover:bg-danger-soft"
        : "border-input bg-background hover:bg-muted";

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-tiefe-950/45"
      onMouseDown={(e) => e.target === e.currentTarget && props.onCancel()}
    >
      <div
        ref={ref}
        role={props.form ? "dialog" : "alertdialog"}
        aria-modal="true"
        aria-label={props.title}
        className={`${props.wide ? "w-[600px]" : "w-[480px]"} max-w-[calc(100vw-32px)] rounded-xl border border-border bg-popover px-6 py-5 text-popover-foreground shadow-lg`}
      >
        <h2 className="m-0 mb-2 flex items-center gap-2.5 text-md font-semibold">
          <span className={`inline-grid size-7 place-items-center rounded-full text-sm ${props.iconClass ?? "bg-warning-soft text-warning"}`}>
            {props.icon ?? "⚠"}
          </span>
          {props.title}
        </h2>
        <div className="text-muted-foreground [&_b]:text-foreground [&_p]:m-0 [&_p]:mb-2.5 [&_ul]:m-0 [&_ul]:mb-4 [&_ul]:pl-[18px]">
          {props.children}
        </div>
        <div className="mt-4 flex justify-end gap-2">
          {props.actions.map((a, i) => (
            <button
              key={i}
              type="button"
              onClick={a.onClick}
              disabled={a.disabled}
              className={`inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium disabled:cursor-default disabled:opacity-50 ${tone(a.tone)}`}
            >
              {a.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
