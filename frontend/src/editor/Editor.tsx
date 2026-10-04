import { useEffect, useRef, useState } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import { createTask as createCanonicalTask } from "../database/service";
import StarterKit from "@tiptap/starter-kit";
import { Extension, InputRule } from "@tiptap/core";
import { AdvancedPanel } from "../advanced/AdvancedPanel";
import { getLock, setLock, type PageLock } from "../advanced/service";
import { replaceDocument, safeWebURL } from "./advanced";
import "./advanced.css";
import { TextColor, TextHighlight, ColorControls, preserveStyledPaste } from "./ColorMarks";
import { BlockVisibility } from "./visibility";
import { dispatch, envelope } from "../api";
import { AllSelection, NodeSelection, Selection, TextSelection } from "@tiptap/pm/state";
import { GapCursor } from "@tiptap/pm/gapcursor";
import {
  batch,
  ids,
  expectedPage,
  pageGuards,
  attachFile,
  type WorkspaceSnapshot,
  type D,
} from "../api";
import {
  TroveText,
  TroveAtom,
  CanonicalIdentity,
  EditorContext,
} from "./nodes";
import {
  toDocument,
  orderedBlocks,
  fromDocument,
  mutations,
  blockMutationGuards,
  type NodeJSON,
  textKinds,
} from "./codec";
const commands = [
  ["paragraph", "Text", "Just start writing"],
  ["heading_1", "Heading 1", "A large section heading"],
  ["heading_2", "Heading 2", "A medium section heading"],
  ["heading_3", "Heading 3", "A small section heading"],
  ["heading_4", "Heading 4", "A compact subsection heading"],
  ["bulleted_list_item", "Bullet list", "Organize an idea"],
  ["numbered_list_item", "Numbered list", "Step by step"],
  ["todo", "To-do", "One task, everywhere"],
  ["toggle", "Toggle", "A collapsible thought"],
  ["quote", "Quote", "Give a thought some space"],
  ["callout", "Callout", "Make something stand out"],
  ["divider", "Divider", "A quiet separation"],
  ["code", "Code", "Syntax-highlighted source"],
  ["mermaid", "Mermaid diagram", "A diagram from text"],
  ["equation", "Equation", "Inline or display LaTeX"],
  ["table", "Table", "A simple table"],
  ["file", "Attachment", "Image, PDF, audio, video, or file"],
  ["page_mention", "Page link", "Connect your thinking"],
  ["tab_container", "Content tabs", "Group canonical content into tabs"],
  ["columns", "Columns", "Arrange reusable content side by side"],
  ["synced_block", "Reusable block", "Reference a canonical block"],
  ["bookmark", "Bookmark", "Keep a link and offline description"],
  ["embed", "Safe embed", "A sandboxed offline content card"],
  ["breadcrumb", "Breadcrumb", "Navigate the page hierarchy"],
  ["table_of_contents", "Table of contents", "Navigate headings"],
  ["date_mention", "Date mention", "A date with explicit timezone"],
  ["button", "Page button", "Review a saved workspace action"],
];
// Compare canonical values independently of JSON object-key order. Sorting keys
// alone is not a content edit, and server sort keys may be compacted after saves.
function comparable(value: unknown): string {
  return JSON.stringify(value, (_key, child) =>
    child && typeof child === "object" && !Array.isArray(child)
      ? Object.fromEntries(
          Object.keys(child)
            .sort()
            .map((key) => [key, child[key]]),
        )
      : child,
  );
}
function sameCanonicalBlock(a: D.Block, b: D.Block): boolean {
  return (
    a.id === b.id &&
    a.revision === b.revision &&
    a.parentBlockId === b.parentBlockId &&
    a.type === b.type &&
    a.schemaVersion === b.schemaVersion &&
    comparable(a.data) === comparable(b.data)
  );
}

export function PageEditor({
  page,
  blocks,
  workspace,
  onCommitted,
  onError,
  openPage,
  toggleTask,
  registerFlush,
  onAttach,
}: {
  page: D.Page;
  blocks: D.Block[];
  workspace: WorkspaceSnapshot;
  onCommitted: () => Promise<void>;
  onAttach?: () => Promise<void>;
  onError: (e: unknown) => void;
  openPage: (id: string) => void;
  toggleTask: (id: string) => void;
  registerFlush?: (flush: (() => Promise<boolean>) | null) => void;
}) {
  const [saveState, setSaveState] = useState("Saved");
  const [menu, setMenu] = useState(false);
  const [filter, setFilter] = useState("");
  const [find, setFind] = useState("");
  const [replacement, setReplacement] = useState("");
  const [lock, setPageLock] = useState<PageLock>({locked:false,revision:"0"});
  const lockRef = useRef<PageLock>({locked:false,revision:"0"});
  const lockChanging = useRef(false);
  const [changingLock, setChangingLock] = useState(false);
  const [findOpen, setFindOpen] = useState(false);
  const [conflict, setConflict] = useState(false);
  const latest = useRef({ page, blocks, workspace });
  // Keep props received during an in-flight save until the accepted baseline
  // and the live draft can be reconciled together after that save completes.
  const incoming = useRef({ page, blocks, workspace });
  incoming.current = { page, blocks, workspace };
  const conflictRef = useRef(false);
  const saving = useRef(false);
  const failed = useRef(false);
  const pending = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const editorRef = useRef<any>(null);
  const saveRef = useRef<() => Promise<void>>(async () => {});
  useEffect(() => {
    latest.current.workspace = workspace;
  }, [workspace]);
  const shortcuts = Extension.create({
    name: "troveShortcuts",
    addInputRules() {
      return [
        ["# ", "heading_1"],
        ["## ", "heading_2"],
        ["### ", "heading_3"],
        ["#### ", "heading_4"],
        ["- ", "bulleted_list_item"],
        ["* ", "bulleted_list_item"],
        ["1. ", "numbered_list_item"],
        ["> ", "quote"],
      ].map(
        ([prefix, kind]) =>
          new InputRule({
            find: new RegExp(
              "^" + prefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "$",
            ),
            handler: ({ state, range }) => {
              state.tr
                .delete(range.from, range.to)
                .setNodeMarkup(state.selection.$from.before(), undefined, {
                  ...state.selection.$from.parent.attrs,
                  kind,
                });
            },
          }),
      );
    },
    addKeyboardShortcuts() {
      const moveBlock = (direction: number) => {
        const e = editorRef.current; if (!e) return false;
        const positions: number[] = []; e.state.doc.forEach((_n:any,p:number) => positions.push(p));
        const from = e.state.selection.from;
        const index = positions.findIndex((p,i) => p <= from && (positions[i+1] === undefined || positions[i+1] > from));
        const pos = positions[index+direction]; if (pos === undefined) return false;
        e.view.dispatch(e.state.tr.setSelection(NodeSelection.create(e.state.doc,pos)).scrollIntoView()); return true;
      };
      const lineEdge = (end: boolean, extend = false) => {
        const e=editorRef.current;if(!e)return false;
        let selection=e.state.selection;
        // A click's DOM selection can precede ProseMirror's selectionchange
        // observer. Resolve the native caret before applying an immediate key.
        const native=window.getSelection();
        if(native?.anchorNode && native.focusNode && e.view.dom.contains(native.anchorNode) && e.view.dom.contains(native.focusNode)) {
          try { selection=TextSelection.create(e.state.doc,e.view.posAtDOM(native.anchorNode,native.anchorOffset),e.view.posAtDOM(native.focusNode,native.focusOffset)); } catch { /* Keep the accepted editor selection. */ }
        }
        if(selection.$head.parent.type.name!=="troveText")return false;
        const pos=end?selection.$head.end():selection.$head.start();
        e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc,extend?selection.anchor:pos,pos)).scrollIntoView());
        return true;
      };
      const leaveAllSelection = (end: boolean) => {
        const e = editorRef.current;
        if (!e || !(e.state.selection instanceof AllSelection)) return false;
        // Nested cell editors can leave the browser without a usable native
        // range. Collapse the canonical selection explicitly before typing or
        // pasting, rather than relying on a delayed selectionchange event.
        const edge = e.state.doc.resolve(end ? e.state.doc.content.size : 0);
        const adjacent = end ? e.state.doc.lastChild : e.state.doc.firstChild;
        const selection = adjacent?.isAtom ? new GapCursor(edge) : end ? Selection.atEnd(e.state.doc) : Selection.atStart(e.state.doc);
        e.view.dispatch(e.state.tr.setSelection(selection).scrollIntoView());
        return true;
      };
      return {
        ArrowLeft: () => leaveAllSelection(false),
        ArrowRight: () => leaveAllSelection(true),
        Home: () => lineEdge(false),
        End: () => lineEdge(true),
        "Shift-Home": () => lineEdge(false,true),
        "Shift-End": () => lineEdge(true,true),
        "Alt-ArrowUp": () => moveBlock(-1),
        "Alt-ArrowDown": () => moveBlock(1),
        Escape: () => {
          document
            .querySelector<HTMLButtonElement>('[aria-label="Save page"]')
            ?.focus();
          return true;
        },
        "Mod-s": () => {
          void saveRef.current();
          return true;
        },
        "Mod-f": () => {
          setFindOpen(true);
          return true;
        },
        Tab: () => {
          const e = editorRef.current;
          if (
            ![
              "bulleted_list_item",
              "numbered_list_item",
              "toggle",
              "todo",
            ].includes(e?.state.selection.$from.parent.attrs.kind)
          )
            return false;
          return (
            e?.commands.updateAttributes("troveText", {
              depth: Math.min(
                (e.state.selection.$from.parent.attrs.depth ?? 0) + 1,
                8,
              ),
            }) ?? false
          );
        },
        "Shift-Tab": () => {
          const e = editorRef.current;
          if (!e?.state.selection.$from.parent.attrs.depth) return false;
          return (
            e?.commands.updateAttributes("troveText", {
              depth: Math.max(
                (e.state.selection.$from.parent.attrs.depth ?? 0) - 1,
                0,
              ),
            }) ?? false
          );
        },
        "Mod-Shift-d": () => {
          const e = editorRef.current;
          const n = e.state.selection.$from.parent;
          const pos = e.state.selection.$from.after();
          e.commands.insertContentAt(pos, {
            ...n.toJSON(),
            attrs: { ...n.attrs, blockId: null, clientKey: null },
          });
          return true;
        },
      };
    },
  });
  const editor = useEditor({
    extensions: [
      TextColor, TextHighlight,
      StarterKit.configure({
        paragraph: false,
        heading: false,
        blockquote: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        codeBlock: false,
        horizontalRule: false,
        trailingNode: false,
        link: {
          openOnClick: false,
          protocols: ["https", "http", "mailto", "trove"],
        },
      }),
      TroveText,
      TroveAtom,
      CanonicalIdentity,
      BlockVisibility,
      shortcuts,
    ],
    content: blocks.length
      ? toDocument(blocks)
      : {
          type: "doc",
          content: [
            {
              type: "troveText",
              attrs: { kind: "paragraph", clientKey: crypto.randomUUID() },
            },
          ],
        },
    editorProps: {
      handlePaste: preserveStyledPaste(),
      handleClick: (_view, _position, event) => {
        const link = (
          event.target as Element | null
        )?.closest<HTMLAnchorElement>("a[href]");
        const href = link?.getAttribute("href") ?? "";
        const target = /^trove:\/\/page\/([0-9a-f-]{36})$/.exec(href);
        if (target) {
          event.preventDefault();
          openPage(target[1]);
          return true;
        }
        if (/^\/files\/[0-9a-f-]{36}$/.test(href)) {
          event.preventDefault();
          window.open(href, "_blank", "noopener,noreferrer");
          return true;
        }
        return false;
      },
      attributes: {
        class: "trove-editor",
        "aria-label": "Page body",
        role: "textbox",
        "aria-multiline": "true",
        spellcheck: "true",
        dir: "auto",
      },
      handleKeyDown: (_view, event) => {
        if (event.key === "/" && !event.metaKey && !event.ctrlKey) {
          setMenu(true);
          setFilter("");
        }
        if (event.key === "Escape") {
          setMenu(false);
          setFindOpen(false);
        }
        return false;
      },
      transformPastedHTML: (html) =>
        html.replace(/\s(?:blockId|clientKey|parentBlockId)="[^"]*"/gi, ""),
    },
    onUpdate: () => {
      void window.go?.main.App.SetEditing?.(true);
      setSaveState("Unsaved changes");
      pending.current = true;
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        void window.go?.main.App.SetEditing?.(false);
        void saveRef.current();
      }, 800);
    },
  });
  editorRef.current = editor;
  function acceptPageLock(next: PageLock) {
    // An older read must not undo a lock change that completed while it ran.
    if (BigInt(next.revision) < BigInt(lockRef.current.revision)) return;
    lockRef.current = next;
    setPageLock(next);
  }
  useEffect(() => {
    let active = true;
    void getLock(workspace,page.id).then(next=>{if(active)acceptPageLock(next);}).catch(onError);
    return () => { active = false; };
  }, [page.id,page.revision,workspace.snapshotCursor]);
  useEffect(() => {
    // Tiptap emits an update by default even though editability changes no
    // document content. That synthetic edit used to dirty a just-locked page.
    editor?.setEditable(!lock.locked, false);
    if (lock.locked && timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
      void window.go?.main.App.SetEditing?.(false);
    }
  }, [editor,lock.locked]);
  function reconcileIncoming() {
    if (!editor || editor.isDestroyed || saving.current) return;
    const next = incoming.current,
      previous = latest.current;
    if (
      next.page.id !== previous.page.id ||
      BigInt(next.page.revision) < BigInt(previous.page.revision)
    )
      return;
    const before = orderedBlocks(previous.blocks),
      after = orderedBlocks(next.blocks);
    const appendOnly =
      after.length >= before.length &&
      before.every((block, i) => sameCanonicalBlock(block, after[i]));
    const additions = appendOnly ? after.slice(before.length) : [];
    const dirty = pending.current || failed.current;
    const advanced =
      BigInt(next.page.revision) > BigInt(previous.page.revision);
    const queueSave = () => {
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => void saveRef.current(), 800);
    };
    if (!appendOnly && dirty) {
      // A changed/deleted/reordered existing block is not an append. Keep both
      // the original revision guards and the user's draft for conflict review.
      conflictRef.current = true;
      setConflict(true);
      setSaveState("Changes need review");
      return;
    }
    if (appendOnly && additions.length === 0) {
      latest.current = next;
      if (dirty && conflictRef.current && advanced) {
        conflictRef.current = false;
        setConflict(false);
        failed.current = false;
        pending.current = true;
        setSaveState("Unsaved changes");
        queueSave();
      }
      return;
    }
    const current = editor.state;
    let tr = current.tr;
    if (appendOnly && (before.length > 0 || dirty)) {
      const existing = new Set<string>();
      current.doc.descendants((node) => {
        if (node.attrs.blockId) existing.add(node.attrs.blockId);
      });
      const addedIDs = new Set(additions.map((block) => block.id));
      if (
        additions.some(
          (block) =>
            existing.has(block.id) ||
            (block.parentBlockId &&
              !existing.has(block.parentBlockId) &&
              !addedIDs.has(block.parentBlockId)),
        )
      ) {
        conflictRef.current = true;
        setConflict(true);
        setSaveState("Changes need review");
        return;
      }
      // Appending a transaction preserves the local draft, caret, and undo
      // history. Calculate nested depths using the complete canonical tree.
      const content = toDocument(after).content!.slice(before.length);
      tr = tr.insert(
        current.doc.content.size,
        content.map((node) => editor.schema.nodeFromJSON(node)),
      );
    } else {
      // Clean overlapping updates can replace the document. Re-anchor the
      // selection by canonical block identity instead of jumping to its end.
      const anchor = (position: number) => {
        const resolved = current.doc.resolve(position);
        for (let depth = resolved.depth; depth > 0; depth--) {
          const node = resolved.node(depth);
          if (node.attrs.blockId)
            return {
              id: node.attrs.blockId as string,
              offset: position - resolved.before(depth),
              fallback: position,
            };
        }
        return {
          id: resolved.nodeAfter?.attrs.blockId as string | undefined,
          offset: 0,
          fallback: position,
        };
      };
      const selection = current.selection;
      const start = anchor(selection.anchor),
        end = anchor(selection.head);
      const content = after.length
        ? toDocument(after).content!
        : [
            {
              type: "troveText",
              attrs: { kind: "paragraph", clientKey: crypto.randomUUID() },
            },
          ];
      tr = tr.replaceWith(
        0,
        current.doc.content.size,
        content.map((node) => editor.schema.nodeFromJSON(node)),
      );
      const locate = (saved: ReturnType<typeof anchor>) => {
        let result = Math.min(saved.fallback, tr.doc.content.size);
        tr.doc.descendants((node, position) => {
          if (saved.id && node.attrs.blockId === saved.id)
            result = Math.min(
              position + saved.offset,
              position + node.nodeSize - 1,
            );
        });
        return result;
      };
      const from = locate(start),
        to = locate(end);
      if (selection instanceof NodeSelection && tr.doc.nodeAt(from)?.isAtom)
        tr = tr.setSelection(NodeSelection.create(tr.doc, from));
      else
        tr = tr.setSelection(
          TextSelection.between(tr.doc.resolve(from), tr.doc.resolve(to)),
        );
    }
    // A server projection is not a new user edit and must never enter Undo.
    tr.setMeta("preventUpdate", true).setMeta("addToHistory", false);
    editor.view.dispatch(tr);
    latest.current = next;
    if (conflictRef.current) {
      conflictRef.current = false;
      setConflict(false);
    }
    if (dirty) {
      pending.current = true;
      failed.current = false;
      setSaveState("Unsaved changes");
      queueSave();
    }
  }
  useEffect(() => {
    reconcileIncoming();
  }, [editor, page.revision, blocks, workspace]);
  async function save() {
    if (!editor || saving.current || lockRef.current.locked) return;
    reconcileIncoming();
    if (conflictRef.current) return;
    if (!pending.current && !failed.current) return;
    saving.current = true;
    setSaveState("Saving…");
    const old = { ...latest.current };
    try {
      const missing: any[] = [];
      editor.state.doc.descendants((n, pos) => {
        if (
          ["troveText", "troveAtom"].includes(n.type.name) &&
          !n.attrs.blockId
        )
          missing.push({ pos, key: n.attrs.clientKey });
      });
      if (missing.length) {
        const allocated = await ids(old.workspace, missing.length);
        const tr = editor.state.tr;
        missing.forEach((m, i) => {
          editor.state.doc.descendants((n, pos) => {
            if (n.attrs.clientKey === m.key)
              tr.setNodeMarkup(pos, undefined, {
                ...n.attrs,
                blockId: allocated[i],
              });
          });
        });
        editor.view.dispatch(tr);
      }
      const captured = editor.getJSON() as NodeJSON;
      const draft = fromDocument(captured);
      const cmds = mutations(old.page.id, old.blocks, draft);
      if (cmds.length) {
        const expected = [
          ...pageGuards(old.workspace, old.page),
          ...blockMutationGuards(old.blocks, cmds),
        ];
        const result = await batch(old.workspace, cmds, expected);
        const rev =
          result.revisions.find(
            (r) =>
              r.target.entityType === "page" &&
              r.target.entityId === old.page.id,
          )?.after ?? old.page.revision;
        latest.current.page = { ...old.page, revision: rev };
        latest.current.blocks = draft.map((b, i) => ({
          id: b.id!,
          pageId: old.page.id,
          parentBlockId: b.parentBlockId,
          type: b.type,
          schemaVersion: 1,
          data: b.data,
          sortKey: String(i * 1000 + 1000).padStart(12, "0"),
          revision:
            result.revisions.find((r) => r.target.entityId === b.id)?.after ??
            old.blocks.find((x) => x.id === b.id)?.revision ??
            "1",
        }));
        await onCommitted();
      }
      pending.current =
        JSON.stringify(editor.getJSON()) !== JSON.stringify(captured);
      failed.current = false;
      setSaveState(pending.current ? "Unsaved changes" : "Saved");
    } catch (e) {
      pending.current = false;
      failed.current = true;
      setSaveState("Could not save");
      const blockedByLock = (e as Error).name === "CONFLICT" &&
        (e as Error).message === "Page is locked; unlock it before making changes";
      conflictRef.current = (e as Error).name === "CONFLICT" && !blockedByLock;
      setConflict(conflictRef.current);
      if (blockedByLock) {
        // A remote lock can arrive without a content revision. Keep the draft
        // and expose Unlock; retrying its original revision guards remains safe.
        try { acceptPageLock(await getLock(incoming.current.workspace, old.page.id)); }
        catch { /* The failed draft remains available if the lock read fails. */ }
        setSaveState("Unsaved changes (page locked)");
      }
      onError(e);
    } finally {
      saving.current = false;
      reconcileIncoming();
      if (pending.current && !conflictRef.current && !lockRef.current.locked) {
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => void saveRef.current(), 1200);
      }
    }
  }
  saveRef.current = save;
  async function togglePageLock() {
    if (lockChanging.current) return;
    lockChanging.current = true;
    setChangingLock(true);
    try {
      const wanted = !lockRef.current.locked;
      if (wanted) {
        while (saving.current) await new Promise(resolve => setTimeout(resolve, 25));
        await saveRef.current();
        if (failed.current || pending.current || conflictRef.current) return;
      }
      // Unlock must never save first: a real pending draft cannot be written
      // while locked. Use the incoming canonical page for the lock CAS without
      // advancing the separate baseline guarding that draft's content.
      const current = await getLock(incoming.current.workspace, incoming.current.page.id);
      acceptPageLock(current);
      if (wanted && (failed.current || pending.current || conflictRef.current)) return;
      if (current.locked !== wanted) {
        const canonical = wanted ? latest.current : incoming.current;
        acceptPageLock(await setLock(canonical.workspace, canonical.page, current));
      }
      if (!wanted && (pending.current || failed.current) && !conflictRef.current)
        setSaveState("Unsaved changes");
    } catch (error) {
      onError(error);
    } finally {
      lockChanging.current = false;
      setChangingLock(false);
    }
  }
  useEffect(() => {
    registerFlush?.(async () => {
      while (saving.current)
        await new Promise((resolve) => setTimeout(resolve, 25));
      await saveRef.current();
      while (saving.current)
        await new Promise((resolve) => setTimeout(resolve, 25));
      return !failed.current && !pending.current;
    });
    return () => registerFlush?.(null);
  }, []);
  useEffect(() => {
    const flush = () => void saveRef.current();
    window.addEventListener("beforeunload", flush);
    return () => {
      if (timer.current) clearTimeout(timer.current);
      void saveRef.current();
      window.removeEventListener("beforeunload", flush);
    };
  }, []);
  async function insert(kind: string) {
    if (!editor || lock.locked) return;
    setMenu(false);
    try {
      if (kind === "file") {
        if (onAttach) {
          await onAttach();
          return;
        }
        await save();
        await attachFile(latest.current.workspace, latest.current.page);
        await onCommitted();
        return;
      }
      let data: any = {};
      if (textKinds.includes(kind)) {
        data = {
          richText: [],
          ...(kind === "callout"
            ? { icon: "💡", backgroundToken: "yellow" }
            : {}),
        };
      }
      if (kind === "code")
        data = {
          source: "// Your code here",
          language: "javascript",
          wrap: true,
        };
      if (kind === "mermaid")
        data = {
          source: "flowchart LR\n  Idea --> Create\n  Create --> Learn",
          renderMode: "inert_svg",
        };
      if (kind === "equation") data = { latex: "E = mc^2", display: true };
      if (kind === "todo") {
        const title = prompt("Task title");
        if (!title) return;
        await save();
        const { recordId } = await createCanonicalTask(
          latest.current.workspace,
          title,
        );
        await onCommitted();
        data = { taskRecordId: recordId };
      }
      if (kind === "table") {
        const cellIDs = await ids(latest.current.workspace, 6);
        data = {
          rowCount: 3,
          columnCount: 2,
          headerRows: 1,
          headerColumns: 0,
          columnWidths: [240, 240],
          cells: cellIDs.map((id, i) => ({
            id,
            row: Math.floor(i / 2),
            column: i % 2,
            rowSpan: 1,
            colSpan: 1,
            richText: [],
            preMergeCells: [],
          })),
        };
      }
      if (kind === "page_mention") {
        const title = prompt("Page title to link");
        const target = workspace.pages.find(
          (p) => p.title.toLowerCase() === title?.toLowerCase(),
        );
        if (!target)
          throw new Error("Choose an existing page by its exact title.");
        data = { targetPageId: target.id, displaySnapshot: target.title };
      }
      if (kind === "columns" || kind === "tab_container") {
        const [container,a,b,childA,childB] = await ids(latest.current.workspace,5);
        const key = kind === "columns" ? "columns" : "tabs";
        data = {[key]: [a,b].map((id,i) => ({id,...(kind === "columns" ? {widthRatio:1} : {title:`Tab ${i+1}`}), childBlockIds:[i ? childB : childA]}))};
        editor.chain().focus().insertContent([
          {type:"troveAtom",attrs:{kind,data,blockId:container,clientKey:crypto.randomUUID(),depth:0}},
          ...[childA,childB].map((id,i) => ({type:"troveText",attrs:{kind:"paragraph",data:{richText:[]},blockId:id,parentBlockId:container,clientKey:crypto.randomUUID(),depth:1},content:[{type:"text",text:`Content ${i+1}`}]}))
        ]).run(); return;
      }
      if (kind === "bookmark" || kind === "embed") {
        const url = prompt("Source URL"); if (!url) return;
        if (!safeWebURL(url)) throw new Error("Use an HTTP or HTTPS URL without credentials.");
        data = kind === "bookmark" ? {url,title:prompt("Bookmark title",url)??url,description:""} : {url,provider:new URL(url).hostname,sandboxPolicy:"no_scripts_no_network",offlineText:prompt("Offline text to keep",url)??url};
      }
      if (kind === "synced_block") {
        const source = prompt("Canonical source block ID (use Copy block link)"); if (!source) return;
        const id = source.split("/").at(-1); if (!workspace.blocks.some(b=>b.id===id)) throw new Error("Choose a block visible in this workspace.");
        data = {contentSourceId:id};
      }
      if (kind === "table_of_contents") data = {maxLevel:4};
      if (kind === "date_mention") {const date=prompt("Date (YYYY-MM-DD)",new Date().toISOString().slice(0,10));if(!date)return;data={start:date,end:null,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,hasTime:false};}
      if (kind === "button") {
        const label=prompt("Button label","Rename this page");if(!label)return;
        const title=prompt("Page title to apply when this button runs",page.title);if(title===null)return;
        const action=await dispatch<{actionDefinitionId:string}>("editor.saveButton",{...envelope(workspace),pageId:page.id,label,commands:[{kind:"page.update",pageId:page.id,title}]});
        data={actionDefinitionId:action.actionDefinitionId,label};
      }
      const sel = editor.state.selection;
      const parent = sel.$from.parent;
      if (
        parent.type.name === "troveText" &&
        (parent.textContent === "/" || parent.textContent === "")
      ) {
        editor.commands.insertContentAt(
          { from: sel.$from.before(), to: sel.$from.after() },
          {
            type: textKinds.includes(kind) ? "troveText" : "troveAtom",
            attrs: { ...parent.attrs, kind, data },
          },
        );
      } else
        editor
          .chain()
          .focus()
          .insertContent({
            type: textKinds.includes(kind) ? "troveText" : "troveAtom",
            attrs: {
              kind,
              data,
              blockId: null,
              clientKey: crypto.randomUUID(),
              depth: 0,
            },
          })
          .run();
    } catch (e) {
      onError(e);
    }
  }
  function searchNext() {
    if (!editor || !find) return;
    const start = editor.state.selection.to;
    const hits: { from: number; to: number }[] = [];
    editor.state.doc.descendants((n, p) => {
      if (n.isText && n.text) {
        let i = 0;
        while (
          (i = n.text
            .toLocaleLowerCase()
            .indexOf(find.toLocaleLowerCase(), i)) >= 0
        ) {
          hits.push({ from: p + i, to: p + i + find.length });
          i += find.length;
        }
      }
    });
    const hit = hits.find((h) => h.from >= start) ?? hits[0];
    if (hit) {
      editor.view.dispatch(
        editor.state.tr
          .setSelection(
            TextSelection.create(editor.state.doc, hit.from, hit.to),
          )
          .scrollIntoView(),
      );
      editor.view.focus();
    }
  }
  return (
    <EditorContext.Provider value={{ workspace, toggleTask, openPage, pageId:page.id, onError, onCommitted, readOnly: lock.locked }}>
      <div className="page-lock"><button disabled={changingLock} aria-label={lock.locked?"Unlock page":"Lock page"} onClick={()=>void togglePageLock()}>{lock.locked?"🔒 Unlock page":"Lock page"}</button><small>{lock.locked?"Page is locked against workspace mutations.":"Alt + ↑ / ↓ navigates blocks"}</small></div>
      <fieldset className="editor-editable" disabled={lock.locked}>
      <div
        className="editor-toolbar"
        role="toolbar"
        aria-label="Text formatting"
        onMouseDown={(event) => {
          // Toolbar clicks must not blur an active composition or collapse its
          // selection before the editing command consumes it.
          if ((event.target as Element).closest("button")) event.preventDefault();
        }}
      >
        <select
          aria-label="Block type"
          value={editor?.getAttributes("troveText").kind ?? "paragraph"}
          onChange={(e) => {
            const kind = e.target.value;
            editor
              ?.chain()
              .focus()
              .updateAttributes("troveText", {
                kind,
                ...(!["toggle", "toggle_heading"].includes(kind) && editor.getAttributes("troveText").data?.collapsed !== undefined
                  ? { data: Object.fromEntries(Object.entries(editor.getAttributes("troveText").data).filter(([key]) => key !== "collapsed")) }
                  : {}),
                ...(kind === "callout"
                  ? { data: { icon: "💡", backgroundToken: "yellow" } }
                  : {}),
              })
              .run();
          }}
        >
          {commands
            .filter((c) => textKinds.includes(c[0]))
            .map((c) => (
              <option key={c[0]} value={c[0]}>
                {c[1]}
              </option>
            ))}
        </select>
        <span className="toolbar-divider" />
        {[
          ["bold", "B"],
          ["italic", "I"],
          ["underline", "U"],
          ["strike", "S"],
        ].map(([mark, label]) => (
          <button
            key={mark}
            className={editor?.isActive(mark) ? "active" : ""}
            aria-label={mark}
            aria-pressed={editor?.isActive(mark)}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => editor?.chain().focus().toggleMark(mark).run()}
          >
            {label}
          </button>
        ))}
        {editor && <ColorControls editor={editor}/>}
        <button
          aria-label="Add link"
          onClick={() => {
            const href = prompt("Link URL");
            if (href && /^(https?:|mailto:)/i.test(href))
              editor?.chain().focus().setLink({ href }).run();
          }}
        >
          ↗
        </button>
        <span className="toolbar-divider" />
        <button
          aria-label="Undo"
          onClick={() => editor?.commands.undo()}
        >
          ↶
        </button>
        <button
          aria-label="Redo"
          onClick={() => editor?.commands.redo()}
        >
          ↷
        </button>
        <button
          aria-label="Find in page"
          onClick={() => setFindOpen(!findOpen)}
        >
          ⌕
        </button>
        <button
          className="insert-button"
          onClick={() => {
            setMenu(!menu);
            setFilter("");
          }}
        >
          ＋ Insert
        </button>
        <span role="status" className="save-state">
          {saveState}
        </span>
        <button onClick={() => void save()} aria-label="Save page">
          Save
        </button>
      </div>
      {findOpen && (
        <div className="find-bar">
          <input
            aria-label="Find text"
            autoFocus
            value={find}
            onChange={(e) => setFind(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && searchNext()}
          />
          <button onClick={searchNext}>Next match</button>
          <input aria-label="Replace with" value={replacement} onChange={e=>setReplacement(e.target.value)} />
          <button disabled={!find} onClick={()=>{if(!editor)return;const result=replaceDocument(editor.getJSON() as NodeJSON,find,replacement);editor.commands.setContent(result.doc,{emitUpdate:true});setSaveState(`Replaced ${result.count} occurrences`);}}>Replace all (exact)</button>
          <button onClick={() => setFindOpen(false)}>Close</button>
        </div>
      )}
      {conflict && (
        <div className="conflict" role="alert">
          This page changed elsewhere. Your draft is still here. Copy it before
          reloading to reconcile the newer revision.
          <button
            onClick={() => {
              navigator.clipboard.writeText(
                JSON.stringify(editor?.getJSON(), null, 2),
              );
            }}
          >
            Copy draft JSON
          </button>
        </div>
      )}
      {menu && (
        <div className="slash-menu" role="dialog" aria-label="Insert a block">
          <input
            autoFocus
            aria-label="Filter block types"
            placeholder="Find a block…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && setMenu(false)}
          />
          {commands
            .filter((c) =>
              c.join(" ").toLowerCase().includes(filter.toLowerCase()),
            )
            .map(([kind, label, desc]) => (
              <button key={kind} onClick={() => void insert(kind)}>
                <strong>{label}</strong>
                <span>{desc}</span>
              </button>
            ))}
        </div>
      )}
      <EditorContent editor={editor} />
      <button
        className="add-block"
        onClick={() => {
          if (editor) {
            const pos = editor.state.doc.content.size;
            editor
              .chain()
              .insertContentAt(pos, {
                type: "troveText",
                attrs: { kind: "paragraph", clientKey: crypto.randomUUID() },
              })
              .setTextSelection(pos + 1)
              .run();
            // Tiptap focus() schedules requestAnimationFrame; direct focus keeps
            // the first keystroke after clicking Add in the newly inserted block.
            editor.view.focus();
          }
        }}
      >
        ＋ Add a thought · type / for blocks
      </button>
      </fieldset>
      <AdvancedPanel workspace={workspace} page={page} flush={async()=>{await save();return !failed.current&&!pending.current}} onCommitted={onCommitted} onError={onError} openPage={openPage}/>
    </EditorContext.Provider>
  );
}
