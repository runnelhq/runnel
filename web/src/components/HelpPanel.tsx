import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { categoryLabel, tileColor, transformCategoryLabel, useCatalog, type CatalogKind, type Descriptor } from "../catalog";
import { exampleStep, helpFields, helpType, showValue, usePluginVersion, useVersion, type HelpField } from "../help";
import { ActionIcon, BackIcon, CloseIcon, CodeIcon, CopyIcon, PlugIcon } from "./Icons";

/** What the help shows and how it was opened. */
export interface HelpTarget {
  type: string;
  kind: CatalogKind;
  /** Opened from the properties panel: "‹ Properties" and Esc go back to it. */
  back: boolean;
  /** The id of the selected step whose help this is: "Show <id> in YAML". */
  step?: string;
}

/**
 * Help of an action or transform (mockup design/mockups/e15-help.html, E15d): the whole
 * reference from its descriptor (E15b) in a wide panel in place of the properties: head
 * with badges, fields with sub-fields, output or fields set, records, errors, limits and
 * an example; a bar with the sections stays at the top. Empty sections are left out.
 */
export function HelpPanel(props: {
  target: HelpTarget;
  onOpen: (type: string, kind: CatalogKind) => void;
  onClose: () => void;
  onShowInYAML?: (id: string) => void;
}) {
  const { type, kind, back, step } = props.target;
  const actions = useCatalog("workflow");
  const transforms = useCatalog("pipeline");
  const catalog = kind === "workflow" ? actions : transforms;
  const other = kind === "workflow" ? transforms : actions;
  const d = catalog.get(type);
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
      className="flex min-h-0 w-[600px] shrink-0 flex-col border-l border-border bg-background outline-none"
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
        <button
          type="button"
          title="Close help"
          aria-label="Close help"
          onClick={props.onClose}
          className="ml-auto grid size-[26px] cursor-pointer place-items-center rounded-md border-0 bg-transparent text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <CloseIcon width={14} height={14} />
        </button>
      </div>
      <div ref={scroller} className="min-h-0 flex-1 overflow-auto pb-4">
        {d ? (
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
        ) : (
          <p className="mx-5 my-4 text-xs text-muted-foreground">
            {catalog.size === 0 ? "Loading…" : `No help for ${type}: the type is not known to this version of Runnel.`}
          </p>
        )}
      </div>
    </section>
  );
}

const kindName = (k: CatalogKind) => (k === "workflow" ? "Action" : "Transform");

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

const smallButton =
  "inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-md border border-input bg-background px-2 text-2xs font-medium text-foreground hover:bg-muted";

function Reference(props: {
  d: Descriptor;
  kind: CatalogKind;
  scroller: RefObject<HTMLDivElement | null>;
  pair: boolean;
  step?: string;
  known: (type: string, kind: CatalogKind) => boolean;
  onOpen: (type: string, kind: CatalogKind) => void;
  onShowInYAML?: (id: string) => void;
}) {
  const { d, kind, scroller } = props;
  const action = kind === "workflow";
  const version = useVersion();
  const pluginVersion = usePluginVersion(d.plugin);
  const fields = useMemo(() => helpFields(d.schema), [d.schema]);
  const plain = !!d.plugin && !d.outputs?.length && !d.records && !d.errors && !d.limits?.length && !d.example;
  const outTitle = action ? "Output" : "Fields set";
  const sections: [string, number | undefined][] = [];
  if (fields.length) sections.push(["Fields", fields.length]);
  if (d.outputs?.length) sections.push([outTitle, d.outputs.length]);
  if (!action && d.records) sections.push(["Records", undefined]);
  if (d.errors) sections.push(["Errors", undefined]);
  if (d.limits?.length) sections.push(["Limits", d.limits.length]);
  if (!plain && (!d.plugin || d.example)) sections.push(["Example", undefined]);

  const head = useRef<HTMLDivElement>(null);
  const bar = useRef<HTMLElement>(null);
  const refs = useRef<Record<string, HTMLDivElement | null>>({});
  const [on, setOn] = useState(0);
  const [stuck, setStuck] = useState(false);
  const jumped = useRef(false);

  // The bar shows the section in view and, once the head is gone, the tile and label.
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
    sections.forEach(([s], n) => {
      const el = refs.current[s];
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
  }, [d, scroller]);

  const jump = (i: number) => {
    const el = refs.current[sections[i][0]];
    const sc = scroller.current;
    if (!el || !sc) return;
    const top = Math.min(el.offsetTop - (bar.current?.offsetHeight ?? 0), sc.scrollHeight - sc.clientHeight);
    setOn(i);
    if (Math.abs(top - sc.scrollTop) < 1) return;
    jumped.current = true;
    sc.scrollTo({ top });
  };
  const section = (title: string, sub: ReactNode, children: ReactNode) => (
    <div ref={(el) => void (refs.current[title] = el)} data-testid={`help-section-${title.toLowerCase().replace(" ", "-")}`} className="px-5 pt-3.5 pb-1">
      <h3 className="m-0 mb-2 flex items-baseline gap-2 text-sm font-semibold">
        {title}
        {sub && <small className="text-2xs font-normal text-muted-foreground">{sub}</small>}
      </h3>
      {children}
    </div>
  );
  const text = (s: string) => <RefText text={s} known={props.known} onOpen={props.onOpen} />;
  const category = (action ? categoryLabel : transformCategoryLabel)[d.category] ?? (d.category ? d.category : "General");

  return (
    <>
      <div ref={head}>
        <div className="grid grid-cols-[auto_1fr] items-center gap-3 px-5 pt-4 pb-2">
          <Tile d={d} size={40} icon={22} radius="rounded-lg" />
          <div className="min-w-0">
            <h2 className="m-0 text-lg leading-tight font-semibold">{d.label}</h2>
            <div className="font-mono text-xs text-muted-foreground">{d.type}</div>
          </div>
        </div>
        <div data-testid="help-badges" className="flex flex-wrap gap-1.5 px-5 pb-2.5">
          <Badge>{kindName(kind)}</Badge>
          <Badge tone="outline">{category}</Badge>
          {d.plugin ? (
            <Badge tone="plugin">
              <PlugIcon width={12} height={12} />
              Plugin {d.plugin}
              {pluginVersion && ` ${pluginVersion}`}
            </Badge>
          ) : (
            <Badge tone="outline">Built-in{version && ` · ${version}`}</Badge>
          )}
          {d.recordErrors && (
            <Badge tone="outline" title="Records that fail go to the error link">
              Error link for records
            </Badge>
          )}
        </div>
        <p className="mx-5 mt-0 mb-3 text-sm">{d.description}</p>
      </div>
      <nav ref={bar} aria-label="Sections" className="sticky top-0 z-[2] flex items-center gap-1 border-b border-border bg-background px-4 py-1.5">
        {stuck && (
          <span className="mr-2 inline-flex items-center gap-1.5 text-xs font-semibold">
            <Tile d={d} size={18} icon={11} radius="rounded-sm" />
            {d.label}
          </span>
        )}
        {sections.map(([s, n], i) => (
          <button
            key={s}
            type="button"
            aria-current={i === on ? "true" : undefined}
            onClick={() => jump(i)}
            className={
              "inline-flex h-6 cursor-pointer items-center gap-[5px] rounded-sm border-0 px-2 text-xs whitespace-nowrap " +
              (i === on ? "bg-muted font-medium text-foreground" : "bg-transparent text-muted-foreground hover:text-foreground")
            }
          >
            {s}
            {n !== undefined && <span className="text-[10px] font-normal text-muted-foreground">{n}</span>}
          </button>
        ))}
        {props.pair && (
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
        )}
      </nav>
      {fields.length > 0 &&
        section(
          "Fields",
          <>
            under <span className="font-mono">with:</span>
          </>,
          <>
            <table className="w-full border-collapse text-xs">
              <thead>
                <tr>
                  <Th width={136}>Name</Th>
                  <Th width={116}>Type</Th>
                  <Th width={60}>Default</Th>
                  <Th>Description</Th>
                </tr>
              </thead>
              <tbody>
                {fields.map((f, i) => (
                  <FieldRow key={i} f={f} last={i === fields.length - 1} />
                ))}
              </tbody>
            </table>
            <p className="mt-1.5 mb-2 text-2xs text-muted-foreground">
              <span className="font-bold text-destructive">*</span> required · <Chip def>value</Chip> default of the allowed values
            </p>
          </>,
        )}
      {plain && (
        <div className="mx-5 mt-1.5 mb-2 flex gap-2.5 rounded-md bg-muted px-3 py-2.5 text-xs leading-normal text-muted-foreground">
          <span aria-hidden className="grid size-[18px] shrink-0 place-items-center rounded-full border-[1.5px] border-current text-[11px] font-bold">
            i
          </span>
          <div>
            <b className="font-semibold text-foreground">No more details from the plugin.</b> The plugin <span className="font-mono">{d.plugin}</span> describes
            its fields, but no output, errors, limits or example. Its documentation, or the Log of a run, shows what it returns.
          </div>
        </div>
      )}
      {!!d.outputs?.length &&
        section(
          outTitle,
          action ? (
            <>
              in <span className="font-mono">result.output</span> of the step
            </>
          ) : (
            "on every record passed on"
          ),
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
          </>,
        )}
      {!action && d.records && section("Records", null, <p className="m-0 mb-2 text-xs leading-[1.55]">{text(d.records)}</p>)}
      {d.errors && section("Errors", null, <p className="m-0 mb-2 text-xs leading-[1.55]">{text(d.errors)}</p>)}
      {!!d.limits?.length &&
        section(
          "Limits",
          null,
          <ul className="m-0 mb-2 pl-[18px] text-xs leading-[1.55]">
            {d.limits.map((l, i) => (
              <li key={i} className="mb-[3px] list-disc">
                {text(l)}
              </li>
            ))}
          </ul>,
        )}
      {sections.some(([s]) => s === "Example") && (
        <Example d={d} step={props.step} onShowInYAML={props.onShowInYAML} section={section} />
      )}
    </>
  );
}

function Example(props: {
  d: Descriptor;
  step?: string;
  onShowInYAML?: (id: string) => void;
  section: (title: string, sub: ReactNode, children: ReactNode) => ReactNode;
}) {
  const { d, step } = props;
  const [copied, setCopied] = useState(false);
  const snippet = exampleStep(step ?? d.type.slice(d.type.indexOf(".") + 1), d.type, d.example);
  return props.section(
    "Example",
    null,
    <>
      <pre data-testid="help-example" className="m-0 mb-2 overflow-auto rounded-md bg-muted px-3 py-2.5 font-mono text-xs leading-[1.6] text-foreground">
        {highlight(snippet)}
      </pre>
      <div className="mb-2 flex items-center gap-2">
        <button
          type="button"
          className={smallButton}
          onClick={() => {
            void navigator.clipboard?.writeText(snippet).then(() => setCopied(true));
          }}
        >
          <CopyIcon width={12} height={12} />
          {copied ? "Copied" : "Copy example"}
        </button>
        {step && props.onShowInYAML && (
          <button type="button" className={smallButton} onClick={() => props.onShowInYAML!(step)}>
            <CodeIcon width={12} height={12} />
            Show <b className="font-mono">{step}</b> in YAML
          </button>
        )}
        <small className="ml-auto text-2xs text-muted-foreground">
          Also in the terminal: <span className="font-mono">runnel catalog {d.type}</span>
        </small>
      </div>
    </>,
  );
}

/** The example with keys in medium weight, quoted texts muted and ${…} or {{ … }} in the colour of expressions. */
function highlight(snippet: string): ReactNode {
  return snippet.split("\n").map((line, i) => {
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
function RefText(props: { text: string; known: (type: string, kind: CatalogKind) => boolean; onOpen: (type: string, kind: CatalogKind) => void }) {
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
      <td className={"py-1.5 pr-2 align-top font-mono text-2xs whitespace-nowrap text-muted-foreground" + b}>{helpType(s)}</td>
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
