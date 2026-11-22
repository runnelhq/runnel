import { useLayoutEffect, useMemo, useRef, useState, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { apiBase } from "../api";
import { kindOf } from "../files";
import { renderMarkdown } from "../markdown";
import { KindIcon } from "./Icons";

/** URL of a file of the project in the file API (images of a document). */
const fileURL = (path: string) => `${apiBase}/files/${path.split("/").map(encodeURIComponent).join("/")}`;

/**
 * The formatted view of a Markdown document (E17, mockup design/mockups/e17-markdown.html,
 * states open and links). Links to files of the project show the icon of their kind and
 * open the file in the editor; HTML in the text is shown as text.
 */
export function MarkdownView(props: { text: string; path: string; files: Set<string>; onOpen: (path: string) => void }) {
  const { text, path, files } = props;
  const html = useMemo(() => renderMarkdown(text, { path, files, fileURL }), [text, path, files]);
  const host = useRef<HTMLDivElement>(null);
  // Icons of the links to files, drawn by React into the spans of the rendered HTML.
  const [icons, setIcons] = useState<HTMLElement[]>([]);
  useLayoutEffect(() => {
    setIcons([...(host.current?.querySelectorAll<HTMLElement>("[data-icon]") ?? [])]);
  }, [html]);

  const onClick = (e: MouseEvent) => {
    const a = (e.target as HTMLElement).closest("a");
    if (!a) return;
    if (a.dataset.file) {
      e.preventDefault();
      props.onOpen(a.dataset.file);
    } else if (a.dataset.missing !== undefined) e.preventDefault();
  };

  return (
    // The click handler is here, not at the rendered HTML: clicks on the icons (portals) reach it as well.
    <div className="min-h-0 flex-1 overflow-auto bg-background px-10 pt-7 pb-12" data-testid="markdown" onClick={onClick}>
      <div ref={host} className="runnel-md" dangerouslySetInnerHTML={{ __html: html }} />
      {icons.map((el) => createPortal(<KindIcon kind={kindOf(el.dataset.icon ?? "")} width={13} height={13} />, el, el.dataset.icon))}
    </div>
  );
}
