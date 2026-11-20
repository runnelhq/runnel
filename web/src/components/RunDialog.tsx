import { useState } from "react";
import type { ProjectInfo } from "../api";
import type { RunSettings } from "../run";
import type { Parameter } from "../workflow";
import { Dialog } from "./Dialog";
import { PlayIcon } from "./Icons";

const inputClass =
  "block w-full min-h-[30px] rounded-md border border-input bg-background px-2 text-sm text-foreground outline-none focus:border-ring focus:outline-2 focus:outline-ring/25";

/**
 * Environment and parameters of a run (mockup E8, state "run"). The values are
 * remembered per workflow; empty parameters use their default value.
 */
export function RunDialog(props: {
  path: string;
  project: ProjectInfo | null;
  parameters: Parameter[];
  initial: RunSettings | null;
  onRun: (settings: RunSettings) => void;
  onCancel: () => void;
}) {
  const envs = props.project?.environments ?? [];
  const def = props.project?.defaultEnvironment ?? "";
  const [environment, setEnvironment] = useState(() => {
    const e = props.initial?.environment ?? "";
    return e === "" || envs.includes(e) ? e : "";
  });
  const [params, setParams] = useState<Record<string, string>>(() =>
    Object.fromEntries(props.parameters.map((p) => [p.name, props.initial?.params[p.name] ?? ""])),
  );
  const missing = props.parameters.filter((p) => p.required && !p.default && (params[p.name] ?? "") === "");
  const run = () => missing.length === 0 && props.onRun({ environment, params });
  const name = props.path.slice(props.path.lastIndexOf("/") + 1);

  return (
    <Dialog
      form
      title={`Run ${name}`}
      icon={<PlayIcon width={14} height={14} />}
      iconClass="bg-success-soft text-status-success"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        {
          label: (
            <>
              <PlayIcon width={14} height={14} />
              Run
            </>
          ),
          tone: "primary",
          disabled: missing.length > 0,
          onClick: run,
        },
      ]}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          run();
        }}
      >
        <p>
          Runs the saved file. The values are remembered for this file; <b>Run</b> uses them until you change them
          here.
        </p>
        <label className="mb-1 flex items-baseline text-xs font-medium text-foreground" htmlFor="run-env">
          Environment
          <span className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">environment</span>
        </label>
        {envs.length > 0 ? (
          <select id="run-env" value={environment} onChange={(e) => setEnvironment(e.target.value)} className={inputClass}>
            {def === "" && <option value="">No environment</option>}
            {envs.map((e) => (
              <option key={e} value={e === def ? "" : e}>
                {e === def ? `${e} (default)` : e}
              </option>
            ))}
          </select>
        ) : (
          <p className="mt-0 text-xs">The project has no environments.</p>
        )}
        {envs.length > 0 && (
          <div className="mt-1 text-2xs">Variables of environments/{environment || def || "…"}.yaml</div>
        )}
        {props.parameters.length > 0 && <h3 className="mt-4 mb-2 text-xs font-semibold text-foreground">Parameters</h3>}
        {props.parameters.map((p) => {
          const id = `run-param-${p.name}`;
          const isMissing = missing.includes(p);
          return (
            <div key={p.name} className="mb-3">
              <label htmlFor={id} className="mb-1 flex items-baseline gap-1 text-xs font-medium text-foreground">
                <span className="font-mono">{p.name}</span>
                {p.required && (
                  <span className="text-destructive" aria-hidden="true">
                    *
                  </span>
                )}
                <span className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">{p.type ?? "string"}</span>
              </label>
              {p.enum && p.enum.length > 0 ? (
                <select
                  id={id}
                  value={params[p.name] ?? ""}
                  onChange={(e) => setParams({ ...params, [p.name]: e.target.value })}
                  className={inputClass}
                >
                  <option value="">{p.default ? `${p.default} (default)` : "Choose…"}</option>
                  {p.enum.map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  id={id}
                  value={params[p.name] ?? ""}
                  placeholder={p.default ? `${p.default} (default)` : undefined}
                  aria-invalid={isMissing}
                  onChange={(e) => setParams({ ...params, [p.name]: e.target.value })}
                  className={inputClass + " font-mono text-xs"}
                />
              )}
              <div className={"mt-1 text-2xs" + (isMissing ? " text-destructive" : "")}>
                {isMissing ? "✖ Required" : p.description ?? (p.default && params[p.name] ? `Default: ${p.default}` : "")}
              </div>
            </div>
          );
        })}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
