import { useEffect, useRef } from "react";
import { defaultKeymap } from "@codemirror/commands";
import { search, searchKeymap } from "@codemirror/search";
import { EditorState, RangeSet, StateEffect, StateField, Transaction, type Range } from "@codemirror/state";
import {
  Decoration,
  drawSelection,
  EditorView,
  gutter,
  GutterMarker,
  highlightActiveLine,
  highlightActiveLineGutter,
  hoverTooltip,
  keymap,
  lineNumbers,
  showTooltip,
  ViewPlugin,
  type DecorationSet,
  type Tooltip,
  type ViewUpdate,
} from "@codemirror/view";
import { highlightLine, type Token, type TokenKind } from "../yaml";
import { highlightMarkdownLine } from "../markdown";
import type { Problem } from "../workflow";
import { applyDelay } from "./PropertiesPanel";

/** Classes of the tokens (index.css, section "YAML editor"); colours as in the read-only view (mockup E4). */
const tokenClass: Partial<Record<TokenKind, string>> = {
  comment: "ry-comment",
  key: "ry-key",
  punct: "ry-punct",
  value: "ry-value",
  var: "ry-var",
  heading: "ry-heading",
  link: "ry-link",
  code: "ry-value",
};

/** Highlights the visible lines with the tokenizer of the read-only view (YAML) or of Markdown. */
const highlighter = (highlightLine: (line: string) => Token[]) => ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    constructor(view: EditorView) {
      this.decorations = this.build(view);
    }
    update(u: ViewUpdate) {
      if (u.docChanged || u.viewportChanged) this.decorations = this.build(u.view);
    }
    build(view: EditorView) {
      const marks: Range<Decoration>[] = [];
      for (const { from, to } of view.visibleRanges) {
        for (let pos = from; pos <= to; ) {
          const line = view.state.doc.lineAt(pos);
          let at = line.from;
          for (const t of highlightLine(line.text)) {
            const cls = tokenClass[t.kind];
            if (cls && t.text.length > 0) marks.push(Decoration.mark({ class: cls }).range(at, at + t.text.length));
            at += t.text.length;
          }
          pos = line.to + 1;
        }
      }
      return Decoration.set(marks, true);
    }
  },
  { decorations: (v) => v.decorations },
);

/** A problem placed in the text: from its line and column to the end of the line. */
interface Placed {
  from: number;
  to: number;
  line: number;
  problem: Problem;
}

function place(state: EditorState, problems: Problem[]): Placed[] {
  const out: Placed[] = [];
  for (const p of problems) {
    if (!p.line || p.line > state.doc.lines) continue;
    const line = state.doc.line(p.line);
    const text = line.text.replace(/\s+$/, "");
    let from = line.from + Math.min(Math.max((p.column ?? 1) - 1, 0), text.length);
    if (from >= line.from + text.length) from = line.from + text.length - text.trimStart().length;
    out.push({ from, to: Math.max(from, line.from + text.length), line: p.line, problem: p });
  }
  return out;
}

class ProblemMarker extends GutterMarker {
  constructor(readonly severity: Problem["severity"]) {
    super();
  }
  eq(other: ProblemMarker) {
    return other.severity === this.severity;
  }
  toDOM() {
    const span = document.createElement("span");
    span.className = this.severity === "error" ? "ry-mark-error" : "ry-mark-warning";
    span.textContent = this.severity === "error" ? "✖" : "⚠";
    return span;
  }
}

/** The problems of the last check: wavy underline, a mark in the gutter, the message at the cursor and on hover. */
const setProblems = StateEffect.define<Problem[]>();
const problemField = StateField.define<{ placed: Placed[]; deco: DecorationSet; marks: RangeSet<GutterMarker> }>({
  create: () => ({ placed: [], deco: Decoration.none, marks: RangeSet.empty }),
  update(value, tr) {
    for (const e of tr.effects) {
      if (e.is(setProblems)) {
        const placed = place(tr.state, e.value);
        const deco = Decoration.set(
          placed.filter((p) => p.to > p.from).map((p) => Decoration.mark({ class: p.problem.severity === "error" ? "rx-error" : "ry-warning" }).range(p.from, p.to)),
          true,
        );
        const first = new Map<number, Placed>();
        for (const p of placed) if (!first.has(p.line) || p.problem.severity === "error") first.set(p.line, p);
        const marks = RangeSet.of(
          [...first.values()].map((p) => new ProblemMarker(p.problem.severity).range(tr.state.doc.line(p.line).from)),
          true,
        );
        return { placed, deco, marks };
      }
    }
    // Typing moves the marks with the text until the next check.
    if (!tr.docChanged) return value;
    return {
      placed: value.placed.map((p) => ({ ...p, from: tr.changes.mapPos(p.from), to: tr.changes.mapPos(p.to, 1) })),
      deco: value.deco.map(tr.changes),
      marks: value.marks.map(tr.changes),
    };
  },
  provide: (f) => [
    EditorView.decorations.from(f, (v) => v.deco),
    gutter({ class: "ry-problems", markers: (view) => view.state.field(f).marks }),
  ],
});

function problemTooltip(placed: Placed[], pos: number): Tooltip | null {
  const here = placed.filter((p) => p.from <= pos && pos <= Math.max(p.to, p.from));
  if (here.length === 0) return null;
  return {
    pos: here[0].from,
    above: true,
    create: () => {
      const dom = document.createElement("div");
      dom.className = "ry-tooltip";
      for (const p of here) {
        const row = document.createElement("div");
        const sym = document.createElement("span");
        sym.className = p.problem.severity === "error" ? "ry-mark-error" : "ry-mark-warning";
        sym.textContent = (p.problem.severity === "error" ? "✖" : "⚠") + " ";
        row.append(sym, p.problem.message);
        const where = document.createElement("span");
        where.className = "ry-where";
        where.textContent = `${p.line}:${p.problem.column ?? 1}`;
        row.append(where);
        dom.append(row);
      }
      return { dom };
    },
  };
}

/** The message of the problem at the cursor stays visible while typing there. */
const cursorTooltip = StateField.define<Tooltip | null>({
  create: () => null,
  update(_, tr) {
    const sel = tr.state.selection.main;
    return sel.empty ? problemTooltip(tr.state.field(problemField).placed, sel.head) : null;
  },
  provide: (f) => showTooltip.from(f),
});

const hoverProblems = hoverTooltip((view, pos) => problemTooltip(view.state.field(problemField).placed, pos));

/** Tab indents with two spaces (YAML allows no tabs). */
const indentTwo = keymap.of([
  {
    key: "Tab",
    run: (view) => {
      view.dispatch(view.state.replaceSelection("  "), { scrollIntoView: true, userEvent: "input" });
      return true;
    },
  },
]);

/**
 * Editing a workflow or pipeline in the YAML view (mockup design/mockups/e9-yaml.html).
 * The text goes to onChange after a typing pause (one undo step of the file, shared with
 * the canvas); undo and redo use the history of the file, not that of CodeMirror.
 */
export function YamlEditor(props: {
  text: string;
  /** The text is a Markdown document (E17): its highlighting, no YAML rules. */
  markdown?: boolean;
  problems: Problem[];
  /** A line to show and put the cursor on; seq changes with every request. */
  focus?: { line: number; seq: number; from?: number; to?: number } | null;
  onChange: (text: string) => void;
  /** Whether typed text waits for the typing pause (for "Checking…"). */
  onPending: (pending: boolean) => void;
  onUndo: () => void;
  onRedo: () => void;
  /** The line and column of the cursor (status bar). */
  onCursor?: (line: number, column: number) => void;
  /** Gives the file view a function that hands over typed text at once, e.g. before saving. */
  registerFlush: (flush: (() => void) | null) => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latest = useRef(props);
  latest.current = props;

  useEffect(() => {
    const flush = () => {
      if (!timer.current) return;
      clearTimeout(timer.current);
      timer.current = null;
      latest.current.onPending(false);
      const v = view.current;
      if (v) latest.current.onChange(v.state.doc.toString());
    };
    // Undo while typed text waits: back to the text of the last step.
    const revert = () => {
      if (!timer.current) return false;
      clearTimeout(timer.current);
      timer.current = null;
      latest.current.onPending(false);
      const v = view.current!;
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: latest.current.text } });
      return true;
    };
    const v = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: props.text,
        extensions: [
          problemField, // its gutter with the marks comes before the line numbers (mockup E9)
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightActiveLine(),
          drawSelection(),
          highlighter(props.markdown ? highlightMarkdownLine : highlightLine),
          cursorTooltip,
          hoverProblems,
          search({ top: true }),
          indentTwo,
          keymap.of([
            { key: "Mod-z", run: () => revert() || (latest.current.onUndo(), true), preventDefault: true },
            { key: "Mod-y", run: () => (flush(), latest.current.onRedo(), true), preventDefault: true },
            { key: "Mod-Shift-z", run: () => (flush(), latest.current.onRedo(), true), preventDefault: true },
            ...searchKeymap,
            ...defaultKeymap,
          ]),
          EditorView.contentAttributes.of({ "aria-label": props.markdown ? "Markdown" : "YAML", spellcheck: "false" }),
          EditorView.updateListener.of((u) => {
            if (u.selectionSet || u.docChanged) {
              const head = u.state.selection.main.head;
              const line = u.state.doc.lineAt(head);
              latest.current.onCursor?.(line.number, head - line.from + 1);
            }
            // Only typing (and search and replace) counts; the text set from outside does not.
            if (!u.docChanged || !u.transactions.some((t) => t.annotation(Transaction.userEvent) !== undefined)) return;
            if (!timer.current) latest.current.onPending(true);
            else clearTimeout(timer.current);
            timer.current = setTimeout(flush, applyDelay);
          }),
          EditorView.domEventHandlers({ blur: () => flush() }),
        ],
      }),
    });
    view.current = v;
    props.registerFlush(flush);
    return () => {
      flush();
      latest.current.registerFlush(null);
      v.destroy();
      view.current = null;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // The text changed elsewhere (undo, redo, a reload): shown unless typed text waits.
  useEffect(() => {
    const v = view.current;
    if (!v || timer.current || v.state.doc.toString() === props.text) return;
    const head = Math.min(v.state.selection.main.head, props.text.length);
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: props.text }, selection: { anchor: head } });
  }, [props.text]);

  useEffect(() => {
    view.current?.dispatch({ effects: setProblems.of(props.problems) });
  }, [props.problems, props.text]);

  useEffect(() => {
    const v = view.current;
    if (!v || !props.focus) return;
    const line = v.state.doc.line(Math.min(Math.max(props.focus.line, 1), v.state.doc.lines));
    const indent = line.text.length - line.text.trimStart().length;
    // A range of the line is selected, e.g. the value of an inserted field (E18b).
    const { from, to } = props.focus;
    const selection =
      from !== undefined ? { anchor: line.from + Math.min(from, line.length), head: line.from + Math.min(to ?? from, line.length) } : { anchor: line.from + indent };
    v.dispatch({ selection, effects: EditorView.scrollIntoView(line.from, { y: "center" }) });
    v.focus();
  }, [props.focus]);

  return <div ref={host} data-testid="yaml-editor" className="runnel-yaml min-h-0 flex-1 overflow-hidden" />;
}
