import { useEffect, useRef } from "react";
import { highlightLine, type TokenKind } from "../yaml";

const tokenClass: Record<TokenKind, string> = {
  comment: "italic text-muted-foreground",
  key: "font-medium text-foreground",
  punct: "text-muted-foreground",
  value: "text-(--code-string)",
  // ${…} and {{ … }} in the expression colour (decision for mockup E4).
  var: "rounded-[3px] bg-expression-soft text-expression",
  text: "",
};

/** Read-only file view with line numbers; YAML files are highlighted. focusLine is scrolled into view and marked. */
export function CodeView(props: { text: string; yaml: boolean; focusLine?: number | null }) {
  const lines = props.text.replace(/\r\n?/g, "\n").replace(/\n$/, "").split("\n");
  const focusRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    focusRef.current?.scrollIntoView({ block: "center" });
  }, [props.focusLine]);
  return (
    <div
      data-testid="code"
      className="flex-1 overflow-auto pt-2 pb-6 font-mono text-sm leading-5"
      role="region"
      aria-label="File content"
    >
      {lines.map((line, i) => (
        <div
          key={i}
          ref={i + 1 === props.focusLine ? focusRef : undefined}
          data-focus={i + 1 === props.focusLine || undefined}
          className={"grid grid-cols-[56px_1fr] hover:bg-muted" + (i + 1 === props.focusLine ? " bg-accent" : "")}
        >
          <span className="pr-4 text-right text-(--code-gutter) tabular-nums select-none">{i + 1}</span>
          <span className="pr-4 whitespace-pre">
            {props.yaml
              ? highlightLine(line).map((t, j) =>
                  t.kind === "text" ? (
                    t.text
                  ) : (
                    <span key={j} className={tokenClass[t.kind]} data-token={t.kind}>
                      {t.text}
                    </span>
                  ),
                )
              : line}
            {line === "" && " "}
          </span>
        </div>
      ))}
    </div>
  );
}
