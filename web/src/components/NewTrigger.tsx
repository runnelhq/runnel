import { useState } from "react";
import { Dialog } from "./Dialog";
import { TriggerIcon } from "./Icons";
import { inputClass } from "./PropertiesPanel";
import { TriggerTypeCard, triggerKinds } from "./TriggerForm";

/**
 * "New → Trigger" (mockup E20, state "new"): name, type and target; the trigger starts
 * disabled with the required fields of its type, the form shows the rest.
 */
export function NewTriggerDialog(props: {
  dir: string;
  targets: string[];
  exists: (path: string) => boolean;
  onCreate: (path: string, text: string) => Promise<string | null>;
  onCancel: () => void;
}) {
  const [name, setName] = useState("new-trigger");
  const [type, setType] = useState("cron");
  const [target, setTarget] = useState(props.targets[0] ?? "workflows/main.workflow.yaml");
  const [error, setError] = useState<string | null>(null);
  const path = `${props.dir}/${name}.yaml`;
  const valid = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(name);
  const problem = !valid ? "Use letters, digits, “-”, “_” and “.”" : props.exists(path) ? `${path} already exists` : error;
  const create = async () => {
    if (problem) return;
    setError(await props.onCreate(path, triggerTemplate(name, type, target)));
  };
  return (
    <Dialog
      title="New trigger"
      form
      wide
      icon={<TriggerIcon width={14} height={14} />}
      iconClass="bg-accent text-accent-foreground"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        { label: "Create", tone: "primary", disabled: !!problem, onClick: () => void create() },
      ]}
    >
      <p className="mt-0 mb-3 text-xs text-muted-foreground">
        A trigger starts a workflow or pipeline while <span className="font-mono">runnel serve</span> or the service runs.
      </p>
      <label className="mb-1 block text-xs font-medium" htmlFor="new-trigger-name">
        Name
      </label>
      <input
        id="new-trigger-name"
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
      <div className="mb-1 text-xs font-medium" id="new-trigger-type">
        Type
      </div>
      <div role="radiogroup" aria-labelledby="new-trigger-type" className="mb-3 grid grid-cols-4 gap-2">
        {triggerKinds.map(([t, label, text]) => (
          <TriggerTypeCard key={t} type={t} label={label} text={text} checked={t === type} onClick={() => setType(t)} />
        ))}
      </div>
      <label className="mb-1 block text-xs font-medium" htmlFor="new-trigger-target">
        Target
      </label>
      <select id="new-trigger-target" value={target} onChange={(e) => setTarget(e.target.value)} className={inputClass(false, true) + " cursor-pointer"}>
        {props.targets.map((t) => (
          <option key={t} value={t}>
            {t}
          </option>
        ))}
      </select>
      <p className="mt-3 mb-0 text-2xs text-muted-foreground">Starts disabled; enable it in the form when it fits.</p>
    </Dialog>
  );
}

/** The text of a new trigger: disabled, with the required fields of its type. */
export function triggerTemplate(name: string, type: string, target: string): string {
  const with_: Record<string, string> = {
    cron: '  schedule: "0 2 * * 1-5"\n',
    interval: "  every: 1h\n",
    file: "  path: inbox\n  pattern: \"*.csv\"\n",
    http: `  method: POST\n  path: /hooks/${name}\n  auth:\n    token: \${secret:${name}-token}\n`,
  };
  return `kind: Trigger\napiVersion: runnel/v1\nname: ${name}\ntype: ${type}\ntarget: ${target}\nenabled: false\nwith:\n${with_[type]}`;
}
