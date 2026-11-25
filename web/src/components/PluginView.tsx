import { useState, type ReactNode } from "react";
import { categoryColor } from "../catalog";
import { formatBytes, shortHash, type PluginInfo, type PluginUse } from "../plugins";
import { Dialog } from "./Dialog";
import { ChevronIcon, CopyIcon, PlugIcon, ReloadIcon, ShieldIcon } from "./Icons";

/**
 * The overview of a plugin (mockup S52, design/mockups/s52-plugins.html): the state of its
 * approval for the user and the Windows service, its types once trusted, where the project
 * uses it and its program. Nothing here starts the plugin: the server starts only trusted
 * plugins to learn their types (S52a).
 */
export function PluginView(props: {
  info: PluginInfo | null | undefined;
  error: string | null;
  serviceApprovals: boolean;
  /** False where the editor cannot trust plugins (Runnel runs as the Windows service). */
  canTrust: boolean;
  onTrust: () => void;
  onRetry: () => void;
  onOpenFile: (path: string) => void;
}) {
  const p = props.info;
  if (p === undefined) return <div className="flex-1" aria-busy="true" />;
  if (p === null) {
    return (
      <div className="grid flex-1 place-items-center p-8 text-center text-xs text-muted-foreground" role="alert">
        {props.error ?? "The plugin cannot be read; check its plugin.yaml in the YAML view."}
      </div>
    );
  }
  return (
    <div className="flex-1 overflow-auto bg-background" data-testid="plugin-view">
      <div className="mx-auto max-w-[760px] px-7 pt-4 pb-10">
        <div className="mb-1 flex items-center gap-3">
          <span
            className={"grid size-9 shrink-0 place-items-center rounded-md " + (p.state === "not-trusted" ? "bg-tiefe-300 text-tiefe-800" : "text-white")}
            style={p.state === "not-trusted" ? undefined : { background: categoryColor.general }}
          >
            <PlugIcon width={20} height={20} />
          </span>
          <div className="min-w-0">
            <h2 className="m-0 text-lg font-semibold">{p.name}</h2>
            <div className="text-xs text-muted-foreground">
              Version {p.version} · namespace <span className="font-mono">{p.namespace}</span>
              {p.description && ` · ${p.description}`}
            </div>
          </div>
        </div>
        <p className="mt-0 mb-3 ml-12 text-xs text-muted-foreground">Runs as a program of its own, started by Runnel for each run</p>
        <Status p={p} serviceApprovals={props.serviceApprovals} canTrust={props.canTrust} onTrust={props.onTrust} onRetry={props.onRetry} />
        <Section title="Types" count={p.types ? countTypes(p) : undefined}>
          <Types p={p} onOpenFile={props.onOpenFile} />
        </Section>
        <Section title="Used by" count={files(p.uses).length ? `${files(p.uses).length} ${files(p.uses).length === 1 ? "file" : "files"}` : undefined}>
          {p.uses.length === 0 ? (
            <p className="m-0 mb-2 text-xs text-muted-foreground">No file of the project uses the plugin yet.</p>
          ) : (
            <ul className="m-0 mb-2 list-none p-0 text-xs">
              {files(p.uses).map(([file, uses]) => (
                <li key={file} className="py-[3px]">
                  <FileLink path={file} onOpen={props.onOpenFile} />
                  <span className="ml-1.5 text-muted-foreground">{uses.map((u) => (u.kind === "connection" ? `type ${u.type}` : `${u.id} (${u.type})`)).join(", ")}</span>
                </li>
              ))}
            </ul>
          )}
        </Section>
        <Section title="Program">
          <dl className="m-0 mb-2 grid grid-cols-[110px_1fr] gap-x-3 gap-y-1.5 text-xs">
            <dt className="text-muted-foreground">Folder</dt>
            <dd className="m-0 font-mono text-[11px]">{p.folder}</dd>
            <dt className="text-muted-foreground">Program</dt>
            <dd className="m-0">
              {p.program ? <span className="font-mono text-[11px]">{p.program}</span> : <span className="text-warning">⚠ none for this computer</span>}
              {typeof p.executable === "object" && (
                <span className="text-muted-foreground">
                  {" "}
                  ·{" "}
                  {Object.entries(p.executable)
                    .map(([os, exe]) => `${os}: ${exe}`)
                    .join(" · ")}
                </span>
              )}
            </dd>
            {p.args && p.args.length > 0 && (
              <>
                <dt className="text-muted-foreground">Arguments</dt>
                <dd className="m-0 font-mono text-[11px]">{p.args.join(" ")}</dd>
              </>
            )}
            <dt className="text-muted-foreground">Files</dt>
            <dd className="m-0">
              {p.files} · {formatBytes(p.bytes)}
            </dd>
          </dl>
        </Section>
      </div>
    </div>
  );
}

const countTypes = (p: PluginInfo) => (p.types ? p.types.actions.length + p.types.transforms.length + p.types.connectionTypes.length : 0);

/** The uses grouped by file, in the order of the files. */
function files(uses: PluginUse[]): [string, PluginUse[]][] {
  const by = new Map<string, PluginUse[]>();
  for (const u of uses) by.set(u.file, [...(by.get(u.file) ?? []), u]);
  return [...by.entries()].sort(([a], [b]) => a.localeCompare(b));
}

function Section(props: { title: string; count?: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(true);
  return (
    <section className="border-t border-border pt-2.5 pb-1" aria-label={props.title}>
      <h3 className="m-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">
        <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="-ml-1 inline-flex cursor-pointer items-center gap-1.5 border-0 bg-transparent p-0 text-sm font-semibold">
          <ChevronIcon open={open} width={12} height={12} className="text-muted-foreground" />
          {props.title}
        </button>
        {props.count !== undefined && <span className="text-2xs font-normal text-muted-foreground">{props.count}</span>}
      </h3>
      {open && props.children}
    </section>
  );
}

function FileLink(props: { path: string; onOpen: (path: string) => void }) {
  return (
    <button type="button" onClick={() => props.onOpen(props.path)} className="cursor-pointer border-0 bg-transparent p-0 font-mono text-[11px] text-primary underline underline-offset-2">
      {props.path}
    </button>
  );
}

/** A command for the Windows service, to copy (the editor never writes its approvals). */
export function Command(props: { text: string }) {
  return (
    <span className="inline-flex max-w-full items-center gap-1.5 rounded-sm border border-border bg-muted py-px pr-1 pl-1.5 font-mono text-[10px] text-foreground">
      <span className="truncate">{props.text}</span>
      <button
        type="button"
        title="Copy"
        aria-label="Copy the command"
        onClick={() => void navigator.clipboard?.writeText(props.text)}
        className="grid size-[18px] shrink-0 cursor-pointer place-items-center rounded-sm border-0 bg-transparent p-0 text-muted-foreground hover:text-foreground"
      >
        <CopyIcon width={12} height={12} />
      </button>
    </span>
  );
}

const date = (iso: string) => new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });

/** The state of the approval as a card (states plugin, untrusted, changed, error of the mockup). */
function Status(props: { p: PluginInfo; serviceApprovals: boolean; canTrust: boolean; onTrust: () => void; onRetry: () => void }) {
  const { p } = props;
  const service = `runnel plugins trust --service --plugin ${p.name}`;
  // As the Windows service, an administrator trusts plugins: the command instead of the button.
  const trustButton = (label: string) =>
    props.canTrust ? (
      button(label, props.onTrust, <ShieldIcon width={12} height={12} />)
    ) : (
      <div className="mt-2 text-2xs text-muted-foreground">
        Runnel runs as the Windows service; an administrator trusts the plugin: <Command text={service} />
      </div>
    );
  const card = (tone: "ok" | "warn" | "fail", sym: string, title: string, body: ReactNode) => (
    <div
      role={tone === "ok" ? "status" : "alert"}
      className={"mb-3 flex items-start gap-2.5 rounded-md px-3 py-2.5 text-xs " + (tone === "ok" ? "bg-success-soft" : tone === "warn" ? "bg-warning-soft" : "bg-danger-soft")}
    >
      <span className={"font-bold " + (tone === "ok" ? "text-success" : tone === "warn" ? "text-warning" : "text-destructive")}>{sym}</span>
      <div className="min-w-0 flex-1">
        <div className="font-semibold">{title}</div>
        {body}
      </div>
    </div>
  );
  const muted = (text: ReactNode) => <div className="mt-0.5 text-2xs text-muted-foreground">{text}</div>;
  const button = (label: string, onClick: () => void, icon: ReactNode, primary = true) => (
    <button
      type="button"
      onClick={onClick}
      className={
        "mt-2 inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-md border px-2 text-2xs font-medium " +
        (primary ? "border-primary bg-primary text-primary-foreground" : "border-input bg-background hover:bg-muted")
      }
    >
      {icon}
      {label}
    </button>
  );
  switch (p.state) {
    case "trusted":
      return card(
        "ok",
        "✓",
        "Trusted on this computer",
        <>
          {muted("Runnel starts it for runs, checks and the editor. A change to any file in its folder takes the approval back.")}
          <div className="mt-2 grid grid-cols-[130px_1fr] items-center gap-x-3 gap-y-1.5 text-2xs">
            <span className="text-muted-foreground">Your account</span>
            <span>
              {p.user?.current ? `✓ trusted on ${date(p.user.trusted)} · version ${p.user.version ?? p.version} · SHA-256 ` : "– not trusted · "}
              <span className="font-mono">{shortHash(p.hash)}</span>
            </span>
            {props.serviceApprovals && (
              <>
                <span className="text-muted-foreground">Windows service</span>
                <span>
                  {p.service?.current ? (
                    `✓ trusted on ${date(p.service.trusted)}`
                  ) : (
                    <>
                      ! not trusted – runs of the service do not start it · as administrator: <Command text={service} />
                    </>
                  )}
                </span>
              </>
            )}
          </div>
        </>,
      );
    case "changed": {
      const before = p.user ?? p.service;
      return card(
        "warn",
        "!",
        "Changed since it was trusted",
        <>
          {muted(`A file in ${p.folder} changed, e.g. by an update. Runnel no longer starts the plugin until you trust it again.`)}
          <div className="mt-2 grid grid-cols-[130px_1fr] gap-x-3 gap-y-1.5 text-2xs">
            <span className="text-muted-foreground">Trusted</span>
            <span>
              {before?.version && `version ${before.version} · `}SHA-256 <span className="font-mono">{shortHash(before?.hash)}</span>
              {before && ` · ${date(before.trusted)}`}
            </span>
            <span className="text-muted-foreground">Now</span>
            <span>
              version {p.version} · SHA-256 <span className="font-mono">{shortHash(p.hash)}</span>
            </span>
          </div>
          {trustButton("Review and trust again…")}
        </>,
      );
    }
    case "error":
      return card(
        "fail",
        "✕",
        "The plugin cannot start",
        <>
          {muted(`${p.message ?? ""} Steps that use it show this message and cannot run.`)}
          {button("Try again", props.onRetry, <ReloadIcon width={12} height={12} />, false)}
        </>,
      );
    default:
      return card(
        "warn",
        "!",
        "Not trusted on this computer",
        <>
          {muted(
            "Runnel does not start this plugin – not to list its types, not to check workflows and not in runs – until you trust it. This protects you from code that came with a copied or downloaded project.",
          )}
          {trustButton("Review and trust…")}
        </>,
      );
  }
}

/** The types of a trusted plugin, or what the project uses of it while it is not started. */
function Types(props: { p: PluginInfo; onOpenFile: (path: string) => void }) {
  const { p } = props;
  if (!p.types) {
    const types = [...new Map(p.uses.map((u) => [u.kind + u.type, u])).values()];
    return (
      <div className="mb-2 rounded-md border border-dashed border-border px-3 py-2.5 text-xs text-muted-foreground">
        {p.state === "error" ? "The types are shown once the plugin starts." : "Runnel learns the types by starting the plugin, so they are shown once it is trusted."}
        {types.length > 0 && (
          <>
            {" "}
            Used in the project:
            <ul className="mt-1.5 mb-0 pl-4">
              {types.map((u) => (
                <li key={u.kind + u.type}>
                  {u.kind === "connection" && "connection type "}
                  <span className="font-mono">{u.type}</span> – {p.uses.filter((x) => x.kind === u.kind && x.type === u.type).length}{" "}
                  {u.kind === "connection" ? "connection(s)" : "step(s)"}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    );
  }
  const rows: [string, string, string | undefined, PluginUse[]][] = [
    ...p.types.actions.map((t) => ["Action", t.type, t.description, p.uses.filter((u) => u.kind === "action" && u.type === t.type)] as const),
    ...p.types.transforms.map((t) => ["Transform", t.type, t.description, p.uses.filter((u) => u.kind === "transform" && u.type === t.type)] as const),
    ...p.types.connectionTypes.map((t) => ["Connection type", t.type, t.description, p.uses.filter((u) => u.kind === "connection" && u.type === t.type)] as const),
  ].map((r) => [...r]);
  if (rows.length === 0) return <p className="m-0 mb-2 text-xs text-muted-foreground">The plugin offers no types.</p>;
  return (
    <>
      <table className="mb-2 w-full border-collapse text-xs">
        <thead>
          <tr className="text-left text-[10px] text-muted-foreground">
            <th className="w-[110px] pr-2 pb-1.5 font-semibold">Kind</th>
            <th className="pr-2 pb-1.5 font-semibold">Type</th>
            <th className="pr-2 pb-1.5 font-semibold">Description</th>
            <th className="pb-1.5 text-right font-semibold">Used</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([kind, type, description, uses]) => (
            <tr key={kind + type} className="border-t border-border align-top">
              <td className="py-1.5 pr-2">
                <span className="inline-flex h-[18px] items-center rounded-sm border border-border px-1.5 text-[10px] font-semibold whitespace-nowrap text-muted-foreground">{kind}</span>
              </td>
              <td className="py-1.5 pr-2 font-mono whitespace-nowrap">{type}</td>
              <td className="py-1.5 pr-2">{description}</td>
              <td className="py-1.5 text-right whitespace-nowrap text-muted-foreground">
                {uses.length === 0 ? (
                  "not used"
                ) : (
                  <button type="button" onClick={() => props.onOpenFile(uses[0].file)} className="cursor-pointer border-0 bg-transparent p-0 text-xs text-primary underline underline-offset-2">
                    {uses.length} {kind === "Connection type" ? (uses.length === 1 ? "connection" : "connections") : uses.length === 1 ? "step" : "steps"}
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="m-0 mb-2 text-2xs text-muted-foreground">Also in the palette and the reference; Runnel reads them from the plugin and keeps them until a file of the plugin changes.</p>
    </>
  );
}

/** "Trust plugin …?" (state trust of the mockup): what is trusted, before any code of the plugin runs. */
export function TrustDialog(props: { p: PluginInfo; projectDir?: string; busy: boolean; onTrust: () => void; onCancel: () => void }) {
  const { p } = props;
  const sep = props.projectDir?.includes("\\") ? "\\" : "/";
  const folder = props.projectDir ? props.projectDir + sep + p.folder.split("/").join(sep) : p.folder;
  return (
    <Dialog
      title={`Trust plugin ${p.name}?`}
      wide
      icon={<ShieldIcon width={16} height={16} />}
      iconClass="bg-accent text-accent-foreground"
      onCancel={props.onCancel}
      actions={[
        { label: "Cancel", onClick: props.onCancel },
        { label: props.busy ? "Trusting…" : "Trust plugin", onClick: props.onTrust, tone: "primary", disabled: props.busy || !p.hash },
      ]}
    >
      <p className="mt-0 mb-3.5 text-xs text-muted-foreground">
        A plugin is a program. Once trusted, Runnel starts it with the rights of your account – to list its types, to check workflows and in runs – and gives it the
        connections and secrets its steps name.
      </p>
      <dl className="m-0 mb-3 grid grid-cols-[110px_1fr] items-baseline gap-x-2.5 gap-y-1.5 text-xs">
        <dt className="text-muted-foreground">Name</dt>
        <dd className="m-0">
          <b>{p.name}</b> · version {p.version} · namespace <span className="font-mono">{p.namespace}</span>
        </dd>
        <dt className="text-muted-foreground">Folder</dt>
        <dd className="m-0 font-mono text-[11px] break-all">{folder}</dd>
        <dt className="text-muted-foreground">Program</dt>
        <dd className="m-0 font-mono text-[11px]">{p.program ?? "– none for this computer"}</dd>
        <dt className="text-muted-foreground">Files</dt>
        <dd className="m-0">
          {p.files} · {formatBytes(p.bytes)}
        </dd>
        <dt className="text-muted-foreground">SHA-256</dt>
        <dd className="m-0 font-mono text-[11px] break-all" data-testid="trust-hash">
          {p.hash}
        </dd>
        <dt className="text-muted-foreground">Trusted for</dt>
        <dd className="m-0">this project · this computer · your account</dd>
      </dl>
      <div className="mb-2.5 flex gap-2 rounded-sm bg-warning-soft px-2.5 py-2 text-2xs text-warning">
        <ShieldIcon width={14} height={14} className="shrink-0" />
        <span>
          <b className="text-foreground">Only trust plugins from a source you know.</b> The hash covers all files of the folder, as runnel plugins trust shows it; any
          change to one of them takes the approval back.
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-2 rounded-sm bg-muted px-2.5 py-2 text-2xs text-muted-foreground">
        For the Windows service, an administrator trusts it separately: <Command text={`runnel plugins trust --service --plugin ${p.name}`} />
      </div>
    </Dialog>
  );
}

/** The state of the plugin and its action in the bar of the file (mockup S52). */
export function PluginActions(props: { info: PluginInfo; canTrust: boolean; onTrust: () => void; onUntrust: () => void; onRetry: () => void }) {
  const { info } = props;
  const badge: Record<PluginInfo["state"], [string, string]> = {
    trusted: ["✓ Trusted", "bg-success-soft text-success"],
    "not-trusted": ["! Not trusted", "bg-warning-soft text-warning"],
    changed: ["! Changed since trusted", "bg-warning-soft text-warning"],
    error: ["✕ Cannot start", "bg-danger-soft text-destructive"],
  };
  const [text, tone] = badge[info.state];
  const plain = "inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted";
  const primary = "inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-primary bg-primary px-2.5 text-xs font-medium text-primary-foreground";
  const service = props.canTrust ? undefined : "Runnel runs as the Windows service: an administrator trusts plugins with runnel plugins trust --service";
  return (
    <>
      <span data-testid="plugin-state" className={`inline-flex h-5 items-center rounded-full px-2 text-2xs font-semibold whitespace-nowrap ${tone}`}>
        {text}
      </span>
      {info.state === "error" ? (
        <button type="button" className={plain} onClick={props.onRetry}>
          <ReloadIcon width={13} height={13} />
          Try again
        </button>
      ) : info.state === "trusted" ? (
        info.user?.current && (
          <button type="button" className={plain} disabled={!props.canTrust} title={service ?? "Takes back the approval of your account"} onClick={props.onUntrust}>
            Untrust
          </button>
        )
      ) : (
        <button type="button" className={primary + " disabled:cursor-default disabled:opacity-50"} disabled={!props.canTrust} title={service} onClick={props.onTrust}>
          <ShieldIcon width={13} height={13} />
          {info.state === "changed" ? "Trust again…" : "Trust…"}
        </button>
      )}
    </>
  );
}
