import { useState } from "react";
import { createPortal } from "react-dom";
import { useConnectionTypes, type ConnectionType, type Schema } from "../catalog";
import { usePluginVersion, type HelpKind } from "../help";
import { useSchema } from "../reference";
import { Dialog } from "./Dialog";
import { CodeText, ConnectionTile, HelpPanel } from "./HelpPanel";
import { ConnectionIcon, PlugIcon } from "./Icons";

/**
 * "New → Connection" (mockup S39b, state "new"; mockup E15f): name and type, each type with
 * its description and whether it offers Test connection; the file gets the required
 * fields of the type with their examples, the form shows the rest. "?" on a card and F1
 * show the help of the type beside the dialog.
 */
export function NewConnectionDialog(props: { exists: (path: string) => boolean; onCreate: (path: string, text: string) => Promise<string | null>; onCancel: () => void }) {
  const schema = useSchema("connection");
  const variants = schema?.properties?.with?.["x-runnel-variants"] ?? {};
  const catalog = useConnectionTypes();
  const types = Object.keys(variants);
  const [name, setName] = useState("new-connection");
  const [type, setType] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [help, setHelp] = useState<{ type: string; kind: HelpKind } | null>(null);
  const path = `connections/${name}.yaml`;
  const valid = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(name);
  const problem = !valid ? "Use letters, digits, “-”, “_” and “.”" : props.exists(path) ? `${path} already exists` : error;
  const create = async () => {
    if (!type || problem) return;
    setError(await props.onCreate(path, connectionTemplate(name, type, variants[type])));
  };
  // The catalog describes the types (E15e); until it is loaded, the schema does.
  const info = (t: string): ConnectionType => {
    const [label, ...rest] = (variants[t]?.description ?? t).split(": ");
    return catalog.get(t) ?? { type: t, label, description: rest.join(": ") };
  };
  const chosen = type ? info(type) : null;
  return (
    <Dialog
      title="New connection"
      form
      wide="xl"
      icon={<ConnectionIcon type="" width={14} height={14} />}
      iconClass="bg-accent text-accent-foreground"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        { label: "Create", tone: "primary", disabled: !type || !!problem, onClick: () => void create() },
      ]}
    >
      <div
        onKeyDown={(e) => {
          if (e.key === "F1" && type) {
            e.preventDefault(); // instead of the help of the browser
            setHelp({ type, kind: "connection" });
          }
        }}
      >
        <p className="mt-0 mb-3 text-xs text-muted-foreground">
          A connection holds the address and sign-in of a system in one place; workflows name it with <span className="font-mono">connection:</span>.
        </p>
        <label className="mb-1 block text-xs font-medium" htmlFor="new-connection-name">
          Name
        </label>
        <input
          id="new-connection-name"
          value={name}
          onChange={(e) => {
            setName(e.target.value.trim());
            setError(null);
          }}
          onKeyDown={(e) => e.key === "Enter" && void create()}
          aria-invalid={!!problem}
          className="block h-[30px] w-full rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:border-ring focus:outline-2 focus:outline-ring/25"
        />
        <div className={"mt-1 mb-3 text-2xs " + (problem ? "text-destructive" : "text-muted-foreground")}>
          {problem ? `✖ ${problem}` : <>Creates <span className="font-mono">{path}</span></>}
        </div>
        <div className="mb-1 text-xs font-medium" id="new-connection-type">
          Type
        </div>
        <div role="radiogroup" aria-labelledby="new-connection-type" className="grid grid-cols-3 gap-2">
          {types.map((t) => (
            <TypeCard key={t} c={info(t)} checked={type === t} onCheck={() => setType(t)} onHelp={() => setHelp({ type: t, kind: "connection" })} />
          ))}
        </div>
        {chosen && (
          <div data-testid="chosen-test" className="mt-2.5 flex items-start gap-2.5 rounded-md bg-muted px-[11px] py-[9px] text-2xs leading-normal text-foreground">
            <span className="pt-px text-muted-foreground">
              <PlugIcon width={14} height={14} />
            </span>
            {chosen.test ? (
              <span>
                <b className="font-semibold">Test connection for {chosen.label}:</b>{" "}
                <CodeText text={chosen.test.charAt(0).toLowerCase() + chosen.test.slice(1)} names={Object.keys(chosen.schema?.properties ?? {})} />
              </span>
            ) : (
              <span>
                <b className="font-semibold">No test for {chosen.label}.</b> Test connection is not offered for this type.
              </span>
            )}
            <button
              type="button"
              onClick={() => setHelp({ type: chosen.type, kind: "connection" })}
              className="ml-auto inline-flex h-6 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2 text-2xs font-medium hover:bg-muted"
            >
              Help <kbd className="font-mono text-[10px] opacity-75">F1</kbd>
            </button>
          </div>
        )}
        <p className="mt-3 mb-0 text-2xs text-muted-foreground">The form shows the fields of the type next.</p>
      </div>
      {help &&
        createPortal(
          <div className="fixed inset-y-0 right-0 z-[60] flex shadow-lg">
            <HelpPanel target={{ ...help, back: false }} onOpen={(t, kind) => setHelp({ type: t, kind })} onClose={() => setHelp(null)} />
          </div>,
          document.body,
        )}
    </Dialog>
  );
}

/** A type in "New → Connection": tile, label, description, Test or No test, the plugin; "?" for its help (mockup E15f). */
function TypeCard(props: { c: ConnectionType; checked: boolean; onCheck: () => void; onHelp: () => void }) {
  const { c } = props;
  const version = usePluginVersion(c.plugin);
  return (
    <div className="group relative">
      <button
        type="button"
        role="radio"
        aria-checked={props.checked}
        onClick={props.onCheck}
        className="flex h-full w-full cursor-pointer items-start gap-2.5 rounded-md border border-border bg-background p-2.5 text-left aria-checked:border-primary aria-checked:ring-1 aria-checked:ring-primary"
      >
        <ConnectionTile type={c.type} plugin={c.plugin} size={28} icon={16} radius="rounded-md" />
        <span className="min-w-0">
          <b className="block pr-5 text-xs">{c.label}</b>
          <span className="block text-[10px] leading-snug text-muted-foreground">{c.description}</span>
          <span className="mt-1 flex flex-wrap gap-2 text-[10px] text-muted-foreground">
            {c.test ? (
              <span className="inline-flex items-center gap-[3px]">
                <PlugIcon width={11} height={11} />
                Test
              </span>
            ) : (
              <span>No test</span>
            )}
            {c.plugin && (
              <span className="inline-flex items-center gap-[3px]">
                <PlugIcon width={11} height={11} />
                Plugin {c.plugin}
                {version && ` ${version}`}
              </span>
            )}
          </span>
        </span>
      </button>
      <button
        type="button"
        aria-label={`Help for ${c.label}`}
        title={`Help for ${c.label} (F1)`}
        onClick={props.onHelp}
        className={
          "absolute top-1.5 right-1.5 grid size-[22px] cursor-pointer place-items-center rounded-md border border-border bg-background text-xs leading-none font-semibold text-muted-foreground hover:bg-accent hover:text-accent-foreground focus:opacity-100 group-hover:opacity-100 " +
          (props.checked ? "opacity-100" : "opacity-0")
        }
      >
        ?
      </button>
    </div>
  );
}

/** The text of a new connection: its required fields with their examples. */
export function connectionTemplate(name: string, type: string, variant: Schema | undefined): string {
  const lines = [`kind: Connection`, `apiVersion: runnel/v1`, `name: ${name}`, `type: ${type}`];
  const required = variant?.required ?? [];
  if (required.length > 0) {
    lines.push("with:");
    for (const key of required) {
      const s = variant?.properties?.[key];
      let value = s?.examples?.[0] ?? s?.default ?? "";
      if (type === "sqlite" && key === "path") value = `out/${name}.db`;
      lines.push(`  ${key}: ${JSON.stringify(String(value))}`);
    }
  }
  return lines.join("\n") + "\n";
}
