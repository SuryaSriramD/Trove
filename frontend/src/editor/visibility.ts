import { Extension, type Editor } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { blockVisibility, presentationBlocks } from "./presentation";

const presentationKey = new PluginKey<ReadonlySet<string>>("troveBlockPresentation");
export function clearTemporaryReveal(editor: Editor) {
  editor.view.dispatch(editor.state.tr.setMeta(presentationKey, { clear: true }));
}
export function temporarilyExpanded(editor: Editor, blockId: string | null): boolean {
  if (!blockId) return false;
  const blocks = presentationBlocks(editor.getJSON().content ?? []);
  return blockVisibility(blocks, presentationKey.getState(editor.state) ?? new Set()).expanded.has(blockId);
}
export const BlockVisibility = Extension.create({
  name: "blockVisibility",
  addProseMirrorPlugins() {
    return [new Plugin<ReadonlySet<string>>({
      key: presentationKey,
      state: {
        init: () => new Set(),
        apply(tr, previous) {
          const action = tr.getMeta(presentationKey);
          if (action?.clear) return new Set();
          if (typeof action?.reveal === "string") return new Set([...previous, action.reveal]);
          return previous;
        },
      },
      props: {
        decorations(state) {
          const nodes = state.doc.content.content;
          const blocks = presentationBlocks(nodes.map(node => node.toJSON()));
          const { hidden, expanded } = blockVisibility(blocks, presentationKey.getState(state) ?? new Set());
          const decorations: Decoration[] = [];
          let position = 0;
          nodes.forEach((node, index) => {
            const key = blocks[index].key;
            if (hidden.has(key)) decorations.push(Decoration.node(position, position + node.nodeSize, { class: "trove-hidden-block", "aria-hidden": "true" }));
            else if (expanded.has(key)) decorations.push(Decoration.node(position, position + node.nodeSize, { "data-temporarily-expanded": "true" }));
            position += node.nodeSize;
          });
          return DecorationSet.create(state.doc, decorations);
        },
      },
      view(view) {
        const reveal = (event: Event) => {
          const blockId = (event as CustomEvent<{ blockId?: string }>).detail?.blockId;
          if (typeof blockId === "string") view.dispatch(view.state.tr.setMeta(presentationKey, { reveal: blockId }));
        };
        const copy = (event: ClipboardEvent) => {
          // Chromium can clear its native range around nested editable cells.
          // In that case the event targets the body, bypassing ProseMirror's
          // normal copy handler even though the outer canonical selection is
          // intact. Serialize that selection through the same clipboard API.
          if (event.defaultPrevented || document.activeElement !== view.dom || view.state.selection.empty || !event.clipboardData) return;
          if (event.target instanceof globalThis.Node && view.dom.contains(event.target)) return;
          const { dom, text } = view.serializeForClipboard(view.state.selection.content());
          event.clipboardData.setData("text/html", dom.innerHTML);
          event.clipboardData.setData("text/plain", text);
          event.preventDefault();
        };
        window.addEventListener("trove:reveal-block", reveal);
        document.addEventListener("copy", copy);
        return { destroy() { window.removeEventListener("trove:reveal-block", reveal); document.removeEventListener("copy", copy); } };
      },
    })];
  },
});
