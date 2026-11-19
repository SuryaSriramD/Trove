import { useEffect, useRef } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import { Extension, type Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import type { RichText } from "../../../contracts/generated/domain";
import { TextColor, TextHighlight, ColorControls, preserveStyledPaste } from "./ColorMarks";
import { richNodes, richText, type NodeJSON } from "./codec";
import { LinkControl } from "./LinkControl";

function inlineDocument(value: RichText) {
  return { type: "doc", content: [{ type: "paragraph", content: richNodes(value) }] };
}
// Paragraphs pasted into this bounded editor become explicit canonical breaks.
// Marks and links remain attached to their original runs.
function inlineValue(doc: NodeJSON): RichText {
  return (doc.content ?? []).flatMap((paragraph, i) => [
    ...(i ? [{ text: "\n" }] : []), ...richText(paragraph.content),
  ]);
}
const comparable = (value: RichText) => JSON.stringify(richText(richNodes(value)));

export function InlineRichEditor({ value, onChange, label, owner, readOnly = false }: {
  value: RichText;
  onChange: (value: RichText) => void;
  label: string;
  owner: Editor;
  readOnly?: boolean;
}) {
  const latest = useRef({ value, onChange, owner, readOnly });
  latest.current = { value, onChange, owner, readOnly };
  const composing = useRef(false);
  const syncRef = useRef<() => void>(() => {});
  const editor = useEditor({
    shouldRerenderOnTransaction: true,
    extensions: [
      TextColor, TextHighlight,
      StarterKit.configure({
        heading: false, blockquote: false, bulletList: false, orderedList: false,
        listItem: false, codeBlock: false, horizontalRule: false, trailingNode: false,
        undoRedo: false,
        link: { openOnClick: false, protocols: ["https", "http", "mailto", "trove"] },
      }),
      Extension.create({
        name: "outerPageHistory",
        addKeyboardShortcuts() {
          return {
            Enter: () => this.editor.commands.setHardBreak(),
            "Mod-z": () => { latest.current.owner.commands.undo(); return true; },
            "Mod-Shift-z": () => { latest.current.owner.commands.redo(); return true; },
            "Mod-y": () => { latest.current.owner.commands.redo(); return true; },
            "Mod-s": () => { latest.current.owner.commands.keyboardShortcut("Mod-s"); return true; },
          };
        },
      }),
    ],
    content: inlineDocument(value),
    editable: !readOnly,
    editorProps: {
      attributes: { role: "textbox", "aria-label": label, "aria-multiline": "true", dir: "auto", class: "inline-rich-content" },
      handlePaste: preserveStyledPaste(()=>latest.current.owner),
      handleDOMEvents: {
        compositionstart: () => { composing.current = true; return false; },
        compositionend: () => {
          composing.current = false;
          requestAnimationFrame(() => syncRef.current());
          return false;
        },
      },
    },
    onUpdate: ({ editor }) => {
      if (!latest.current.readOnly) latest.current.onChange(inlineValue(editor.getJSON()));
    },
  });
  syncRef.current = () => {
    if (!editor || editor.isDestroyed || composing.current) return;
    if (comparable(inlineValue(editor.getJSON())) === comparable(latest.current.value)) return;
    const { from, to } = editor.state.selection;
    editor.commands.setContent(inlineDocument(latest.current.value), { emitUpdate: false });
    const end = Math.max(1, editor.state.doc.content.size - 1);
    editor.commands.setTextSelection({ from: Math.min(from, end), to: Math.min(to, end) });
  };
  useEffect(() => { syncRef.current(); }, [editor, value]);
  useEffect(() => { editor?.setEditable(!readOnly, false); }, [editor, readOnly]);
  if (!editor) return null;
  return <div className="inline-rich-editor" data-inline-editor="">
    {!readOnly && <div className="inline-rich-toolbar" role="toolbar" aria-label={`${label} formatting`}>
      <ColorControls editor={editor} label={label}/>
      {(["bold", "italic", "underline", "strike", "code"] as const).map(mark =>
        <button type="button" key={mark} aria-label={`${label} ${mark}`} aria-pressed={editor.isActive(mark)}
          onMouseDown={event => event.preventDefault()}
          onClick={() => editor.chain().focus().toggleMark(mark).run()}>{mark}</button>)}
      <LinkControl editor={editor} label={`${label} link`} allowInternal removeOnEmpty>Link</LinkControl>
    </div>}
    <EditorContent editor={editor} />
  </div>;
}
