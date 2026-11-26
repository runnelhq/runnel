import { useState } from "react";
import { categoryColor, type Schema } from "../catalog";
import { useSchema } from "../reference";
import { Dialog } from "./Dialog";
import { ConnectionIcon } from "./Icons";

/**
 * "New → Connection" (mockup S39b, state "new"): name and type; the file gets the
 * required fields of the type with their examples, the form shows the rest.
 */
export function NewConnectionDialog(props: { exists: (path: string) => boolean; onCreate: (path: string, text: string) => Promise<string | null>; onCancel: () => void }) {
  const schema = useSchema("connection");
  const variants = schema?.properties?.with?.["x-runnel-variants"] ?? {};
  const types = Object.keys(variants);
  const [name, setName] = useState("new-connection");
  const [type, setType] = useState("");
  const [error, setError] = useState<string | null>(null);
  const path = `connections/${name}.yaml`;
  const valid = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(name);
  const problem = !valid ? "Use letters, digits, “-”, “_” and “.”" : props.exists(path) ? `${path} already exists` : error;
  const create = async () => {
    if (!type || problem) return;
    setError(await props.onCreate(path, connectionTemplate(name, type, variants[type])));
  };
  return (
    <Dialog
      title="New connection"
      form
      wide
      icon={<ConnectionIcon type="" width={14} height={14} />}
      iconClass="bg-accent text-accent-foreground"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        { label: "Create", tone: "primary", disabled: !type || !!problem, onClick: () => void create() },
      ]}
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
        {types.map((t) => {
          const [label, ...rest] = (variants[t].description ?? t).split(": ");
          const net = t === "http" || t === "smtp" || t === "sftp" || t === "ftps" || t === "ftp";
          return (
            <button
              key={t}
              type="button"
              role="radio"
              aria-checked={type === t}
              onClick={() => setType(t)}
              className="flex cursor-pointer items-start gap-2.5 rounded-md border border-border bg-background p-2.5 text-left aria-checked:border-primary aria-checked:ring-1 aria-checked:ring-primary"
            >
              <span className="grid size-7 shrink-0 place-items-center rounded-md text-white" style={{ background: categoryColor[net ? "network" : "database"] }}>
                <ConnectionIcon type={t} width={16} height={16} />
              </span>
              <span className="min-w-0">
                <b className="block text-xs">{label}</b>
                <span className="block text-[10px] leading-snug text-muted-foreground">{rest.join(": ")}</span>
              </span>
            </button>
          );
        })}
      </div>
      <p className="mt-3 mb-0 text-2xs text-muted-foreground">The form shows the fields of the type next.</p>
    </Dialog>
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
