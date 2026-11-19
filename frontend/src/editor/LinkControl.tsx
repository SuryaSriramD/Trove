import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import type { Editor } from "@tiptap/core";
import type { SelectionBookmark, Transaction } from "@tiptap/pm/state";
import "./LinkControl.css";

export function LinkControl({ editor, label, children, allowInternal = false, removeOnEmpty = false }: {
  editor: Editor;
  label: string;
  children: ReactNode;
  allowInternal?: boolean;
  removeOnEmpty?: boolean;
}) {
  const [href, setHref] = useState<string | null>(null);
  const [error, setError] = useState("");
  const selection = useRef<SelectionBookmark | null>(null);
  const panelId = useId();
  const errorId = useId();
  useEffect(() => {
    const mapSelection = ({ transaction }: { transaction: Transaction }) => {
      selection.current = selection.current?.map(transaction.mapping) ?? null;
    };
    editor.on("transaction", mapSelection);
    return () => { editor.off("transaction", mapSelection); };
  }, [editor]);
  const restore = () => {
    const bookmark = selection.current;
    selection.current = null;
    setHref(null);
    setError("");
    if (!bookmark || editor.isDestroyed) return;
    editor.chain().focus().command(({ tr }) => {
      tr.setSelection(bookmark.resolve(tr.doc));
      return true;
    }).run();
  };
  const apply = () => {
    if (href === null || !selection.current || editor.isDestroyed || !editor.isEditable) return;
    const remove = removeOnEmpty && href === "";
    if (!remove && !(allowInternal ? /^(https?:|mailto:|trove:)/i : /^(https?:|mailto:)/i).test(href)) {
      setError(allowInternal ? "Use an http, https, mailto, or trove URL." : "Use an http, https, or mailto URL.");
      return;
    }
    const bookmark = selection.current;
    const chain = editor.chain().focus().command(({ tr }) => {
      tr.setSelection(bookmark.resolve(tr.doc));
      return true;
    });
    if (!(remove ? chain.unsetLink() : chain.setLink({ href })).run()) {
      setError("This URL cannot be used as a link.");
      return;
    }
    selection.current = null;
    setHref(null);
    setError("");
  };
  return <div className="link-control">
    <button type="button" aria-label={label} aria-expanded={href !== null} aria-controls={href !== null ? panelId : undefined}
      onMouseDown={event => event.preventDefault()} onClick={() => {
        if (href !== null) { restore(); return; }
        selection.current = editor.state.selection.getBookmark();
        setError("");
        setHref(editor.getAttributes("link").href ?? "");
      }}>{children}</button>
    {href !== null && <div id={panelId} className="link-editor" role="group" aria-label={`${label} editor`}
      onKeyDown={event => {
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); restore(); }
        else if (event.key === "Enter" && event.target instanceof HTMLInputElement && !event.nativeEvent.isComposing) { event.preventDefault(); event.stopPropagation(); apply(); }
      }}>
      <label>Link URL<input autoFocus value={href} aria-invalid={!!error} aria-describedby={error ? errorId : undefined}
        onChange={event => { setHref(event.target.value); setError(""); }} /></label>
      <button type="button" onClick={apply}>Apply link</button>
      <button type="button" onClick={restore}>Cancel link</button>
      {error && <span id={errorId} role="alert">{error}</span>}
    </div>}
  </div>;
}
