import { useEffect, useRef } from "react";
import {
  autocompletion,
  closeCompletion,
  completionKeymap,
  completionStatus,
  startCompletion,
  type Completion,
  type CompletionContext,
} from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { EditorState, StateEffect, StateField, type Extension, type Range } from "@codemirror/state";
import {
  Decoration,
  EditorView,
  hoverTooltip,
  keymap,
  placeholder as placeholderExt,
  showTooltip,
  ViewPlugin,
  type DecorationSet,
  type Tooltip,
  type ViewUpdate,
} from "@codemirror/view";
import { scan, suggest, tokens, type ExprContext, type Located, type TokenKind } from "../expressions";
import { applyDelay } from "./PropertiesPanel";

/** Classes of the tokens (index.css, section "Expression editor"). */
const tokenClass: Record<TokenKind, string> = {
  expr: "rx-expr",
  brace: "rx-brace",
  var: "rx-var",
  root: "rx-root",
  fn: "rx-fn",
  string: "rx-string",
  number: "rx-number",
};

const kindLetter: Record<string, string> = { param: "P", variable: "V", field: "R", function: "ƒ", root: "C", pattern: "“" };

/** Highlights ${…} and {{ … }} and the parts of expressions. */
const highlight = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;
    constructor(view: EditorView) {
      this.decorations = this.build(view);
    }
    update(u: ViewUpdate) {
      if (u.docChanged) this.decorations = this.build(u.view);
    }
    build(view: EditorView) {
      const marks: Range<Decoration>[] = tokens(view.state.doc.toString())
        .filter((t) => t.to > t.from)
        .map((t) => Decoration.mark({ class: tokenClass[t.kind] }).range(t.from, t.to));
      return Decoration.set(marks, true);
    }
  },
  { decorations: (v) => v.decorations },
);

/** Problems located in the value: wavy underline and the message on hover. */
const setProblems = StateEffect.define<Located[]>();
const problemField = StateField.define<{ list: Located[]; deco: DecorationSet }>({
  create: () => ({ list: [], deco: Decoration.none }),
  update(value, tr) {
    for (const e of tr.effects) {
      if (e.is(setProblems)) {
        const len = tr.state.doc.length;
        const ranges = e.value
          .filter((p) => p.from < p.to && p.to <= len)
          .map((p) => Decoration.mark({ class: "rx-error", attributes: { title: p.message } }).range(p.from, p.to));
        return { list: e.value, deco: Decoration.set(ranges, true) };
      }
    }
    // Typing changes the value; the marks follow it until the next check.
    return tr.docChanged ? { list: value.list, deco: value.deco.map(tr.changes) } : value;
  },
  provide: (f) => EditorView.decorations.from(f, (v) => v.deco),
});

function el(tag: string, cls: string, text?: string) {
  const e = document.createElement(tag);
  e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

/** The extensions that need the context of the field (reference, parameters, actions). */
function contextual(ctx: { current: ExprContext }): Extension {
  const complete = (c: CompletionContext) => {
    const s = suggest(c.state.doc.toString(), c.pos, ctx.current, c.explicit);
    if (!s || s.options.length === 0) return null;
    const options: Completion[] = s.options.map((o, i) => ({
      label: o.label,
      apply: o.apply,
      detail: o.detail,
      info: o.info,
      type: o.kind,
      boost: -i, // keep the order of the reference
    }));
    return { from: s.from, options, validFor: /^[\w:]*$/ };
  };

  // Signature help while the cursor is in the arguments of a function.
  const signature = StateField.define<Tooltip | null>({
    create: () => null,
    update(_, tr) {
      const pos = tr.state.selection.main.head;
      const { call } = scan(tr.state.doc.toString(), pos);
      const doc = call && ctx.current.ref.functions.find((f) => f.name === call.name);
      if (!call || !doc?.signature) return null;
      const arg = call.arg;
      // Above the line, so that the suggestions below stay visible.
      return {
        pos,
        above: true,
        create: () => {
          const dom = el("div", "rx-signature");
          const sig = el("div", "rx-signature-head");
          const args = doc.signature!.slice(doc.name.length + 1, -1).split(", ");
          sig.append(doc.name + "(");
          args.forEach((a, i) => {
            if (i > 0) sig.append(", ");
            sig.append(el("span", i === arg ? "rx-signature-current" : "", a));
          });
          sig.append(") → " + doc.type);
          dom.append(sig, el("div", "rx-signature-text", doc.description));
          return { dom };
        },
      };
    },
    provide: (f) => showTooltip.from(f),
  });

  // Pointing at a variable shows its value, at a function its signature.
  const hover = hoverTooltip((view, pos) => {
    const text = view.state.doc.toString();
    const t = tokens(text).find((x) => (x.kind === "var" || x.kind === "fn") && x.from <= pos && pos <= x.to);
    if (!t) return null;
    const word = text.slice(t.from, t.to);
    let head: string, body: string;
    if (t.kind === "fn") {
      const f = ctx.current.ref.functions.find((d) => d.name === word);
      if (!f) return null;
      head = f.signature ?? f.name;
      body = f.description;
    } else {
      const name = word.replace(/^\$\{|\}$/g, "");
      const prm = ctx.current.params.find((p) => p.name === name);
      const v = ctx.current.ref.variables.find((x) => x.name === name);
      if (prm) {
        head = `${word} – parameter`;
        body = prm.default ? `Default ${prm.default}` : "Given when the workflow starts";
      } else if (v) {
        head = `${word} = ${v.value}`;
        body = `Variable · ${v.source}`;
      } else if (name.startsWith("env:")) {
        head = word;
        body = "Environment variable of the operating system";
      } else {
        return null;
      }
    }
    return {
      pos: t.from,
      end: t.to,
      above: true,
      create: () => {
        const dom = el("div", "rx-hover");
        dom.append(el("div", "rx-hover-head", head), el("div", "rx-hover-text", body));
        return { dom };
      },
    };
  });

  return [
    autocompletion({
      override: [complete],
      icons: false,
      addToOptions: [{ render: (c) => el("span", "rx-kind", kindLetter[c.type ?? ""] ?? ""), position: 20 }],
      tooltipClass: () => "rx-complete",
    }),
    signature,
    hover,
  ];
}

/** Typing "{{" adds the closing "}}", typing "${" the closing "}"; suggestions open. */
const autoClose = EditorView.inputHandler.of((view, from, to, text) => {
  if (text !== "{" || from !== to) return false;
  const before = view.state.doc.sliceString(Math.max(0, from - 1), from);
  const after = view.state.doc.sliceString(from, from + 2);
  let insert: string, cursor: number;
  if (before === "{" && !after.startsWith("}")) {
    insert = "{  }}";
    cursor = from + 2;
  } else if (before === "$" && !after.startsWith("}")) {
    insert = "{}";
    cursor = from + 1;
  } else {
    return false;
  }
  view.dispatch({ changes: { from, insert }, selection: { anchor: cursor }, userEvent: "input.type" });
  startCompletion(view);
  return true;
});

/** A single-line field turns line breaks (e.g. pasted) into spaces. */
const singleLine = EditorState.transactionFilter.of((tr) => {
  if (!tr.docChanged || tr.newDoc.lines === 1) return tr;
  const text = tr.newDoc.toString().replace(/\r?\n/g, " ");
  return [tr, { changes: { from: 0, to: tr.newDoc.length, insert: text }, sequential: true }];
});

export interface ExpressionInputProps {
  /** id of the label (aria-labelledby). */
  labelId: string;
  value: string;
  invalid: boolean;
  placeholder?: string;
  multiline?: boolean;
  /** Problems of this field, located in the value. */
  problems: Located[];
  ctx: ExprContext;
  onCommit: (value: string) => void;
  testId?: string;
}

/**
 * A text field with ${…} variables and {{ … }} expressions (roadmap S35a, mockup
 * design/mockups/s35-expressions.html): highlighting, suggestions after "{{", "${" and
 * ".", or with Ctrl+Space, signature help, problems at their position. Like the other
 * fields of the panel, it applies its value after a pause in typing and when it is left.
 */
export function ExpressionInput(props: ExpressionInputProps) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const ctx = useRef(props.ctx);
  ctx.current = props.ctx;
  const commitRef = useRef(props.onCommit);
  commitRef.current = props.onCommit;
  const committed = useRef(props.value);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const commit = () => {
      clearTimeout(timer.current ?? undefined);
      timer.current = null;
      const v = view.current?.state.doc.toString() ?? committed.current;
      if (v === committed.current) return;
      committed.current = v;
      commitRef.current(v);
    };
    const v = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: props.value,
        extensions: [
          history(),
          keymap.of([
            ...completionKeymap,
            ...(props.multiline
              ? []
              : [
                  {
                    key: "Enter",
                    run: (view: EditorView) => {
                      if (completionStatus(view.state)) return false;
                      commit();
                      return true;
                    },
                  },
                ]),
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          props.multiline ? [] : singleLine,
          EditorView.lineWrapping,
          autoClose,
          highlight,
          problemField,
          contextual(ctx),
          props.placeholder ? placeholderExt(props.placeholder) : [],
          EditorView.contentAttributes.of({
            "aria-labelledby": props.labelId,
            "aria-multiline": props.multiline ? "true" : "false",
            spellcheck: "false",
          }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged && u.transactions.some((t) => t.isUserEvent("input") || t.isUserEvent("delete") || t.isUserEvent("undo") || t.isUserEvent("redo"))) {
              clearTimeout(timer.current ?? undefined);
              timer.current = setTimeout(commit, applyDelay);
            }
          }),
          // Applied on the DOM event: CodeMirror reports a lost focus a little later, after
          // a click on another control (e.g. a condition button) has already changed the file.
          EditorView.domEventHandlers({
            blur: (_, view) => {
              closeCompletion(view);
              commit();
            },
          }),
        ],
      }),
    });
    view.current = v;
    return () => {
      // A change still waiting for the pause is applied when the field goes away.
      if (timer.current) commit();
      v.destroy();
      view.current = null;
    };
    // The editor is created once; value, problems and context follow in the effects below.
  }, [props.multiline]);

  // Changes from outside (e.g. after undo or a reload) replace the text unless it is being edited.
  useEffect(() => {
    committed.current = props.value;
    const v = view.current;
    if (!v || v.hasFocus || v.state.doc.toString() === props.value) return;
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: props.value } });
  }, [props.value]);

  useEffect(() => {
    view.current?.dispatch({ effects: setProblems.of(props.problems) });
  }, [props.problems]);

  return (
    <div
      ref={host}
      data-testid={props.testId}
      data-invalid={props.invalid || undefined}
      className={"runnel-expr" + (props.multiline ? " runnel-expr-area" : "")}
    />
  );
}
