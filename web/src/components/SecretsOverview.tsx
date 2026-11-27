import { Fragment, useEffect, useRef, useState } from "react";
import { openFile, takeSecretsRequest, type SecretInfo, type SecretsInfo } from "../reference";
import { ChevronIcon, ChevronUpIcon, CopyIcon } from "./Icons";
import { advice, ago, serviceHint, signClass, signOf, smallButton, stateOf, useSecretActions } from "./SecretField";

/**
 * All secrets of the project in the form of project.yaml (mockup S39f, state "overview"):
 * one row per secret, one column per environment, a click shows the uses and per
 * environment what to do; values in the local store that no file names are "not used".
 * Values are never shown.
 */

type Filter = "all" | "missing" | "plain" | "unused";

export function SecretsOverview(props: { info: SecretsInfo | null }) {
  const { info } = props;
  const [filter, setFilter] = useState<Filter>("all");
  const [open, setOpen] = useState<string | null>(null);
  const actions = useSecretActions(info);
  const top = useRef<HTMLDivElement>(null);
  // "All secrets of the project…" at a field opens project.yaml here.
  useEffect(() => {
    const show = () => takeSecretsRequest() && top.current?.scrollIntoView({ block: "start" });
    show();
    window.addEventListener("runnel:secrets-overview", show);
    return () => window.removeEventListener("runnel:secrets-overview", show);
  }, []);
  if (!info) return <div ref={top} />;

  const envs = info.environments.length > 0 ? info.environments : [""];
  const states = (s: SecretInfo) => envs.map((e) => s.environments[e]).filter((st) => st !== undefined).map(stateOf);
  const missing = info.secrets.filter((s) => states(s).includes("missing"));
  const plain = info.secrets.filter((s) => states(s).includes("plain"));
  const unused = info.local?.unused ?? [];
  const shown = filter === "missing" ? missing : filter === "plain" ? plain : filter === "unused" ? [] : info.secrets;
  // The store of each environment, from its secrets (the heads of the columns).
  const storeOf = (e: string) => info.secrets.find((s) => !s.store && s.environments[e])?.environments[e].store;
  const localEnvs = envs.filter((e) => storeOf(e) === "local");
  const example = missing[0]?.name ?? info.secrets[0]?.name ?? "NAME";
  const readOnly = info.local?.readOnly;
  const cols = envs.length + 2;

  const chip = (f: Filter, label: string, n: number, sign?: "missing" | "plain") => (
    <button
      type="button"
      aria-pressed={filter === f}
      onClick={() => setFilter(f)}
      className={
        "inline-flex h-6 cursor-pointer items-center gap-[5px] rounded-full border bg-background px-[9px] text-2xs " +
        (filter === f ? "border-primary text-foreground shadow-[0_0_0_1px_var(--primary)]" : "border-border text-muted-foreground")
      }
    >
      {sign && <span className={"font-bold " + signClass[sign]}>⚠</span>}
      {label} {n}
    </button>
  );

  return (
    <div ref={top} className="mb-3 scroll-mt-3 px-4" data-testid="secrets-overview">
      <div className="mb-2.5 flex flex-wrap gap-1.5" role="group" aria-label="Filter secrets">
        {chip("all", "All", info.secrets.length)}
        {chip("missing", "Missing", missing.length, "missing")}
        {chip("plain", "Plain text", plain.length, "plain")}
        {chip("unused", "Not used", unused.length)}
      </div>
      <table className="w-full border-collapse text-xs" aria-label="Secrets of the project">
        <thead>
          <tr>
            <th className={th}>Secret</th>
            {envs.map((e) => (
              <th key={e} className={th}>
                {e || "project"}
                <small className="block font-mono text-[10px] font-normal tracking-normal normal-case">
                  {storeOf(e) === "local" ? "local · this computer" : (storeOf(e) ?? "")}
                </small>
              </th>
            ))}
            <th className={th} />
          </tr>
        </thead>
        <tbody>
          {shown.map((s) => {
            const key = `${s.store ?? ""}/${s.name}`;
            const isOpen = open === key;
            const n = s.uses.length;
            return (
              <Fragment key={key}>
                <tr data-secret={s.name} onClick={() => setOpen(isOpen ? null : key)} className={"cursor-pointer " + (isOpen ? "bg-card" : "")}>
                  <td className={td + " font-mono font-medium" + (isOpen ? " border-b-0" : "")}>
                    {s.store ? `${s.store}/` : ""}
                    {s.name}
                    <small className="mt-px block font-sans text-[10px] font-normal text-muted-foreground">{n === 1 ? "1 use" : `${n} uses`}</small>
                  </td>
                  {envs.map((e) => {
                    const st = s.environments[e];
                    if (!st) return <td key={e} className={td} />;
                    const x = stateOf(st);
                    return (
                      <td key={e} className={td + " text-2xs whitespace-nowrap" + (isOpen ? " border-b-0" : "")}>
                        <span className={"font-bold " + signClass[x]}>{signOf(x)}</span>{" "}
                        {x === "set" ? "set" : x === "missing" ? "missing" : x === "plain" ? "plain text" : "cannot be read"}{" "}
                        {x === "set" && <span className="text-muted-foreground">{st.store === "env" ? "variable" : ago(st.updated)}</span>}
                      </td>
                    );
                  })}
                  <td className={td + " w-[30px] text-right text-muted-foreground" + (isOpen ? " border-b-0" : "")}>
                    <button type="button" aria-expanded={isOpen} aria-label={`Details of ${s.name}`} className="cursor-pointer border-0 bg-transparent p-0 text-muted-foreground">
                      {isOpen ? <ChevronUpIcon width={13} height={13} /> : <ChevronIcon open width={13} height={13} />}
                    </button>
                  </td>
                </tr>
                {isOpen && (
                  <tr className="bg-card">
                    <td colSpan={cols} className="border-b border-border px-2 pb-2.5">
                      <ul className="m-0 list-none p-0 text-2xs">
                        <li className="mr-2.5 inline text-muted-foreground">Used in</li>
                        {s.uses.map((u) => (
                          <li key={`${u.file}:${u.line}:${u.column}`} className="mr-2.5 inline">
                            <button type="button" onClick={() => openFile(u.file)} className="cursor-pointer border-0 bg-transparent p-0 font-mono text-[10px] text-primary underline underline-offset-2">
                              {u.file}:{u.line}
                            </button>
                          </li>
                        ))}
                      </ul>
                      <div className="mt-2 grid gap-2" style={{ gridTemplateColumns: `repeat(${Math.min(envs.length, 3)}, minmax(0, 1fr))` }}>
                        {envs.map((e) => {
                          const st = s.environments[e];
                          if (!st) return null;
                          const a = advice(s, e, st, info, actions, true);
                          const x = stateOf(st);
                          return (
                            <div key={e} data-env={e || "project"} className="rounded-md border border-border bg-background px-[9px] py-[7px] text-2xs">
                              <div className="mb-[3px] flex items-center gap-1.5 font-semibold">
                                <span className={"font-bold " + signClass[x]}>{signOf(x)}</span>
                                {e || "project"}
                                <span className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">{st.store}</span>
                              </div>
                              <p className="m-0 mb-1.5 leading-[1.45] text-muted-foreground">{a.text}</p>
                              {a.buttons && <div className="flex flex-wrap gap-1.5">{a.buttons}</div>}
                            </div>
                          );
                        })}
                      </div>
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
          {(filter === "all" || filter === "unused") &&
            unused.map((v) => (
              <tr key={`unused/${v.name}`} data-secret={v.name} className="text-muted-foreground">
                <td className={td + " font-mono font-medium"}>
                  {v.name}
                  <small className="mt-px block font-sans text-[10px] font-normal">not used</small>
                </td>
                {envs.map((e) => (
                  <td key={e} className={td + " text-2xs whitespace-nowrap"}>
                    {localEnvs.includes(e) || (localEnvs.length === 0 && e === envs[envs.length - 1]) ? (
                      <>
                        <span className="font-bold text-success">●</span> set{" "}
                        <span title={`changed ${ago(v.updated)}`}>
                          <button
                            type="button"
                            disabled={!!readOnly}
                            onClick={() => actions.remove(v.name, [])}
                            className={smallButton + " ml-1.5 text-destructive"}
                          >
                            Delete value
                          </button>
                        </span>
                      </>
                    ) : (
                      "–"
                    )}
                  </td>
                ))}
                <td className={td} />
              </tr>
            ))}
          {shown.length === 0 && (filter !== "all" && filter !== "unused" ? true : unused.length === 0) && (
            <tr>
              <td colSpan={cols} className={td + " text-muted-foreground"}>
                {filter === "all" ? "The project uses no secrets yet." : "No secret matches the filter."}
              </td>
            </tr>
          )}
        </tbody>
      </table>
      <p className="mt-2 mb-0 text-[10px] leading-[1.6] text-muted-foreground">
        <span className="font-bold text-success">●</span> set · <span className="font-bold text-warning">⚠</span> used, but missing – runs with this environment do not start ·{" "}
        <span className="font-bold text-destructive">⚠</span> plain text in the project (inline) · – not used there. Values are never shown; “not used” lists values in the local
        store that no file names any more.
      </p>
      <div className="mt-2.5 flex items-start gap-2 rounded-md bg-muted px-2.5 py-2 text-2xs leading-normal text-muted-foreground">
        <span className="grid size-4 shrink-0 place-items-center rounded-full border-[1.5px] border-current text-[10px] font-bold">i</span>
        <div>
          {info.local?.store === "service" ? (
            <b className="font-semibold text-foreground">{serviceHint(info)}</b>
          ) : (
            <>
              <b className="font-semibold text-foreground">The Windows service has its own store.</b> Runs of the service (<span className="font-mono">runnel service</span>) read{" "}
              <span className="font-mono">%ProgramData%\runnel\secrets</span>, not this one. The editor runs as you and can neither read nor set it; set the values in a console opened
              as administrator, and check them there:
            </>
          )}
          <div className="mt-[5px] flex flex-wrap items-center gap-1.5">
            <Command text={`runnel secret set ${example} --service`} />
            <Command text="runnel secret missing --service" />
          </div>
        </div>
      </div>
      {actions.element}
    </div>
  );
}

/** The badges in the head of the section (mockup S39f): missing values, environments with plain text. */
export function SecretsBadges(props: { info: SecretsInfo | null }) {
  const info = props.info;
  if (!info) return null;
  const envs = info.environments.length > 0 ? info.environments : [""];
  const missing = info.secrets.reduce((n, s) => n + envs.filter((e) => s.environments[e]?.state === "missing").length, 0);
  const plain = envs.filter((e) => info.secrets.some((s) => s.environments[e] && stateOf(s.environments[e]) === "plain"));
  return (
    <span className="ml-auto inline-flex gap-1.5 font-normal">
      {missing > 0 && <span className="inline-flex h-5 items-center rounded-full bg-warning-soft px-2 text-2xs font-semibold text-warning">⚠ {missing} missing</span>}
      {plain.length > 0 && <span className="inline-flex h-5 items-center rounded-full bg-danger-soft px-2 text-2xs font-semibold text-destructive">⚠ {plain.map((e) => e || "project").join(", ")}: plain text</span>}
    </span>
  );
}

/** The neutral note when the editor may not change values of this server (local.readOnly). */
export function SecretsReadOnly(props: { info: SecretsInfo | null }) {
  const readOnly = props.info?.local?.readOnly;
  if (!readOnly) return null;
  return (
    <div role="note" data-testid="secrets-read-only" className="mx-4 mb-3 flex items-start gap-2.5 rounded-md border border-border bg-muted px-3 py-2.5 text-xs leading-normal">
      <span className="mt-px grid size-4 shrink-0 place-items-center rounded-full border-[1.5px] border-muted-foreground text-[10px] font-bold text-muted-foreground">i</span>
      <div>
        <div className="font-semibold">Setting secrets is off for this server</div>
        <div className="mt-0.5 text-2xs text-muted-foreground">
          {readOnly === "service" ? "Runnel runs as the Windows service, whose store only an administrator sets." : `Reason: ${readOnly}.`} The editor only shows names and
          states. Set values in a console: <code className="font-mono text-[10px]">runnel secret set NAME{readOnly === "service" ? " --service" : ""}</code>
        </div>
      </div>
    </div>
  );
}

const th = "border-b border-border px-2 pt-1 pb-[5px] text-left align-bottom text-[10px] font-semibold tracking-[.05em] whitespace-nowrap text-muted-foreground uppercase";
const td = "border-b border-border px-2 py-[7px] align-top";

function Command(props: { text: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <code className="rounded-sm border border-border bg-background px-1.5 py-px font-mono text-[10px] text-foreground">{props.text}</code>
      <button type="button" aria-label={`Copy ${props.text}`} onClick={() => void navigator.clipboard?.writeText(props.text)} className={smallButton}>
        <CopyIcon width={12} height={12} />
        Copy
      </button>
    </span>
  );
}
