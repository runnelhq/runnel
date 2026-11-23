import { useEffect, useRef, useState } from "react";
import { useSecrets, type SecretInfo } from "../reference";
import { LockIcon, SearchIcon } from "./Icons";

/**
 * A field of format runnel-secret (mockup S39f, design/mockups/s39-connections.html):
 * a reference ${secret:…} shows as such with its state per environment; "Change…"
 * chooses a name of the project or a new one. Plain text is shown with a warning; other
 * values such as ${env:…} stay a text field. Values of secrets are never shown – the
 * editor does not know them.
 */

const refPattern = /^\$\{secret:(?:([A-Za-z][\w-]*)\/)?([A-Za-z0-9_.-]+)\}$/;
const namePattern = /^[A-Za-z0-9_.-]+$/;

export function SecretInput(props: { id: string; invalid: boolean; value: string; onChange: (value: string | null) => void }) {
  const { value, onChange } = props;
  const secrets = useSecrets(value);
  const [picking, setPicking] = useState(false);
  const [asText, setAsText] = useState(false);
  const ref = refPattern.exec(value.trim());

  if ((!ref && value !== "") || asText) {
    const plain = value !== "" && !value.includes("${");
    return (
      <>
        <input
          id={props.id}
          defaultValue={value}
          key={value}
          aria-invalid={props.invalid}
          onBlur={(e) => {
            setAsText(false);
            if (e.target.value !== value) onChange(e.target.value === "" ? null : e.target.value);
          }}
          onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
          className={
            "block min-h-[30px] w-full rounded-md border bg-background px-2 font-mono text-xs outline-none focus:outline-2 " +
            (plain ? "border-warning focus:outline-warning/25" : "border-input focus:border-ring focus:outline-ring/25")
          }
        />
        {plain && (
          <div role="note" data-testid="plaintext" className="mt-1 flex gap-1.5 rounded-sm bg-warning-soft px-2 py-1 text-2xs text-warning">
            <span aria-hidden="true">⚠</span>
            <span>
              <b className="font-semibold text-foreground">Plain text in a secret field.</b> The value ends up in the file and in Git, and every
              environment uses the same value; use <span className="font-mono">{"${secret:NAME}"}</span>.
            </span>
          </div>
        )}
      </>
    );
  }

  const info = ref ? secrets?.secrets.find((s) => s.name === ref[2] && (s.store ?? "") === (ref[1] ?? "")) : undefined;
  return (
    <div className="relative">
      <div
        id={props.id}
        data-testid="secret"
        className={
          "flex min-h-[30px] items-center gap-2 rounded-md border py-[3px] pr-1 pl-2 text-xs " +
          (picking ? "border-ring bg-background outline-2 outline-ring/25" : props.invalid ? "border-destructive bg-muted" : "border-input bg-muted")
        }
      >
        <span className="inline-flex text-muted-foreground" aria-hidden="true">
          <LockIcon width={14} height={14} />
        </span>
        {ref ? (
          <span className="min-w-0 truncate font-mono text-expression">
            <span className="text-muted-foreground">{"${secret:"}</span>
            {ref[1] ? `${ref[1]}/` : ""}
            {ref[2]}
            <span className="text-muted-foreground">{"}"}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">Not set</span>
        )}
        <span className="ml-auto inline-flex flex-wrap justify-end gap-1">{info && !picking && <States info={info} />}</span>
        <button
          type="button"
          onClick={() => setPicking((p) => !p)}
          className="inline-flex h-6 shrink-0 cursor-pointer items-center rounded-md border border-input bg-background px-2 text-2xs font-medium hover:bg-muted"
        >
          {picking ? "Cancel" : ref ? "Change…" : "Choose…"}
        </button>
      </div>
      {picking && (
        <Picker
          secrets={secrets?.secrets ?? []}
          current={ref?.[2] ?? ""}
          onPick={(name) => {
            setPicking(false);
            onChange(`\${secret:${name}}`);
          }}
          onText={() => {
            setPicking(false);
            setAsText(true);
          }}
          onClose={() => setPicking(false)}
        />
      )}
    </div>
  );
}

/** The state per environment: ● set (with its store), ⚠ missing, – store not available yet. */
function States(props: { info: SecretInfo }) {
  return (
    <>
      {Object.entries(props.info.environments).map(([env, s]) => {
        const label = env || "project";
        const [tone, sign, text] =
          s.state === "set"
            ? ["bg-success-soft text-success", "●", s.store]
            : s.state === "missing"
              ? ["bg-warning-soft text-warning", "⚠", "missing"]
              : ["bg-muted text-muted-foreground border border-border", "–", `${s.store} not available yet`];
        return (
          <span
            key={env}
            data-testid={`secret-state-${label}`}
            title={`${label}: ${s.state === "set" ? `set in ${s.store}` : s.state === "missing" ? `missing in ${s.store}` : `the store ${s.store} is not available yet`}`}
            className={"inline-flex h-5 items-center gap-1 rounded-full px-1.5 text-[10px] font-semibold whitespace-nowrap " + tone}
          >
            {sign} {label} <span className="font-normal opacity-85">{text}</span>
          </span>
        );
      })}
    </>
  );
}

function Picker(props: { secrets: SecretInfo[]; current: string; onPick: (name: string) => void; onText: () => void; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  const q = query.trim();
  const names = [...new Set(props.secrets.filter((s) => !s.store).map((s) => s.name))].filter((n) => n.toLowerCase().includes(q.toLowerCase()));
  const canCreate = q !== "" && namePattern.test(q) && !names.includes(q);
  return (
    <div
      role="dialog"
      aria-label="Choose a secret"
      onKeyDown={(e) => e.key === "Escape" && props.onClose()}
      className="absolute inset-x-0 top-[calc(100%+4px)] z-20 rounded-md border border-border bg-popover p-1 text-xs text-popover-foreground shadow-lg"
    >
      <label className="m-0.5 mb-1 flex h-7 items-center gap-1.5 rounded-md border border-ring bg-background px-2 text-muted-foreground">
        <SearchIcon width={13} height={13} />
        <input
          ref={input}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter") return;
            if (names.length > 0) props.onPick(names[0]);
            else if (canCreate) props.onPick(q);
          }}
          aria-label="Name of the secret"
          placeholder="Name of the secret"
          className="min-w-0 flex-1 border-0 bg-transparent font-mono text-xs text-foreground outline-none"
        />
      </label>
      <div className="px-2 pt-1.5 pb-0.5 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">Secrets of the project</div>
      {names.length === 0 && <div className="px-2 py-1 text-muted-foreground">{q ? "No secret with this name yet" : "The project uses no secrets yet"}</div>}
      {names.map((n) => {
        const info = props.secrets.find((s) => s.name === n && !s.store)!;
        return (
          <button
            key={n}
            type="button"
            role="option"
            aria-selected={n === props.current}
            onClick={() => props.onPick(n)}
            className="flex w-full cursor-pointer items-center gap-2 rounded-sm border-0 bg-transparent px-2 py-1.5 text-left hover:bg-accent aria-selected:bg-accent"
          >
            <span className="font-mono">{n}</span>
            <span className="ml-auto inline-flex gap-1">
              <States info={info} />
            </span>
          </button>
        );
      })}
      <hr className="my-1 border-0 border-t border-border" />
      {canCreate && (
        <button
          type="button"
          onClick={() => props.onPick(q)}
          className="flex w-full cursor-pointer items-center gap-2 rounded-sm border-0 bg-transparent px-2 py-1.5 text-left font-medium text-primary hover:bg-accent"
        >
          + Use new secret <span className="font-mono">“{q}”</span>
          <span className="ml-auto font-normal text-muted-foreground">set its value in the store of each environment</span>
        </button>
      )}
      <button
        type="button"
        onClick={props.onText}
        className="flex w-full cursor-pointer items-center gap-2 rounded-sm border-0 bg-transparent px-2 py-1.5 text-left text-muted-foreground hover:bg-accent"
      >
        Edit as text, e.g. <span className="font-mono">{"${secret:env/NAME}"}</span>
      </button>
    </div>
  );
}
