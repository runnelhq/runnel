import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import {
  categoryLabel,
  connectionColor,
  tileColor,
  transformCategoryLabel,
  useCatalog,
  useConnectionTypes,
  type CatalogKind,
  type ConnectionType,
  type Descriptor,
} from "../catalog";
import { exampleStep, helpFields, helpGroups, helpType, showValue, usePluginVersion, useVersion, type HelpField, type HelpKind } from "../help";
import { ActionIcon, BackIcon, CloseIcon, CodeIcon, ConnectionIcon, CopyIcon, LockIcon, PlugIcon, SearchIcon } from "./Icons";

/** What the help shows and how it was opened. */
export interface HelpTarget {
  type: string;
  kind: HelpKind;
  /** Opened from the properties panel: "‹ Properties" and Esc go back to it. */
  back: boolean;
  /** The id of the selected step whose help this is: "Show <id> in YAML". */
  step?: string;
}

/**
 * Help of an action or transform (mockup design/mockups/e15-help.html, E15d) or of a
 * connection type (mockup design/mockups/e15f-connection-help.html, E15g): the whole
 * reference from the catalog (E15b, E15e) in a wide panel: head with badges, the sections
 * and an example; a bar with the sections stays at the top. Empty sections are left out.
 */
export function HelpPanel(props: {
  target: HelpTarget;
  onOpen: (type: string, kind: HelpKind) => void;
  onClose: () => void;
  onShowInYAML?: (id: string) => void;
  /** Where the panel is: next to the file (default) or over the screen beside a dialog. */
  className?: string;
}) {
  const { type, kind, back, step } = props.target;
  const actions = useCatalog("workflow");
  const transforms = useCatalog("pipeline");
  const connections = useConnectionTypes();
  const catalog = kind === "workflow" ? actions : kind === "pipeline" ? transforms : connections;
  const other = kind === "workflow" ? transforms : actions;
  const d = kind === "connection" ? undefined : (catalog as Map<string, Descriptor>).get(type);
  const c = kind === "connection" ? connections.get(type) : undefined;
  const panel = useRef<HTMLElement>(null);
  const scroller = useRef<HTMLDivElement>(null);

  // Focus moves into the help so that Esc works at once.
  useEffect(() => panel.current?.focus({ preventScroll: true }), []);

  return (
    <section
      ref={panel}
      tabIndex={-1}
      aria-label="Help"
      data-testid="help-panel"
      onKeyDown={(e) => {
        if (e.key === "Escape" && !e.defaultPrevented) {
          e.preventDefault();
          props.onClose();
        }
      }}
      className={"flex min-h-0 w-[600px] shrink-0 flex-col border-l border-border bg-background outline-none " + (props.className ?? "")}
    >
      <div className="flex h-9 shrink-0 items-center gap-1.5 border-b border-border pr-2 pl-4">
        {back ? (
          <button
            type="button"
            onClick={props.onClose}
            title="Back to the properties (Esc)"
            className="inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-md border-0 bg-transparent px-2 text-2xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <BackIcon width={12} height={12} />
            Properties
          </button>
        ) : (
          <span className="label-caps">Help</span>
        )}
        <SearchHelp actions={actions} transforms={transforms} connections={connections} onOpen={props.onOpen} />
        <button
          type="button"
          title="Close help"
          aria-label="Close help"
          onClick={props.onClose}
          className="grid size-[26px] cursor-pointer place-items-center rounded-md border-0 bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <CloseIcon width={14} height={14} />
        </button>
      </div>
      <div ref={scroller} className="min-h-0 flex-1 overflow-auto pb-4">
        {d && kind !== "connection" ? (
          <Reference
            key={`${kind}:${type}`}
            d={d}
            kind={kind}
            scroller={scroller}
            pair={other.has(type)}
            step={step}
            onOpen={props.onOpen}
            onShowInYAML={props.onShowInYAML}
            known={(t, k) => (k === "workflow" ? actions : transforms).has(t)}
          />
        ) : c ? (
          <ConnectionReference key={`connection:${type}`} c={c} scroller={scroller} />
        ) : (
          <p className="mx-5 my-4 text-xs text-muted-foreground">
            {catalog.size === 0 ? "Loading…" : `No help for ${type}: the type is not known to this version of Runnel.`}
          </p>
        )}
      </div>
    </section>
  );
}

const kindName = (k: HelpKind) => (k === "workflow" ? "Action" : k === "pipeline" ? "Transform" : "Connection");

function Tile(props: { d: Descriptor; size: number; icon: number; radius: string }) {
  return (
    <span
      className={"grid shrink-0 place-items-center " + props.radius}
      style={{ width: props.size, height: props.size, background: tileColor(props.d), color: "var(--rn-tiefe-0)" }}
    >
      {props.d.plugin ? (
        <PlugIcon width={props.icon} height={props.icon} />
      ) : (
        <ActionIcon type={props.d.type} known width={props.icon} height={props.icon} />
      )}
    </span>
  );
}

/** The tile of a connection type: the sign of the type (S39b), a plug for plugins (mockup E15f). */
export function ConnectionTile(props: { type: string; plugin?: string; size: number; icon: number; radius: string }) {
  return (
    <span
      className={"grid shrink-0 place-items-center " + props.radius}
      style={{ width: props.size, height: props.size, background: connectionColor(props.type, props.plugin), color: "var(--rn-tiefe-0)" }}
    >
      {props.plugin ? (
        <PlugIcon width={props.icon} height={props.icon} />
      ) : (
        <ConnectionIcon type={props.type} width={props.icon} height={props.icon} />
      )}
    </span>
  );
}

function Badge(props: { tone?: "outline" | "plugin"; title?: string; children: ReactNode }) {
  const tone =
    props.tone === "outline"
      ? "border border-border bg-transparent font-medium text-muted-foreground"
      : props.tone === "plugin"
        ? "bg-accent font-semibold text-accent-foreground"
        : "bg-secondary font-semibold text-secondary-foreground";
  return (
    <span title={props.title} className={"inline-flex h-5 items-center gap-1 rounded-full px-2 text-2xs whitespace-nowrap " + tone}>
      {props.children}
    </span>
  );
}

function OriginBadge(props: { plugin?: string }) {
  const version = useVersion();
  const pluginVersion = usePluginVersion(props.plugin);
  return props.plugin ? (
    <Badge tone="plugin">
      <PlugIcon width={12} height={12} />
      Plugin {props.plugin}
      {pluginVersion && ` ${pluginVersion}`}
    </Badge>
  ) : (
    <Badge tone="outline">Built-in{version && ` · ${version}`}</Badge>
  );
}

const smallButton =
  "inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2 text-2xs font-medium text-foreground hover:bg-muted";

/** A section of the help: with a title it is in the bar; without, a note between the sections. */
interface Part {
  title?: string;
  count?: number;
  sub?: ReactNode;
  body: ReactNode;
}

/**
 * A page of the help: the head (tile, label, type, badges, description), the bar with the
 * sections, which shows the section in view and, once the head is gone, the tile and
 * label, and the sections.
 */
function Page(props: {
  scroller: RefObject<HTMLDivElement | null>;
  tile: (size: number, icon: number, radius: string) => ReactNode;
  label: string;
  type: string;
  badges: ReactNode;
  description: ReactNode;
  parts: Part[];
  /** At the end of the bar, e.g. the switch between action and transform. */
  barEnd?: ReactNode;
}) {
  const { scroller } = props;
  const sections = props.parts.filter((p) => p.title);
  const head = useRef<HTMLDivElement>(null);
  const bar = useRef<HTMLElement>(null);
  const refs = useRef<Record<string, HTMLDivElement | null>>({});
  const [on, setOn] = useState(0);
  const [stuck, setStuck] = useState(false);
  const jumped = useRef(false);

  const update = () => {
    const sc = scroller.current;
    if (!sc) return;
    setStuck(sc.scrollTop >= (head.current?.offsetHeight ?? 0));
    // After a jump link, its section stays marked, also when it is too near the end to reach the top.
    if (jumped.current) {
      jumped.current = false;
      return;
    }
    const top = sc.scrollTop + (bar.current?.offsetHeight ?? 0) + 8;
    let i = 0;
    sections.forEach(({ title }, n) => {
      const el = refs.current[title!];
      if (el && el.offsetTop <= top) i = n;
    });
    if (sc.scrollTop > 0 && sc.scrollTop + sc.clientHeight >= sc.scrollHeight - 2) i = sections.length - 1;
    setOn(i);
  };
  // Another type starts at the top (the component is keyed by the type).
  useLayoutEffect(() => {
    if (scroller.current) scroller.current.scrollTop = 0;
  }, [scroller]);
  useEffect(() => {
    const sc = scroller.current;
    if (!sc) return;
    update();
    sc.addEventListener("scroll", update);
    return () => sc.removeEventListener("scroll", update);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.type, scroller]);

  const jump = (i: number) => {
    const el = refs.current[sections[i].title!];
    const sc = scroller.current;
    if (!el || !sc) return;
    const top = Math.min(el.offsetTop - (bar.current?.offsetHeight ?? 0), sc.scrollHeight - sc.clientHeight);
    setOn(i);
    if (Math.abs(top - sc.scrollTop) < 1) return;
    jumped.current = true;
    sc.scrollTo({ top });
  };

  return (
    <>
      <div ref={head}>
        <div className="grid grid-cols-[auto_1fr] items-center gap-3 px-5 pt-4 pb-2">
          {props.tile(40, 22, "rounded-lg")}
          <div className="min-w-0">
            <h2 className="m-0 text-lg leading-tight font-semibold">{props.label}</h2>
            <div className="font-mono text-xs text-muted-foreground">{props.type}</div>
          </div>
        </div>
        <div data-testid="help-badges" className="flex flex-wrap gap-1.5 px-5 pb-2.5">
          {props.badges}
        </div>
        <p className="mx-5 mt-0 mb-3 text-sm">{props.description}</p>
      </div>
      <nav ref={bar} aria-label="Sections" className="sticky top-0 z-[2] flex items-center gap-1 border-b border-border bg-background px-4 py-1.5">
        {stuck && (
          <span className="mr-2 inline-flex items-center gap-1.5 text-xs font-semibold whitespace-nowrap">
            {props.tile(18, 11, "rounded-sm")}
            {props.label}
          </span>
        )}
        {sections.map(({ title, count }, i) => (
          <button
            key={title}
            type="button"
            aria-current={i === on ? "true" : undefined}
            onClick={() => jump(i)}
            className={
              "inline-flex h-6 cursor-pointer items-center gap-[5px] rounded-sm border-0 px-2 text-xs whitespace-nowrap " +
              (i === on ? "bg-muted font-medium text-foreground" : "bg-transparent text-muted-foreground hover:text-foreground")
            }
          >
            {title}
            {count !== undefined && <span className="text-[10px] font-normal text-muted-foreground">{count}</span>}
          </button>
        ))}
        {props.barEnd}
      </nav>
      {props.parts.map((p, i) =>
        p.title ? (
          <div
            key={p.title}
            ref={(el) => void (refs.current[p.title!] = el)}
            data-testid={`help-section-${p.title.toLowerCase().replace(" ", "-")}`}
            className="px-5 pt-3.5 pb-1"
          >
            <h3 className="m-0 mb-2 flex items-baseline gap-2 text-sm font-semibold">
              {p.title}
              {p.sub && <small className="text-2xs font-normal text-muted-foreground">{p.sub}</small>}
            </h3>
            {p.body}
          </div>
        ) : (
          <Fragment key={i}>{p.body}</Fragment>
        ),
      )}
    </>
  );
}

/** The neutral note for a plugin that describes only some of the reference (mockups E15c, E15f). */
function Missing(props: { children: ReactNode }) {
  return (
    <div className="mx-5 mt-1.5 mb-2 flex gap-2.5 rounded-md bg-muted px-3 py-2.5 text-xs leading-normal text-muted-foreground">
      <span aria-hidden className="grid size-[18px] shrink-0 place-items-center rounded-full border-[1.5px] border-current text-[11px] font-bold">
        i
      </span>
      <div>
        <b className="font-semibold text-foreground">No more details from the plugin.</b> {props.children}
      </div>
    </div>
  );
}

function Reference(props: {
  d: Descriptor;
  kind: CatalogKind;
  scroller: RefObject<HTMLDivElement | null>;
  pair: boolean;
  step?: string;
  known: (type: string, kind: CatalogKind) => boolean;
  onOpen: (type: string, kind: HelpKind) => void;
  onShowInYAML?: (id: string) => void;
}) {
  const { d, kind } = props;
  const action = kind === "workflow";
  const fields = useMemo(() => helpFields(d.schema), [d.schema]);
  const plain = !!d.plugin && !d.outputs?.length && !d.records && !d.errors && !d.limits?.length && !d.example;
  const outTitle = action ? "Output" : "Fields set";
  const text = (s: string) => <RefText text={s} known={props.known} onOpen={props.onOpen} />;
  const category = (action ? categoryLabel : transformCategoryLabel)[d.category] ?? (d.category ? d.category : "General");

  const parts: Part[] = [];
  if (fields.length)
    parts.push({
      title: "Fields",
      count: fields.length,
      sub: (
        <>
          under <span className="font-mono">with:</span>
        </>
      ),
      body: (
        <>
          <FieldTable widths={[136, 116]}>
            {fields.map((f, i) => (
              <FieldRow key={i} f={f} last={i === fields.length - 1} />
            ))}
          </FieldTable>
          <p className="mt-1.5 mb-2 text-2xs text-muted-foreground">
            <span className="font-bold text-destructive">*</span> required · <Chip def>value</Chip> default of the allowed values
          </p>
        </>
      ),
    });
  if (plain)
    parts.push({
      body: (
        <Missing>
          The plugin <span className="font-mono">{d.plugin}</span> describes its fields, but no output, errors, limits or example. Its documentation, or the Log
          of a run, shows what it returns.
        </Missing>
      ),
    });
  if (d.outputs?.length)
    parts.push({
      title: outTitle,
      count: d.outputs.length,
      sub: action ? (
        <>
          in <span className="font-mono">result.output</span> of the step
        </>
      ) : (
        "on every record passed on"
      ),
      body: (
        <>
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr>
                <Th width={action ? 150 : 170}>{action ? "Path" : "Field"}</Th>
                <Th width={96}>Type</Th>
                <Th>Description</Th>
              </tr>
            </thead>
            <tbody>
              {d.outputs.map((o, i) => {
                const b = i === d.outputs!.length - 1 ? "" : " border-b border-border";
                return (
                  <tr key={i}>
                    <td className={"py-1.5 pr-2 align-top font-mono text-xs font-medium whitespace-nowrap" + b}>{o.name}</td>
                    <td className={"py-1.5 pr-2 align-top font-mono text-2xs whitespace-nowrap text-muted-foreground" + b}>{o.type}</td>
                    <td className={"py-1.5 pr-2 align-top leading-[1.45]" + b}>{text(o.description)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!action && d.outputs.some((o) => o.name.includes("<")) && (
            <p className="mt-1.5 mb-2 text-2xs text-muted-foreground">
              <span className="font-mono">&lt;…&gt;</span> a name from the configuration
            </p>
          )}
        </>
      ),
    });
  if (!action && d.records) parts.push({ title: "Records", body: <p className="m-0 mb-2 text-xs leading-[1.55]">{text(d.records)}</p> });
  if (d.errors) parts.push({ title: "Errors", body: <p className="m-0 mb-2 text-xs leading-[1.55]">{text(d.errors)}</p> });
  if (d.limits?.length)
    parts.push({
      title: "Limits",
      count: d.limits.length,
      body: (
        <ul className="m-0 mb-2 pl-[18px] text-xs leading-[1.55]">
          {d.limits.map((l, i) => (
            <li key={i} className="mb-[3px] list-disc">
              {text(l)}
            </li>
          ))}
        </ul>
      ),
    });
  if (!plain && (!d.plugin || d.example)) {
    const step = props.step;
    parts.push({
      title: "Example",
      body: (
        <Example snippet={exampleStep(step ?? d.type.slice(d.type.indexOf(".") + 1), d.type, d.example)} type={d.type}>
          {step && props.onShowInYAML && (
            <button type="button" className={smallButton} onClick={() => props.onShowInYAML!(step)}>
              <CodeIcon width={12} height={12} />
              Show <b className="font-mono">{step}</b> in YAML
            </button>
          )}
        </Example>
      ),
    });
  }

  return (
    <Page
      scroller={props.scroller}
      tile={(size, icon, radius) => <Tile d={d} size={size} icon={icon} radius={radius} />}
      label={d.label}
      type={d.type}
      badges={
        <>
          <Badge>{kindName(kind)}</Badge>
          <Badge tone="outline">{category}</Badge>
          <OriginBadge plugin={d.plugin} />
          {d.recordErrors && (
            <Badge tone="outline" title="Records that fail go to the error link">
              Error link for records
            </Badge>
          )}
        </>
      }
      description={d.description}
      parts={parts}
      barEnd={
        props.pair && (
          <div
            role="group"
            aria-label="Same type as"
            title="Same type in workflows and pipelines"
            className="ml-auto inline-flex gap-0.5 rounded-md border border-border bg-muted p-0.5"
          >
            {(["workflow", "pipeline"] as const).map((k) => (
              <button
                key={k}
                type="button"
                aria-pressed={k === kind}
                onClick={() => k !== kind && props.onOpen(d.type, k)}
                className={
                  "cursor-pointer rounded-sm border-0 px-2 py-0.5 text-2xs whitespace-nowrap " +
                  (k === kind ? "bg-background text-foreground shadow-sm" : "bg-transparent text-muted-foreground")
                }
              >
                {kindName(k)}
              </button>
            ))}
          </div>
        )
      }
    />
  );
}

/**
 * The help of a connection type (mockup E15f): fields in the groups of the form, what Test
 * connection checks, errors and the example file; a plugin describes only fields and test.
 */
function ConnectionReference(props: { c: ConnectionType; scroller: RefObject<HTMLDivElement | null> }) {
  const { c } = props;
  const groups = useMemo(() => helpGroups(c.schema), [c.schema]);
  const fields = groups.flatMap(([, fs]) => fs);
  const names = fields.filter((f) => f.depth === 0).map((f) => f.name);
  const text = (s: string) => <CodeText text={s} names={names} />;
  const secret = fields.some((f) => f.schema.format === "runnel-secret");
  const tile = (size: number, icon: number, radius: string) => <ConnectionTile type={c.type} plugin={c.plugin} size={size} icon={icon} radius={radius} />;

  const parts: Part[] = [];
  if (fields.length)
    parts.push({
      title: "Fields",
      count: fields.length,
      sub: (
        <>
          under <span className="font-mono">with:</span>
          {groups.some(([g]) => g) && ", in the groups of the form"}
        </>
      ),
      body: (
        <>
          <FieldTable widths={[120, 78]}>
            {groups.map(([g, fs]) => (
              <Fragment key={g}>
                {g && (
                  <tr>
                    <td colSpan={4} className="pt-2 pb-[3px] text-[10px] font-semibold tracking-[.05em] text-muted-foreground uppercase">
                      {g}
                    </td>
                  </tr>
                )}
                {fs.map((f) => (
                  <FieldRow key={f.name + f.depth} f={f} last={false} />
                ))}
              </Fragment>
            ))}
          </FieldTable>
          <p className="mt-1.5 mb-2 text-2xs text-muted-foreground">
            <span className="font-bold text-destructive">*</span> required
            {secret && (
              <>
                {" "}
                · <LockIcon width={11} height={11} className="inline align-[-1px]" /> secret: takes <span className="font-mono">{"${secret:NAME}"}</span>, the
                form shows only the reference
              </>
            )}
          </p>
        </>
      ),
    });
  if (c.test)
    parts.push({
      title: "Test connection",
      sub: "with the button in the file bar, for the chosen environment",
      body: (
        <div className="mb-2 flex items-start gap-2.5 rounded-md border border-border px-3 py-2.5 text-xs leading-[1.55]">
          <span className="pt-0.5 text-muted-foreground">
            <PlugIcon width={15} height={15} />
          </span>
          <span>{text(c.test)}</span>
        </div>
      ),
    });
  if (c.plugin && !c.errors && !c.example)
    parts.push({
      body: (
        <Missing>
          The plugin <span className="font-mono">{c.plugin}</span> describes the fields of its connection type{c.test ? " and offers a test" : ""}, but no{" "}
          {c.test ? "" : "test, "}errors or example. Its documentation{c.test ? ", or the message of Test connection," : ""} shows more.
        </Missing>
      ),
    });
  if (c.errors)
    parts.push({ title: "Errors", sub: "when connecting and signing in fail", body: <p className="m-0 mb-2 text-xs leading-[1.55]">{text(c.errors)}</p> });
  if (c.example)
    parts.push({
      title: "Example",
      sub: <span className="font-mono">connections/&lt;name&gt;.yaml</span>,
      body: <Example snippet={c.example} type={c.type} />,
    });

  return (
    <Page
      scroller={props.scroller}
      tile={tile}
      label={c.label}
      type={c.type}
      badges={
        <>
          <Badge>Connection type</Badge>
          <OriginBadge plugin={c.plugin} />
          {c.test && (
            <Badge tone="outline" title="Test connection checks connections of this type">
              <PlugIcon width={12} height={12} />
              Test connection
            </Badge>
          )}
        </>
      }
      description={text(c.description)}
      parts={parts}
    />
  );
}

/**
 * A text of a connection type with names of its fields in the font of code (mockup E15f):
 * those in camel case (hostKey) and a field with its value (insecure: true); plain words
 * such as host or user stay text.
 */
export function CodeText(props: { text: string; names: string[] }) {
  const camel = props.names.filter((n) => /[A-Z]/.test(n));
  const alts = [...(props.names.length ? [`(?:${props.names.join("|")}): [\\w-]+`] : []), ...camel];
  if (alts.length === 0) return props.text;
  const parts = props.text.split(new RegExp(`\\b(${alts.join("|")})\\b`));
  return parts.map((p, i) =>
    i % 2 === 1 ? (
      <span key={i} className="font-mono">
        {p}
      </span>
    ) : (
      <Fragment key={i}>{p}</Fragment>
    ),
  );
}

function Example(props: { snippet: string; type: string; children?: ReactNode }) {
  const [copied, setCopied] = useState(false);
  return (
    <>
      <pre data-testid="help-example" className="m-0 mb-2 overflow-auto rounded-md bg-muted px-3 py-2.5 font-mono text-xs leading-[1.6] text-foreground">
        {highlight(props.snippet)}
      </pre>
      <div className="mb-2 flex items-center gap-2">
        <button
          type="button"
          className={smallButton}
          onClick={() => {
            void navigator.clipboard?.writeText(props.snippet).then(() => setCopied(true));
          }}
        >
          <CopyIcon width={12} height={12} />
          {copied ? "Copied" : "Copy example"}
        </button>
        {props.children}
        <small className="ml-auto text-2xs text-muted-foreground">
          Also in the terminal: <span className="font-mono">runnel catalog {props.type}</span>
        </small>
      </div>
    </>
  );
}

/** The example with keys in medium weight, quoted texts muted and ${…} or {{ … }} in the colour of expressions. */
function highlight(snippet: string): ReactNode {
  return snippet
    .replace(/\n$/, "")
    .split("\n")
    .map((line, i) => {
      const m = /^(\s*(?:- )?[\w.-]+:)?(.*)$/.exec(line)!;
      const parts = m[2].split(/("(?:[^"\\]|\\.)*"|\$\{[^}]*\}|\{\{.*?\}\})/);
      return (
        <Fragment key={i}>
          {i > 0 && "\n"}
          {m[1] && <span className="font-medium">{m[1]}</span>}
          {parts.map((p, j) =>
            j % 2 === 0 ? (
              p
            ) : p.startsWith('"') ? (
              <span key={j} className="text-muted-foreground">
                {p}
              </span>
            ) : (
              <span key={j} className="rounded-[3px] bg-expression-soft text-expression">
                {p}
              </span>
            ),
          )}
        </Fragment>
      );
    });
}

/** A text of the reference; "the action <type>" or "the transform <type>" opens that help. */
function RefText(props: { text: string; known: (type: string, kind: CatalogKind) => boolean; onOpen: (type: string, kind: HelpKind) => void }) {
  const parts = props.text.split(/\b((?:action|transform) [a-z][a-z0-9-]*\.[a-z0-9.-]*[a-z0-9])/);
  return parts.map((p, i) => {
    const m = i % 2 === 1 ? /^(action|transform) (.*)$/.exec(p) : null;
    const kind: CatalogKind = m?.[1] === "action" ? "workflow" : "pipeline";
    if (!m || !props.known(m[2], kind)) return <Fragment key={i}>{p}</Fragment>;
    return (
      <Fragment key={i}>
        {m[1]}{" "}
        <button
          type="button"
          onClick={() => props.onOpen(m[2], kind)}
          className="cursor-pointer border-0 bg-transparent p-0 text-primary underline underline-offset-2"
        >
          {m[2]}
        </button>
      </Fragment>
    );
  });
}

function Th(props: { width?: number; children: ReactNode }) {
  return (
    <th
      style={props.width ? { width: props.width } : undefined}
      className="border-b border-border pt-1 pr-2 pb-[5px] text-left text-[10px] font-semibold tracking-[.06em] whitespace-nowrap text-muted-foreground uppercase"
    >
      {props.children}
    </th>
  );
}

function FieldTable(props: { widths: [number, number]; children: ReactNode }) {
  return (
    <table className="w-full border-collapse text-xs">
      <thead>
        <tr>
          <Th width={props.widths[0]}>Name</Th>
          <Th width={props.widths[1]}>Type</Th>
          <Th width={60}>Default</Th>
          <Th>Description</Th>
        </tr>
      </thead>
      <tbody>{props.children}</tbody>
    </table>
  );
}

function Chip(props: { def?: boolean; children: ReactNode }) {
  return (
    <span
      className={
        "mr-[3px] mb-[3px] inline-block rounded-sm border bg-muted px-[5px] font-mono text-[10px] leading-4 " + (props.def ? "border-primary" : "border-border")
      }
    >
      {props.children}
    </span>
  );
}

function FieldRow(props: { f: HelpField; last: boolean }) {
  const { f } = props;
  const s = f.schema;
  const b = props.last ? "" : " border-b border-border";
  const only = Object.entries(s["x-runnel-only"] ?? {});
  return (
    <tr data-testid={`help-field-${f.name}`}>
      <td className={"py-1.5 pr-2 align-top font-mono text-xs font-medium whitespace-nowrap" + b} style={f.depth ? { paddingLeft: 4 + 14 * (f.depth - 1) } : undefined}>
        {f.depth > 0 && (
          <span className="inline-block w-[14px] text-muted-foreground" title={f.where}>
            └
          </span>
        )}
        {f.name}
        {f.required && (
          <>
            <span className="font-bold text-destructive" title="Required">
              *
            </span>
            <small className="mt-px block font-sans text-[10px] font-normal text-muted-foreground">required</small>
          </>
        )}
      </td>
      <td className={"py-1.5 pr-2 align-top font-mono text-2xs whitespace-nowrap text-muted-foreground" + b}>
        {s.format === "runnel-secret" && <LockIcon width={12} height={12} className="mr-1 inline align-[-2px]" />}
        {helpType(s)}
      </td>
      <td className={"py-1.5 pr-2 align-top font-mono text-2xs whitespace-nowrap" + b}>
        {s.default !== undefined ? showValue(s.default) : <span className="font-sans text-muted-foreground">–</span>}
      </td>
      <td className={"py-1.5 pr-2 align-top leading-[1.45]" + b}>
        {s.description}
        {s.enum && (
          <div className="mt-1">
            {s.enum.map((v) => (
              <Chip key={String(v)} def={v === s.default}>
                {showValue(v)}
              </Chip>
            ))}
          </div>
        )}
        {s.examples?.length ? (
          <div className="mt-0.5 text-2xs text-muted-foreground">
            e.g. <span className="font-mono text-foreground">{showValue(s.examples[0])}</span>
          </div>
        ) : null}
        {only.length > 0 && (
          <div className="mt-0.5 text-2xs text-muted-foreground">
            Only with{" "}
            <span className="font-mono">{only.map(([k, vs]) => `${k}: ${vs.map(showValue).join(", ")}`).join("; ")}</span>
          </div>
        )}
      </td>
    </tr>
  );
}

/** The parts of a text that match one of the words, marked (the search of the help). */
function Marked(props: { text: string; words: string[] }) {
  const esc = props.words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const parts = props.text.split(new RegExp(`(${esc.join("|")})`, "i"));
  return parts.map((p, i) =>
    i % 2 === 1 ? (
      <b key={i} className="rounded-[2px] bg-ring/20 font-[inherit]">
        {p}
      </b>
    ) : (
      <Fragment key={i}>{p}</Fragment>
    ),
  );
}

type Found = { type: string; label: string; kind: HelpKind; tile: ReactNode };

/**
 * "Search help" (Ctrl+K) in the head of the help (mockups E15c, E15f): finds connection
 * types and actions and transforms (also of plugins) by type, label and description, as
 * the palette does, and opens their help. Connection types come first.
 */
function SearchHelp(props: {
  actions: Map<string, Descriptor>;
  transforms: Map<string, Descriptor>;
  connections: Map<string, ConnectionType>;
  onOpen: (type: string, kind: HelpKind) => void;
}) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  // Ctrl+K jumps into the search while the help is shown.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        input.current?.focus();
        input.current?.select();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const { conns, blocks, more } = useMemo(() => {
    const match = (s: string) => words.every((w) => s.toLowerCase().includes(w));
    if (words.length === 0) return { conns: [], blocks: [], more: 0 };
    const conns: Found[] = [...props.connections.values()]
      .filter((c) => match(`${c.type} ${c.label} ${c.description}`))
      .sort((a, b) => a.label.localeCompare(b.label))
      .map((c) => ({ type: c.type, label: c.label, kind: "connection", tile: <ConnectionTile type={c.type} plugin={c.plugin} size={22} icon={13} radius="rounded-sm" /> }));
    const all = [
      ...[...props.actions.values()].map((d) => ({ d, kind: "workflow" as const })),
      ...[...props.transforms.values()].map((d) => ({ d, kind: "pipeline" as const })),
    ];
    const blocks: Found[] = all
      .filter(({ d }) => match(`${d.type} ${d.label} ${d.description}`))
      .sort((a, b) => a.d.label.localeCompare(b.d.label) || a.kind.localeCompare(b.kind))
      .map(({ d, kind }) => ({ type: d.type, label: d.label, kind, tile: <Tile d={d} size={22} icon={13} radius="rounded-sm" /> }));
    // Below connection types, the building blocks are shortened (mockup E15f).
    const shown = conns.length > 0 ? 3 : 10;
    return { conns, blocks: blocks.slice(0, shown), more: Math.max(0, blocks.length - shown) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.actions, props.transforms, props.connections, words.join(" ")]);
  const found = [...conns, ...blocks];

  const open = (i: number) => {
    const r = found[i];
    if (!r) return;
    setQuery("");
    input.current?.blur();
    props.onOpen(r.type, r.kind);
  };
  const group = (title: string, count: number) => (
    <li role="presentation" className="flex px-2 pt-1.5 pb-0.5 text-[10px] font-semibold tracking-[.05em] text-muted-foreground uppercase">
      {title}
      <span className="ml-auto font-normal tracking-normal normal-case">{count}</span>
    </li>
  );
  const option = (r: Found, i: number) => (
    <li
      key={`${r.kind}:${r.type}`}
      role="option"
      aria-selected={i === active}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => open(i)}
      onMouseEnter={() => setActive(i)}
      className={"flex h-[34px] cursor-pointer items-center gap-2 rounded-md px-2 " + (i === active ? "bg-accent text-accent-foreground" : "")}
    >
      {r.tile}
      <span className="min-w-0 flex-1 leading-tight">
        <span className="block truncate text-xs">
          <Marked text={r.label} words={words} />
        </span>
        <small className="block truncate font-mono text-[10px] text-muted-foreground">
          <Marked text={r.type} words={words} />
        </small>
      </span>
      <span className="text-2xs text-muted-foreground">{kindName(r.kind)}</span>
    </li>
  );

  return (
    <div className="relative ml-auto">
      <label className="flex h-[26px] w-[240px] items-center gap-1.5 rounded-md border border-input bg-background px-2 text-xs text-muted-foreground focus-within:border-ring focus-within:outline-3 focus-within:outline-ring/20">
        <SearchIcon width={14} height={14} />
        <input
          ref={input}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(0, Math.min(found.length - 1, a + (e.key === "ArrowDown" ? 1 : -1))));
            } else if (e.key === "Enter") {
              e.preventDefault();
              open(active);
            } else if (e.key === "Escape" && query) {
              e.preventDefault(); // clears the search, the help stays
              setQuery("");
            }
          }}
          placeholder="Search help"
          aria-label="Search help"
          className="min-w-0 flex-1 border-0 bg-transparent text-foreground outline-none"
        />
        <kbd className="rounded-[3px] border border-border px-1 font-mono text-[10px]">Ctrl+K</kbd>
      </label>
      {words.length > 0 && (
        <ul
          role="listbox"
          aria-label="Help"
          className="absolute top-full right-0 z-10 m-0 mt-1 max-h-[420px] w-[380px] list-none overflow-auto rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
        >
          {conns.length > 0 && group("Connection types", conns.length)}
          {conns.map((r, i) => option(r, i))}
          {conns.length > 0 && blocks.length > 0 && <li role="presentation" className="my-1 h-px bg-border" />}
          {blocks.length > 0 && group("Actions and transforms", blocks.length + more)}
          {blocks.map((r, i) => option(r, conns.length + i))}
          {more > 0 && (
            <li role="presentation" className="px-2 pt-1 pb-0.5 text-2xs text-muted-foreground">
              {more} more
            </li>
          )}
          {found.length === 0 && <li className="px-2 py-1.5 text-xs text-muted-foreground">Nothing matches “{query}”.</li>}
        </ul>
      )}
    </div>
  );
}
