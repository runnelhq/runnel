import { useEffect, useRef, useState, type ReactNode } from "react";
import { fetchVersion } from "../api";
import { changeSecret, openFile, openSecretsOverview, reloadSecrets, useSecrets, type SecretInfo, type SecretState, type SecretsInfo } from "../reference";
import { Dialog } from "./Dialog";
import { ChevronIcon, ChevronUpIcon, KeyIcon, LockIcon, ReloadIcon, SearchIcon, ServiceStoreIcon, TrashIcon } from "./Icons";

/**
 * A field of format runnel-secret (mockups S39b and S39f): a reference ${secret:…} shows
 * as such with its state per environment; "Change…" chooses a name of the project or a new
 * one. Below it, per environment, what to do there (S39g2): set, replace or delete the
 * value of the local store, how env is set, the warning for inline. Plain text is shown
 * with a warning; other values such as ${env:…} stay a text field. Values of secrets are
 * never shown – the editor does not know them.
 */

const refPattern = /^\$\{secret:(?:([A-Za-z][\w-]*)\/)?([A-Za-z0-9_.-]+)\}$/;
const namePattern = /^[A-Za-z0-9_.-]+$/;

export function SecretInput(props: { id: string; invalid: boolean; value: string; onChange: (value: string | null) => void }) {
  const { value, onChange } = props;
  const secrets = useSecrets(value);
  const [picking, setPicking] = useState(false);
  const [asText, setAsText] = useState(false);
  const [open, setOpen] = useState<boolean | null>(null);
  const actions = useSecretActions(secrets);
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
  // The list below is open while something needs doing, also only for inline (decision for S39f).
  const expanded = !!info && !picking && (open ?? Object.values(info.environments).some((s) => s.state !== "set" || s.store === "inline"));
  return (
    <div className="relative">
      <div
        id={props.id}
        data-testid="secret"
        className={
          "flex min-h-[30px] items-center gap-2 border py-[3px] pr-1 pl-2 text-xs " +
          (expanded ? "rounded-t-md " : "rounded-md ") +
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
        {info && !picking && (
          <button type="button" aria-expanded={expanded} aria-label="States per environment" title="States per environment" onClick={() => setOpen(!expanded)} className={smallButton}>
            {expanded ? <ChevronUpIcon width={12} height={12} /> : <ChevronIcon open width={12} height={12} />}
          </button>
        )}
        <button type="button" onClick={() => setPicking((p) => !p)} className={smallButton}>
          {picking ? "Cancel" : ref ? "Change…" : "Choose…"}
        </button>
      </div>
      {expanded && info && secrets && (
        <div data-testid="secret-environments" className="rounded-b-md border border-t-0 border-input bg-card">
          {Object.entries(info.environments).map(([env, st]) => {
            const a = advice(info, env, st, secrets, actions, false);
            return (
              <div key={env} data-env={env || "project"} className={"grid grid-cols-[76px_56px_1fr_auto] items-start gap-2.5 border-t border-border px-2.5 py-2 text-2xs leading-normal first:border-t-0 " + (st.store === "local" ? "bg-accent/55" : "")}>
                <span>
                  <Pill env={env} st={st} />
                </span>
                <span className="pt-0.5 font-mono text-muted-foreground">{st.store}</span>
                <span className="pt-0.5">{a.text}</span>
                <span className="inline-flex gap-1.5">{a.buttons}</span>
              </div>
            );
          })}
          <div className="flex items-center gap-2 border-t border-border px-2.5 py-1.5 text-2xs text-muted-foreground">
            <ServiceStoreIcon width={13} height={13} className="shrink-0" />
            <span>{serviceHint(secrets)}</span>
            <button type="button" onClick={openSecretsOverview} className="ml-auto shrink-0 cursor-pointer border-0 bg-transparent p-0 text-2xs text-primary underline underline-offset-2">
              All secrets of the project…
            </button>
          </div>
        </div>
      )}
      {actions.element}
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

export const smallButton =
  "inline-flex h-6 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2 text-2xs font-medium whitespace-nowrap hover:bg-muted disabled:cursor-default disabled:opacity-50 disabled:hover:bg-background";
const primaryButton = smallButton + " border-primary! bg-primary! text-primary-foreground hover:opacity-90";
const dangerButton = smallButton + " text-destructive";

/** The hint on the store of the Windows service (mockup S39f), at the field and in the overview. */
export const serviceHint = (info: SecretsInfo) =>
  info.local?.store === "service"
    ? "This server runs as the Windows service: an administrator sets its values in a console."
    : "Runs of the Windows service read their own store; it is set in the console as administrator.";

/** The state of a secret in an environment: ● set, ⚠ missing, ⚠ red plain text (inline), – cannot be read. */
export function stateOf(st: SecretState): "set" | "missing" | "plain" | "unavailable" {
  if (st.state === "set" && st.store === "inline") return "plain";
  return st.state;
}

export const signClass = { set: "text-success", missing: "text-warning", plain: "text-destructive", unavailable: "text-muted-foreground" };
export const signOf = (s: ReturnType<typeof stateOf>) => (s === "set" ? "●" : s === "unavailable" ? "–" : "⚠");

function Pill(props: { env: string; st: SecretState; src?: boolean }) {
  const s = stateOf(props.st);
  const label = props.env || "project";
  const tone = {
    set: "bg-success-soft text-success",
    missing: "bg-warning-soft text-warning",
    plain: "bg-danger-soft text-destructive",
    unavailable: "bg-muted text-muted-foreground border border-border",
  }[s];
  const src = s === "set" || s === "plain" ? props.st.store : s === "missing" ? "missing" : "cannot be read";
  const title = s === "set" ? `set in ${props.st.store}` : s === "plain" ? "plain text in the project (inline)" : s === "missing" ? `missing in ${props.st.store}` : `the store ${props.st.store} cannot be read`;
  return (
    <span
      data-testid={props.src ? `secret-state-${label}` : undefined}
      title={`${label}: ${title}`}
      className={"inline-flex h-5 items-center gap-1 rounded-full px-1.5 text-[10px] font-semibold whitespace-nowrap " + tone}
    >
      {signOf(s)} {label}
      {props.src && <> <span className="font-normal opacity-85">{src}</span></>}
    </span>
  );
}

/** The state per environment: ● set (with its store), ⚠ missing, ⚠ plain text, – store cannot be read. */
function States(props: { info: SecretInfo }) {
  return (
    <>
      {Object.entries(props.info.environments).map(([env, st]) => (
        <Pill key={env} env={env} st={st} src />
      ))}
    </>
  );
}

/** When a value changed, e.g. "2 days ago". */
export function ago(iso: string | undefined): string {
  const t = iso ? Date.parse(iso) : NaN;
  if (Number.isNaN(t)) return "";
  const s = Math.round((t - Date.now()) / 1000);
  const fmt = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  for (const [unit, n] of [["year", 31536000], ["month", 2592000], ["day", 86400], ["hour", 3600], ["minute", 60]] as const) {
    if (Math.abs(s) >= n) return fmt.format(Math.trunc(s / n), unit);
  }
  return "just now";
}

let serverOS: Promise<string | undefined> | undefined;

/** The folder of the local store, for when it cannot be read (no path from the server, decision 2026-11-27). */
function useStoreFolder(service: boolean): string {
  const [os, setOS] = useState<string | undefined | null>(null);
  useEffect(() => {
    serverOS ??= fetchVersion().then((v) => v.os, () => undefined);
    let live = true;
    void serverOS.then((o) => live && setOS(o));
    return () => {
      live = false;
    };
  }, []);
  if (service) return "%ProgramData%\\runnel\\secrets";
  if (os === "windows") return "%LOCALAPPDATA%\\runnel\\secrets";
  if (os === "darwin") return "~/Library/Application Support/runnel/secrets";
  if (os) return "~/.config/runnel/secrets";
  return "%LOCALAPPDATA%\\runnel\\secrets or the user config folder";
}

function StoreFolder(props: { service: boolean }) {
  return <span className="font-mono">{useStoreFolder(props.service)}</span>;
}

const envFile = (env: string) => (env ? `environments/${env}.yaml` : "project.yaml");
const fileName = (env: string) => (env ? `${env}.yaml` : "project.yaml");

/**
 * What an environment needs (mockup S39f): the text and the buttons, in a row below the
 * field (card false) or in a card of the overview (card true).
 */
export function advice(secret: SecretInfo, env: string, st: SecretState, info: SecretsInfo, actions: SecretActions, card: boolean): { text: ReactNode; buttons: ReactNode } {
  const s = stateOf(st);
  const label = env || "the project";
  const readOnly = !!info.local?.readOnly;
  const muted = (t: ReactNode) => <span className="text-muted-foreground">{t}</span>;
  const open = (
    <button type="button" className={smallButton} onClick={() => openFile(envFile(env))}>
      Open {fileName(env)}
    </button>
  );
  const fileLink = (
    <button type="button" onClick={() => openFile(envFile(env))} className="cursor-pointer border-0 bg-transparent p-0 font-mono text-2xs text-primary underline underline-offset-2">
      {envFile(env)}
    </button>
  );
  switch (st.store) {
    case "inline":
      return {
        text:
          s === "plain" ? (
            card ? (
              <>Plain text in <span className="font-mono">{envFile(env)}</span>. Change it in that file.</>
            ) : (
              <><b className="font-semibold">Plain text</b> under <span className="font-mono">secrets:</span> in {fileLink} – the value is in the file and in Git. Use it only for development.</>
            )
          ) : (
            <><b className="font-semibold">Not set</b> under <span className="font-mono">secrets:</span> in {fileLink}{env && " or project.yaml"}. Runs with {label} do not start.</>
          ),
        buttons: open,
      };
    case "env": {
      const name = <span className="font-mono">{secret.name}</span>;
      if (card)
        return {
          text:
            s === "set" ? <>Read from the environment variable {name} of the Runnel process.</> : <>Environment variable {name} is not set for the Runnel process. Runnel cannot set it; set it in Windows and restart Runnel.</>,
          buttons: null,
        };
      return {
        text: (
          <>
            {s === "set" ? "Read from" : <><b className="font-semibold">Not set.</b> Read from</>} the environment variable {name} of the Runnel process.{" "}
            {muted("Runnel cannot set it: set it in Windows for the account that runs Runnel (System properties › Environment variables), then restart Runnel.")}
          </>
        ),
        buttons: null,
      };
    }
    case "local": {
      const where = { env, secret };
      const disabled = readOnly ? { disabled: true, title: "Setting secrets is off for this server" } : {};
      if (s === "unavailable")
        return {
          text: (
            <>
              <b className="font-semibold">The local store cannot be read{info.local?.error ? ":" : "."}</b> {info.local?.error?.replace(/; details are in the server log$/, "")}
              {info.local?.error && "."} Runs with {label} do not start.{" "}
              {muted(<>Details are in the server log; check the permissions of <StoreFolder service={info.local?.store === "service"} />.</>)}
            </>
          ),
          buttons: (
            <button type="button" className={smallButton} onClick={reloadSecrets}>
              <ReloadIcon width={12} height={12} />
              Check again
            </button>
          ),
        };
      if (s === "missing")
        return {
          text: card ? <>Not set on this computer. Runs with {label} do not start.</> : <><b className="font-semibold">Not set</b> in the local store of this project on this computer. Runs with {label} do not start.</>,
          buttons: (
            <button type="button" className={primaryButton} onClick={() => actions.set(where, false)} {...disabled}>
              <KeyIcon width={12} height={12} />
              Set value…
            </button>
          ),
        };
      return {
        text: card ? (
          <>Set on this computer · changed {ago(st.updated)}.</>
        ) : (
          <><b className="font-semibold">Set</b> in the local store on this computer · changed {ago(st.updated)}. {muted("The value is never shown.")}</>
        ),
        buttons: (
          <>
            <button type="button" className={smallButton} onClick={() => actions.set(where, true)} {...disabled}>
              <KeyIcon width={12} height={12} />
              Replace value…
            </button>
            <button type="button" className={dangerButton} onClick={() => actions.remove(secret.name, localEnvs(secret))} {...disabled}>
              <TrashIcon width={12} height={12} />
              Delete value
            </button>
          </>
        ),
      };
    }
  }
  return { text: <>The secret store <span className="font-mono">{st.store}</span> cannot be read here.</>, buttons: null };
}

/** The environments that read a secret from the local store: one value serves them all. */
const localEnvs = (secret: SecretInfo) => Object.entries(secret.environments).filter(([, st]) => st.store === "local").map(([env]) => env);
const names = (envs: string[]) => (envs.length <= 1 ? (envs[0] || "the project") : `${envs.slice(0, -1).join(", ")} and ${envs[envs.length - 1]}`);

export interface SecretActions {
  set: (where: { env: string; secret: SecretInfo }, replace: boolean) => void;
  remove: (name: string, envs: string[]) => void;
  element: ReactNode;
}

/** useSecretActions holds the dialogs to set and delete values (mockup S39f). */
export function useSecretActions(info: SecretsInfo | null): SecretActions {
  const [setting, setSetting] = useState<{ env: string; secret: SecretInfo; replace: boolean } | null>(null);
  const [deleting, setDeleting] = useState<{ name: string; envs: string[]; error?: string } | null>(null);
  const element = (
    <>
      {setting && info && <SetSecretDialog {...setting} service={info.local?.store === "service"} onClose={() => setSetting(null)} />}
      {deleting && (
        <Dialog
          title={`Delete the value of ${deleting.name}?`}
          iconClass="bg-danger-soft text-destructive"
          onCancel={() => setDeleting(null)}
          actions={[
            { label: "Cancel", onClick: () => setDeleting(null) },
            {
              label: "Delete value",
              tone: "danger",
              onClick: () =>
                void changeSecret(deleting.name, null, {}).then(
                  () => setDeleting(null),
                  (e: unknown) => setDeleting({ ...deleting, error: e instanceof Error ? e.message : String(e) }),
                ),
            },
          ]}
        >
          <p>
            {deleting.envs.length > 0 ? (
              <>
                The value is removed from the local store of this project on this computer: runs with <b>{names(deleting.envs)}</b> do not start until it is set again.
              </>
            ) : (
              <>No file of the project uses it any more. The value is removed from the local store of this project on this computer.</>
            )}
          </p>
          {deleting.error && (
            <p role="alert" className="text-destructive">
              ✖ {deleting.error}
            </p>
          )}
        </Dialog>
      )}
    </>
  );
  return {
    set: (where, replace) => setSetting({ ...where, replace }),
    remove: (name, envs) => setDeleting({ name, envs }),
    element,
  };
}

/**
 * "Set value" (mockup S39f, states dialog and error): environment and store are fixed, the
 * value is only masked – no reveal, also not of what is typed (decision 2026-11-27). Pasting
 * is allowed, line breaks are kept. An error stays in the dialog with the value in the field.
 */
function SetSecretDialog(props: { env: string; secret: SecretInfo; replace: boolean; service: boolean; onClose: () => void }) {
  const { secret, env } = props;
  const [value, setValue] = useState("");
  const [pasted, setPasted] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLDivElement>(null);
  const envs = localEnvs(secret);
  const files = [...new Set(secret.uses.map((u) => u.file))];
  const length = [...value].length;
  const close = useRef(props.onClose);
  close.current = props.onClose;
  useEffect(() => {
    input.current?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const save = () => {
    if (value === "" || busy) return;
    setBusy(true);
    changeSecret(secret.name, value, secret.store ? { store: secret.store } : { environment: env }).then(props.onClose, (e: unknown) => {
      setBusy(false);
      setError(e instanceof Error ? e.message : String(e));
    });
  };
  const edit = (next: string, paste = false) => {
    setValue(next);
    setPasted(paste);
  };
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-tiefe-950/40" onMouseDown={(e) => e.target === e.currentTarget && props.onClose()}>
      <div role="dialog" aria-modal="true" aria-label="Set value" className="w-[540px] max-w-[calc(100vw-32px)] rounded-lg border border-border bg-card px-5 pt-[18px] pb-4 text-xs text-card-foreground shadow-lg">
        <h2 className="m-0 mb-1 text-md font-semibold">
          {props.replace ? "Replace value of " : "Set value of "}
          <span className="font-mono">{secret.name}</span>
        </h2>
        <p className="m-0 mb-3.5 text-muted-foreground">
          {files.length > 0 && <>Used by <span className="font-mono">{files.join(", ")}</span>.</>}
          {props.replace && " The current value is overwritten."}
        </p>
        <div className="mb-3 grid grid-cols-[96px_1fr] items-baseline gap-x-2.5 gap-y-1.5">
          <span className="text-muted-foreground">{envs.length > 1 ? "Environments" : "Environment"}</span>
          <b>{names(envs.length > 0 ? envs : [env])}</b>
          <span className="text-muted-foreground">Stored in</span>
          <span>
            <span className="font-mono">local</span> – the store of this project on this computer, for your account; encrypted, outside the project folder
          </span>
        </div>
        <div className="mb-1 text-xs font-medium">Value</div>
        <div
          ref={input}
          role="textbox"
          tabIndex={0}
          aria-label="Value"
          aria-multiline="true"
          data-testid="secret-value"
          onKeyDown={(e) => {
            if (e.ctrlKey || e.metaKey || e.altKey) {
              if (e.key === "Backspace") edit("");
              return; // Ctrl+V pastes (onPaste); Ctrl+C has nothing to copy
            }
            if (e.key === "Enter" && !e.shiftKey) save();
            else if (e.key === "Enter") edit(value + "\n");
            else if (e.key === "Backspace") edit([...value].slice(0, -1).join(""));
            else if (e.key === "Delete") edit("");
            else if (e.key.length === 1 || [...e.key].length === 1) edit(value + e.key);
            else return;
            e.preventDefault();
          }}
          onPaste={(e) => {
            e.preventDefault();
            edit(value + e.clipboardData.getData("text/plain"), true);
          }}
          className="flex h-[30px] cursor-text items-center gap-1.5 overflow-hidden rounded-md border border-input bg-background px-2 font-mono text-xs outline-none focus:border-ring focus:outline-[3px] focus:outline-ring/22"
        >
          <span className="tracking-[2px] whitespace-nowrap">{"•".repeat(Math.min(length, 48))}</span>
        </div>
        <div className="mt-[3px] text-2xs text-muted-foreground" aria-live="polite">
          {length === 0 ? "Type or paste the value" : `${pasted ? "Pasted from the clipboard · " : ""}${length} ${length === 1 ? "character" : "characters"}`} · multi-line values such as private keys keep their line breaks
        </div>
        {error ? (
          <div role="alert" className="mt-2.5 flex items-start gap-2 rounded-md bg-danger-soft px-[11px] py-[9px] text-2xs leading-normal">
            <span className="font-bold text-destructive">✖</span>
            <div>
              <ErrorText message={error} />
              <div className="mt-1 text-muted-foreground">The value stays in the field until you close the dialog.</div>
            </div>
          </div>
        ) : (
          <div className="mt-2.5 flex items-start gap-2 rounded-md bg-muted px-2.5 py-2 text-2xs leading-normal text-muted-foreground">
            <span className="grid size-4 shrink-0 place-items-center rounded-full border-[1.5px] border-current text-[10px] font-bold">i</span>
            <span>
              After saving, the value is never shown again – not here, not in the log or in results.{envs.length <= 1 && " Runs of other environments do not get it."}{" "}
              {props.service ? (
                "This server runs as the Windows service and reads its own store."
              ) : (
                <>
                  Runs of the Windows service read their own store; set it there as administrator: <span className="font-mono">runnel secret set {secret.name} --service</span>
                </>
              )}
            </span>
          </div>
        )}
        <div className="mt-4 flex items-center justify-end gap-2">
          <button type="button" onClick={props.onClose} className="inline-flex h-7 cursor-pointer items-center rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted">
            Cancel
          </button>
          <button
            type="button"
            disabled={value === "" || busy}
            onClick={save}
            className="inline-flex h-7 cursor-pointer items-center rounded-md border border-primary bg-primary px-2.5 text-xs font-medium text-primary-foreground disabled:cursor-default disabled:opacity-50"
          >
            {error ? "Try again" : props.replace ? "Replace value" : "Set value"}
          </button>
        </div>
      </div>
    </div>
  );
}

/** The message of the server: "The value was not stored." in bold, names of variables and commands as code. */
function ErrorText(props: { message: string }) {
  const m = /^(the value was not (?:stored|deleted))[.:]\s*(.*)$/is.exec(props.message);
  const rest = m ? m[2].charAt(0).toUpperCase() + m[2].slice(1) : props.message;
  return (
    <>
      {m && <b className="font-semibold">{m[1].charAt(0).toUpperCase() + m[1].slice(1)}.</b>}{" "}
      {rest.split(/("[^"]+"|RUNNEL_MASTER_KEY|runnel serve)/).map((part, i) =>
        i % 2 ? (
          <code key={i} className="rounded-sm bg-background px-[5px] py-px font-mono text-[10px]">
            {part.replace(/"/g, "")}
          </code>
        ) : (
          part
        ),
      )}
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
