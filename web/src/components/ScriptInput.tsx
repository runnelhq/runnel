import { useEffect, useRef } from "react";
import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  closeCompletion,
  completionKeymap,
  completionStatus,
  type Completion,
  type CompletionContext,
  type CompletionResult,
} from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { javascript, javascriptLanguage } from "@codemirror/lang-javascript";
import { bracketMatching, indentOnInput, syntaxHighlighting, syntaxTree } from "@codemirror/language";
import { EditorState, StateEffect, StateField, type Range } from "@codemirror/state";
import { Decoration, EditorView, keymap, lineNumbers, type DecorationSet } from "@codemirror/view";
import { classHighlighter } from "@lezer/highlight";
import { fetchTree, type Entry } from "../files";
import { applyDelay } from "./PropertiesPanel";

/** What the code of core.script sees (ADR 0028): the names offered as suggestions. */
export interface ScriptContext {
  /** The code gets record (the script of the transform). */
  record: boolean;
  /** Fields of the incoming records, if known from the schema of the pipeline. */
  fields: { name: string; type?: string }[] | null;
  /** The keys configured in params of the step. */
  params: string[];
}

/** A problem of the code at a line, e.g. a syntax error reported by the check. */
export interface ScriptProblem {
  line: number;
  message: string;
}

const kindLetter: Record<string, string> = {
  root: "C",
  field: "R",
  param: "P",
  function: "ƒ",
  method: "ƒ",
  file: "F",
  keyword: "K",
  variable: "V",
  constant: "V",
  class: "C",
};

const consoleMethods: [string, string][] = [
  ["log", "Writes a line to the run log (info)"],
  ["info", "Writes a line to the run log (info)"],
  ["warn", "Writes a warning to the run log"],
  ["error", "Writes an error to the run log"],
];

const identifier = /^[A-Za-z_$][\w$]*$/;

/** Files under scripts/ of the project, for require('./scripts/…'); read again after a few seconds. */
let scriptFiles: { at: number; list: Promise<string[]> } | null = null;
function projectScripts(): Promise<string[]> {
  if (!scriptFiles || Date.now() - scriptFiles.at > 5000) {
    const collect = (e: Entry): string[] =>
      e.type === "file" ? (/\.js$/i.test(e.name) ? [e.path] : []) : (e.children ?? []).flatMap(collect);
    const list = fetchTree()
      .then((root) => {
        const dir = (root.children ?? []).find((c) => c.type === "dir" && c.name === "scripts");
        return dir ? collect(dir) : [];
      })
      .catch(() => []);
    scriptFiles = { at: Date.now(), list };
  }
  return scriptFiles.list;
}

/** Suggestions for record, params, console and require, added to those of the JavaScript mode (keywords, local names). */
function completions(ctx: { current: ScriptContext }) {
  return (c: CompletionContext): CompletionResult | null | Promise<CompletionResult | null> => {
    const node = syntaxTree(c.state).resolveInner(c.pos, -1);
    const line = c.state.doc.lineAt(c.pos);
    const before = line.text.slice(0, c.pos - line.from);
    // require('./scripts/…'): the files of the project (the string may not be closed yet).
    const req = /\brequire\(\s*(['"])([^'"]*)$/.exec(before);
    if (req) {
      return projectScripts().then((files) => ({
        from: c.pos - req[2].length,
        options: files.map((f) => ({ label: `./${f}`, type: "file", detail: "module" })),
        validFor: /^[^'"]*$/,
      }));
    }
    if (/String|Comment|Template/.test(node.name)) return null;

    const member = /\b(record|params|console)\s*(\?)?\.\s*([\w$]*)$/.exec(before);
    if (member) {
      const [, root, , word] = member;
      const from = c.pos - word.length;
      let options: Completion[] = [];
      if (root === "console") {
        options = consoleMethods.map(([name, info], i) => ({ label: name, type: "method", detail: "console", info, boost: -i }));
      } else if (root === "params") {
        options = ctx.current.params.map((name) => property(name, "param", "params"));
      } else if (ctx.current.record) {
        options = (ctx.current.fields ?? []).map((f) => property(f.name, "field", f.type ?? "field"));
      }
      return options.length > 0 ? { from, options, validFor: /^[\w$]*$/ } : null;
    }

    const word = c.matchBefore(/[\w$]+/);
    if (!word && !c.explicit) return null;
    if (word && /[\w$.]/.test(c.state.sliceDoc(word.from - 1, word.from))) return null;
    const roots: Completion[] = [
      ...(ctx.current.record ? [{ label: "record", type: "root", detail: "fields of the record", info: "The incoming record: record.NAME" }] : []),
      { label: "params", type: "root", detail: "values", info: "The values of params of this step: params.NAME" },
      { label: "console", type: "root", detail: "run log", info: "console.log, info, warn and error write to the run log" },
      { label: "require", type: "function", detail: "module", info: "Loads a module from scripts/ of the project, e.g. require('./scripts/util.js')" },
    ];
    return { from: word?.from ?? c.pos, options: roots.map((o) => ({ ...o, boost: 1 })), validFor: /^[\w$]*$/ };
  };
}

/** A name after "record." or "params."; one that is no identifier becomes ["name"]. */
function property(name: string, type: string, detail: string): Completion {
  if (identifier.test(name)) return { label: name, type, detail };
  return {
    label: name,
    type,
    detail,
    apply: (view, _c, from, to) => {
      // "record.a b" → record["a b"]; after "?." the dot stays: record?.["a b"].
      const optional = view.state.sliceDoc(from - 2, from) === "?.";
      const start = optional ? from : from - 1;
      const insert = `["${name.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"]`;
      view.dispatch({ changes: { from: start, to, insert }, selection: { anchor: start + insert.length }, userEvent: "input.complete" });
    },
  };
}

/**
 * Escape closes a visible list of suggestions; otherwise CodeMirror handles it, so that
 * Esc and then Tab leave the field (a pending, invisible suggestion would take the Esc).
 */
const escape = { key: "Escape", run: (view: EditorView) => completionStatus(view.state) === "active" && closeCompletion(view) };

function el(tag: string, cls: string, text?: string) {
  const e = document.createElement(tag);
  e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

/** Problems at their line: wavy underline of the line and the message on hover. */
const setProblems = StateEffect.define<ScriptProblem[]>();
const problemField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(value, tr) {
    for (const e of tr.effects) {
      if (e.is(setProblems)) {
        const marks: Range<Decoration>[] = [];
        for (const p of e.value) {
          if (p.line < 1 || p.line > tr.state.doc.lines) continue;
          const line = tr.state.doc.line(p.line);
          const from = line.from + (line.text.length - line.text.trimStart().length);
          const to = line.from + line.text.trimEnd().length;
          marks.push(Decoration.line({ class: "rs-error-line" }).range(line.from));
          if (to > from) marks.push(Decoration.mark({ class: "rx-error", attributes: { title: p.message } }).range(from, to));
        }
        return Decoration.set(marks, true);
      }
    }
    // Typing moves the marks with the text until the next check.
    return tr.docChanged ? value.map(tr.changes) : value;
  },
  provide: (f) => EditorView.decorations.from(f),
});

/**
 * A field with JavaScript for core.script (roadmap S73c, ADR 0028): highlighting,
 * suggestions for record, params, console and require, line numbers. It grows with the
 * text up to a maximum height and then scrolls. Like the other fields of the panel, it
 * applies its value after a pause in typing and when it is left; Ctrl+Z inside undoes
 * typing not yet applied, applied changes are steps of the file. Tab indents; Esc and then
 * Tab (or Ctrl+M to switch) moves the focus on, as CodeMirror provides.
 */
export function ScriptInput(props: {
  /** id of the label (aria-labelledby). */
  labelId: string;
  value: string;
  invalid: boolean;
  problems: ScriptProblem[];
  ctx: ScriptContext;
  onCommit: (value: string) => void;
}) {
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
          lineNumbers(),
          javascript(),
          javascriptLanguage.data.of({ autocomplete: completions(ctx) }),
          syntaxHighlighting(classHighlighter),
          bracketMatching(),
          closeBrackets(),
          indentOnInput(),
          autocompletion({
            icons: false,
            defaultKeymap: false, // its Escape is replaced below

            addToOptions: [{ render: (c) => el("span", "rx-kind", kindLetter[c.type ?? ""] ?? "·"), position: 20 }],
            tooltipClass: () => "rx-complete",
          }),
          keymap.of([
            ...closeBracketsKeymap,
            escape,
            ...completionKeymap.filter((b) => b.key !== "Escape"),
            indentWithTab,
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          problemField,
          EditorView.contentAttributes.of({ "aria-labelledby": props.labelId, "aria-multiline": "true", spellcheck: "false" }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged && u.transactions.some((t) => t.isUserEvent("input") || t.isUserEvent("delete") || t.isUserEvent("undo") || t.isUserEvent("redo"))) {
              clearTimeout(timer.current ?? undefined);
              timer.current = setTimeout(commit, applyDelay);
            }
          }),
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
    // The editor is created once; value and problems follow in the effects below.
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

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

  return <div ref={host} data-invalid={props.invalid || undefined} className="runnel-expr runnel-script" />;
}
