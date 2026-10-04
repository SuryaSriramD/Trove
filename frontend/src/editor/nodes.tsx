import React, { createContext, useContext, useEffect, useState } from "react";
import { Node, Extension, mergeAttributes } from "@tiptap/core";
import {
  NodeViewWrapper,
  NodeViewContent,
  ReactNodeViewRenderer,
  type NodeViewProps,
} from "@tiptap/react";
import { Plugin } from "@tiptap/pm/state";
import { fileURL, ids, type WorkspaceSnapshot } from "../api";
import { plain } from "./codec";
import { clearTemporaryReveal, temporarilyExpanded } from "./visibility";
import { AdvancedBlock, AdvancedTable } from "./AdvancedBlocks";
import katex from "katex";
import mermaid from "mermaid";
import { CodeBlock, ImageBlock } from "./TechnicalBlocks";
import "katex/dist/katex.min.css";
import "highlight.js/styles/github.css";
export const EditorContext = createContext<{
  workspace: WorkspaceSnapshot | null;
  toggleTask: (id: string) => void;
  openPage: (id: string) => void;
  pageId?: string;
  readOnly: boolean;
  onError: (e: unknown) => void;
  onCommitted: () => Promise<void>;
}>({ workspace: null, toggleTask: () => {}, openPage: () => {}, onError: () => {}, onCommitted: async () => {}, readOnly: false });
const attrs = {
  blockId: { default: null, renderHTML: () => ({}) },
  clientKey: { default: null, renderHTML: () => ({}) },
  kind: {
    default: "paragraph",
    parseHTML: (e: HTMLElement) =>
      e.dataset.kind ??
      {
        H1: "heading_1",
        H2: "heading_2",
        H3: "heading_3",
        H4: "heading_4",
        LI: "bulleted_list_item",
        BLOCKQUOTE: "quote",
      }[e.tagName] ??
      "paragraph",
  },
  depth: {
    default: 0,
    parseHTML: (e: HTMLElement) => Number(e.dataset.depth ?? 0),
  },
  parentBlockId: { default: null, renderHTML: () => ({}) },
  version: { default: 1 },
  data: {
    default: {},
    parseHTML: (e: HTMLElement) => {
      try {
        return JSON.parse(e.dataset.payload ?? "{}");
      } catch {
        return {};
      }
    },
    renderHTML: () => ({}),
  },
};
export function BlockActions({ editor, node, getPos }: NodeViewProps) {
  const p = () => getPos() ?? 0;
  const { pageId, onError, workspace } = useContext(EditorContext);
  async function duplicate() {
    if (!workspace) return;
    const nodes: {node:any;pos:number}[] = []; let collecting=false;
    editor.state.doc.forEach((n,pos)=>{if(pos===p())collecting=true;else if(collecting&&(n.attrs.depth??0)<=(node.attrs.depth??0))collecting=false;if(collecting)nodes.push({node:n,pos})});
    const allocated=await ids(workspace,nodes.length),mapping=new Map(nodes.map((n,i)=>[n.node.attrs.blockId,allocated[i]]));
    const remap=(value:any,key=''):any=>Array.isArray(value)?value.map(v=>remap(v,key)):value&&typeof value==='object'?Object.fromEntries(Object.entries(value).map(([k,v])=>[k,remap(v,k)])):typeof value==='string'&&['childBlockIds','contentSourceId'].includes(key)?(mapping.get(value)??value):value;
    const copies=nodes.map(({node:n},i)=>({...n.toJSON(),attrs:{...n.attrs,blockId:allocated[i],clientKey:crypto.randomUUID(),parentBlockId:mapping.get(n.attrs.parentBlockId)??n.attrs.parentBlockId,data:remap(n.attrs.data)}}));
    const last=nodes.at(-1);if(last)editor.commands.insertContentAt(last.pos+last.node.nodeSize,copies);
  }
  return (
    <div className="block-actions" contentEditable={false}>
      <button
        className="drag-grip"
        data-drag-handle
        title="Drag block"
        aria-label="Drag block"
      >
        ⠿
      </button>
      <button aria-label="Copy block link" title="Copy block link" disabled={!node.attrs.blockId} onClick={() => void navigator.clipboard.writeText(`trove://page/${pageId}/block/${node.attrs.blockId}${workspace?.workspaceId ? `?workspace=${encodeURIComponent(workspace.workspaceId)}` : ""}`).catch(onError)}>↗</button>
      <button
        title="Duplicate block"
        aria-label="Duplicate block"
        onClick={() => void duplicate().catch(onError)}
      >
        ⧉
      </button>
    </div>
  );
}
function TextView(props: NodeViewProps) {
  const { node, updateAttributes } = props;
  const k = node.attrs.kind;
  const [, refresh] = useState(0);
  useEffect(() => {
    if (!["toggle", "toggle_heading"].includes(k)) return;
    const changed = () => refresh(n => n + 1);
    props.editor.on("transaction", changed);
    return () => { props.editor.off("transaction", changed); };
  }, [props.editor, k]);
  const open = node.attrs.data.collapsed !== true || temporarilyExpanded(props.editor, node.attrs.blockId);
  return (
    <NodeViewWrapper
      className={`text-block block-${k}`}
      data-block-id={node.attrs.blockId}
      data-kind={k}
      data-parent={node.attrs.parentBlockId}
      style={{ marginInlineStart: `${node.attrs.depth * 24}px` }}
      dir="auto"
    >
      <BlockActions {...props} />
      {["toggle", "toggle_heading"].includes(k) && (
        <button
          className="toggle-button"
          contentEditable={false}
          aria-label="Toggle content"
          aria-expanded={open}
          onClick={() => {
            updateAttributes({ data: { ...node.attrs.data, collapsed: open } });
            clearTemporaryReveal(props.editor);
          }}
        >
          ▸
        </button>
      )}
      {k === "callout" && (
        <button
          contentEditable={false}
          className="callout-icon"
          onClick={() => {
            const icon = prompt("Callout icon", node.attrs.data.icon ?? "💡");
            if (icon !== null)
              updateAttributes({ data: { ...node.attrs.data, icon } });
          }}
        >
          {node.attrs.data.icon ?? "💡"}
        </button>
      )}
      <NodeViewContent
        className="block-text"
        role={k.startsWith("heading_") ? "heading" : undefined}
        aria-level={k.startsWith("heading_") ? Number(k.slice(-1)) : undefined}
      />
    </NodeViewWrapper>
  );
}
function Mermaid({ source }: { source: string }) {
  const [svg, setSvg] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let stale = false;
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: "neutral",
      suppressErrorRendering: true,
    });
    mermaid
      .render(`m${crypto.randomUUID().replaceAll("-", "")}`, source)
      .then((r) => {
        if (!stale) {
          setSvg(r.svg);
          setError("");
        }
      })
      .catch(() => {
        if (!stale)
          setError(
            "Diagram source needs a correction. Your source is preserved.",
          );
      });
    return () => {
      stale = true;
    };
  }, [source]);
  return error ? (
    <p className="muted">{error}</p>
  ) : (
    <div className="mermaid-output" dangerouslySetInnerHTML={{ __html: svg }} />
  );
}
function AtomView(props: NodeViewProps) {
  const { node, updateAttributes } = props;
  const { workspace: w, toggleTask, openPage, pageId, onError, onCommitted, readOnly } = useContext(EditorContext);
  const d = node.attrs.data;
  const kind = node.attrs.kind;
  const update = (patch: any) => updateAttributes({ data: { ...d, ...patch } });
  const file = w?.files.find((f) => f.id === d.fileId);
  const record = w?.records.find((r) => r.id === d.taskRecordId);
  const page = w?.pages.find((p) => p.id === record?.pageId);
  const facet = w?.taskFacets.find((t) => t.recordId === record?.id);
  const property = w?.properties.find((p) => p.id === facet?.statusPropertyId);
  const option = record?.values.find(
    (v) => v.propertyId === property?.id,
  )?.value;
  const done =
    (property?.config as any)?.options?.find((o: any) => o.id === option)
      ?.group === "done";
  let body: React.ReactNode;
  if (kind === "divider") body = <hr />;
  else if (kind === "todo")
    body = (
      <div className="todo-block">
        <input
          type="checkbox"
          aria-label={`Complete ${page?.title ?? "unavailable task"}`}
          checked={done}
          disabled={!record || !page || !!page.deletedAt}
          onChange={() => toggleTask(d.taskRecordId)}
        />
        <button
          className={done ? "done" : ""}
          onClick={() => page && openPage(page.id)}
        >
          {page?.title ?? "Unavailable task"}
        </button>
      </div>
    );
  else if (kind === "code") body = <CodeBlock data={d} update={update} owner={props.editor} readOnly={readOnly} onError={onError}/>;
  else if (kind === "mermaid")
    body = (
      <>
        <Mermaid source={d.source ?? ""} />
        <details>
          <summary>Edit Mermaid</summary>
          <textarea
            aria-label="Mermaid source"
            value={d.source ?? ""}
            onChange={(e) => update({ source: e.target.value })}
          />
        </details>
      </>
    );
  else if (kind === "equation")
    body = (
      <>
        <div
          className="equation"
          dangerouslySetInnerHTML={{
            __html: katex.renderToString(d.latex ?? "", {
              throwOnError: false,
              displayMode: d.display ?? true,
              trust: false,
            }),
          }}
        />
        <details>
          <summary>Edit equation</summary>
          <input
            aria-label="LaTeX equation"
            value={d.latex ?? ""}
            onChange={(e) => update({ latex: e.target.value })}
          />
          <label>
            <input
              type="checkbox"
              checked={d.display ?? true}
              onChange={(e) => update({ display: e.target.checked })}
            />{" "}
            Display equation
          </label>
        </details>
      </>
    );
  else if (kind === "table") body = <AdvancedTable data={d} update={update} owner={props.editor} allocateIds={count => w ? ids(w,count) : Promise.reject(new Error("Workspace unavailable"))} readOnly={readOnly} />;
  else if (["image", "audio", "video", "pdf", "file"].includes(kind))
    body = (
      <figure>
        {kind === "image" ? (
          <ImageBlock data={d} update={update} originalName={file?.originalName} readOnly={readOnly}/>
        ) : kind === "audio" ? (
          <audio controls src={fileURL(d.fileId)} />
        ) : kind === "video" ? (
          <video controls src={fileURL(d.fileId)} />
        ) : kind === "pdf" ? (
          <object
            type="application/pdf"
            data={fileURL(d.fileId)}
            aria-label={file?.originalName ?? "PDF attachment"}
          >
            <a href={fileURL(d.fileId)} target="_blank" rel="noreferrer">
              Open PDF
            </a>
          </object>
        ) : (
          <a
            className="file-card"
            href={fileURL(d.fileId)}
            target="_blank"
            rel="noreferrer"
          >
            ↗ {d.displayName ?? file?.originalName ?? "Open attachment"}
            <small>{file?.sizeBytes} bytes · immutable file</small>
          </a>
        )}
        <figcaption>
          <input
            aria-label="Attachment caption"
            placeholder="Add a caption…"
            value={plain(d.caption ?? [])}
            onChange={(e) => update({ caption: [{ text: e.target.value }] })}
          />
        </figcaption>
        {kind === "image" && (
          <details>
            <summary>Image description</summary>
            <input
              aria-label="Image alternative text"
              value={d.altText ?? ""}
              onChange={(e) => update({ altText: e.target.value })}
            />
          </details>
        )}
      </figure>
    );
  else if (kind === "page_mention")
    body = (
      <button className="page-link" onClick={() => openPage(d.targetPageId)}>
        ↗{" "}
        {w?.pages.find((p) => p.id === d.targetPageId)?.title ??
          d.displaySnapshot}
      </button>
    );
  else if (["tab_container", "columns", "bookmark", "embed", "synced_block", "breadcrumb", "table_of_contents", "date_mention", "button"].includes(kind)) body = <AdvancedBlock props={props} w={w} pageId={pageId} openPage={openPage} onError={onError} onCommitted={onCommitted} readOnly={readOnly} />;
  else
    body = (
      <div className="preserved">
        <span className="eyebrow">Preserved · {d.sourceType ?? kind}</span>
        <p>
          {d.readableText ??
            d.offlineText ??
            d.title ??
            "Original structure is retained in this page."}
        </p>
        <details>
          <summary>View original structure</summary>
          <pre>{JSON.stringify(d, null, 2)}</pre>
        </details>
      </div>
    );
  return (
    <NodeViewWrapper
      className={`atom-block block-${kind}`}
      style={{ marginInlineStart: `${node.attrs.depth * 24}px` }}
      data-block-id={node.attrs.blockId}
      data-parent={node.attrs.parentBlockId}
      contentEditable={false}
    >
      <BlockActions {...props} />
      {body}
    </NodeViewWrapper>
  );
}
export const TroveText = Node.create({
  name: "troveText",
  group: "block",
  content: "inline*",
  defining: true,
  draggable: true,
  addAttributes() {
    return attrs;
  },
  parseHTML() {
    return ["p", "h1", "h2", "h3", "h4", "li", "blockquote"].map((tag) => ({
      tag,
    }));
  },
  renderHTML({ node, HTMLAttributes }) {
    const k = node.attrs.kind;
    const tag = k.startsWith("heading_")
      ? `h${k.slice(-1)}`
      : k === "quote"
        ? "blockquote"
        : "p";
    return [
      tag,
      mergeAttributes(HTMLAttributes, {
        "data-kind": k,
        "data-depth": node.attrs.depth,
        "data-payload": JSON.stringify(node.attrs.data),
      }),
      0,
    ];
  },
  addNodeView() {
    return ReactNodeViewRenderer(TextView);
  },
});
export const TroveAtom = Node.create({
  name: "troveAtom",
  group: "block",
  atom: true,
  draggable: true,
  selectable: true,
  addAttributes() {
    return {
      ...attrs,
      kind: {
        default: "unsupported_external",
        parseHTML: (e: HTMLElement) => e.dataset.kind,
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-trove-atom]" }];
  },
  renderHTML({ node }) {
    return [
      "div",
      {
        "data-trove-atom": "",
        "data-kind": node.attrs.kind,
        "data-depth": node.attrs.depth,
        "data-payload": JSON.stringify(node.attrs.data),
      },
      node.attrs.data.readableText ?? node.attrs.kind,
    ];
  },
  addNodeView() {
    return ReactNodeViewRenderer(AtomView);
  },
});
export const CanonicalIdentity = Extension.create({
  name: "canonicalIdentity",
  addProseMirrorPlugins() {
    return [
      new Plugin({
        appendTransaction(transactions, _old, state) {
          if (!transactions.some((t) => t.docChanged)) return null;
          const seenKeys = new Set<string>(),
            seenIDs = new Set<string>();
          const tr = state.tr;
          state.doc.descendants((node, pos) => {
            if (!["troveText", "troveAtom"].includes(node.type.name)) return;
            let key = node.attrs.clientKey,
              id = node.attrs.blockId;
            if (!key || seenKeys.has(key)) key = crypto.randomUUID();
            if (id && seenIDs.has(id)) id = null;
            seenKeys.add(key);
            if (id) seenIDs.add(id);
            if (key !== node.attrs.clientKey || id !== node.attrs.blockId)
              tr.setNodeMarkup(pos, undefined, {
                ...node.attrs,
                clientKey: key,
                blockId: id,
              });
          });
          return tr.docChanged ? tr : null;
        },
      }),
    ];
  },
});
