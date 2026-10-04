import { useEffect, useRef, useState } from "react";
import type { NodeViewProps } from "@tiptap/react";
import type { Editor } from "@tiptap/core";
import { closeHistory } from "@tiptap/pm/history";
import type { D, WorkspaceSnapshot } from "../api";
import { readBlock } from "../api";
import { richNodes, richText } from "./codec";
import { InlineRichEditor } from "./InlineRichEditor";
import { insertTableLine, removeTableLine, tableLineRemoval, tableWidths } from "./tableStructure";
import { blockReadable, inertEmbedDocument, mergeCells, safeWebURL, splitCell } from "./advanced";
import { previewButton, runButton } from "../advanced/service";

export function AdvancedTable({ data: d, update, owner, allocateIds, readOnly = false }: {
  data: D.block_table_v1; update: (value: Partial<D.block_table_v1>) => void; owner: Editor; allocateIds: (count: number) => Promise<string[]>; readOnly?: boolean;
}) {
  const latest = useRef({data:d,update,readOnly}); latest.current = {data:d,update,readOnly};
  const [first, setFirst] = useState(""), [last, setLast] = useState(""), [error, setError] = useState("");
  const [row, setRow] = useState(0), [column, setColumn] = useState(0), [busy, setBusy] = useState(false);
  const [removal, setRemoval] = useState<{axis: "row" | "column"; index: number} | null>(null);
  const commitStructure = (value: Partial<D.block_table_v1>) => {
    owner.view.dispatch(closeHistory(owner.state.tr));
    latest.current.update(value);
    owner.view.dispatch(closeHistory(owner.state.tr));
  };
  const action = (fn: () => void) => { try { fn(); setError(""); } catch (e) { setError((e as Error).message); } };
  const add = async (axis: "row" | "column", index: number) => {
    if (readOnly || busy) return;
    setBusy(true);
    try {
      const allocated = await allocateIds(axis === "row" ? d.columnCount : d.rowCount), current = latest.current;
      if (current.readOnly) return;
      if (current.data.rowCount !== d.rowCount || current.data.columnCount !== d.columnCount) throw new Error("The table changed while adding cells. Try again.");
      commitStructure(insertTableLine(current.data, axis, index, allocated)); setError("");
    }
    catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  };
  const remove = (axis: "row" | "column", index: number) => action(() => {
    if (tableLineRemoval(d, axis, index).length) setRemoval({axis,index});
    else commitStructure(removeTableLine(d,axis,index));
  });
  const widths = tableWidths(d);
  return <div className="table-scroll">
    <details className="table-structure"><summary>Table structure</summary>
      <fieldset disabled={readOnly || busy}>
        {(["row", "column"] as const).map(axis => {
          const count = axis === "row" ? d.rowCount : d.columnCount, selected = Math.min(axis === "row" ? row : column, count - 1);
          return <div key={axis} className="table-axis-controls"><label>Selected {axis}<select aria-label={`Selected table ${axis}`} value={selected} onChange={event => (axis === "row" ? setRow : setColumn)(Number(event.target.value))}>
            {Array.from({length: count}, (_, i) => <option key={i} value={i}>{i + 1}</option>)}
          </select></label><button type="button" onClick={() => void add(axis, selected)}>Insert {axis} before</button><button type="button" onClick={() => void add(axis, selected + 1)}>Insert {axis} after</button><button type="button" disabled={count <= 1} onClick={() => remove(axis, selected)}>Remove {axis}</button></div>;
        })}
        <label>Header rows<input aria-label="Header rows" type="number" min="0" max={d.rowCount} value={d.headerRows} onChange={event => update({headerRows: Math.max(0,Math.min(d.rowCount,Math.floor(Number(event.target.value) || 0)))})}/></label>
        <label>Header columns<input aria-label="Header columns" type="number" min="0" max={d.columnCount} value={d.headerColumns} onChange={event => update({headerColumns: Math.max(0,Math.min(d.columnCount,Math.floor(Number(event.target.value) || 0)))})}/></label>
        <div className="table-width-controls">{widths.map((width,index) => <label key={index}>Column {index+1} width<input aria-label={`Column ${index+1} width in pixels`} type="number" min="40" max="2400" value={width} onChange={event => update({columnWidths: widths.map((value,i) => i === index ? Math.max(40,Math.min(2400,Number(event.target.value) || 40)) : value)})}/></label>)}</div>
      </fieldset>
    </details>
    {removal && <div role="alertdialog" aria-label={`Remove table ${removal.axis}`} className="table-removal"><p>Remove {removal.axis} {removal.index+1} and its populated cells? This can be undone.</p><button type="button" onClick={() => setRemoval(null)}>Keep cells</button><button type="button" disabled={readOnly} onClick={() => action(() => {commitStructure(removeTableLine(d,removal.axis,removal.index));setRemoval(null);})}>Remove populated {removal.axis}</button></div>}
    <fieldset className="table-controls" disabled={readOnly}>
      <label>First cell <select value={first} onChange={e => setFirst(e.target.value)}><option value="">Choose</option>{d.cells.map(c => <option key={c.id} value={c.id}>{c.row + 1}:{c.column + 1}</option>)}</select></label>
      <label>Last cell <select value={last} onChange={e => setLast(e.target.value)}><option value="">Choose</option>{d.cells.map(c => <option key={c.id} value={c.id}>{c.row + 1}:{c.column + 1}</option>)}</select></label>
      <button type="button" onClick={() => action(() => commitStructure(mergeCells(d, first, last)))}>Merge range</button>
      {error && <span role="alert">{error}</span>}
    </fieldset>
    <table className="simple-table" style={{width: widths.reduce((sum,width) => sum+width,0), tableLayout:"fixed"}}><colgroup>{widths.map((width,index) => <col key={index} style={{width}}/>)}</colgroup><tbody>{Array.from({ length: d.rowCount }, (_, row) => <tr key={row}>
      {d.cells.filter(c => c.row === row).sort((a, b) => a.column - b.column).map(cell => {const Cell = cell.row < d.headerRows || cell.column < d.headerColumns ? "th" : "td"; return <Cell key={cell.id} rowSpan={cell.rowSpan} colSpan={cell.colSpan} scope={cell.row < d.headerRows ? "col" : cell.column < d.headerColumns ? "row" : undefined}>
        <InlineRichEditor label={`Row ${row + 1} column ${cell.column + 1}`} value={cell.richText} owner={owner} readOnly={readOnly}
          onChange={value => update({ cells: d.cells.map(c => c.id === cell.id ? { ...c, richText: value } : c) })} />
        {(cell.colSpan > 1 || cell.rowSpan > 1) && <button type="button" disabled={readOnly || !cell.preMergeCells.length}
          title={!cell.preMergeCells.length ? "Imported merge has no original cell evidence" : undefined}
          onClick={() => action(() => commitStructure(splitCell(d, cell.id)))}>Restore cells</button>}
      </Cell>;})}
    </tr>)}</tbody></table>
  </div>;
}
export function LayoutBlock({ node, editor, updateAttributes, readOnly = false }: NodeViewProps & { readOnly?: boolean }) {
  const data = node.attrs.data, tabbed = node.attrs.kind === "tab_container", key = tabbed ? "tabs" : "columns";
  const groups = data[key] ?? [];
  const [active, setActive] = useState(groups[0]?.id), [, refresh] = useState(0);
  useEffect(() => {
    const changed = () => refresh(n => n + 1);
    editor.on("transaction", changed);
    return () => { editor.off("transaction", changed); };
  }, [editor]);
  const current = groups.some((group: any) => group.id === active) ? active : groups[0]?.id;
  const update = (items: any[]) => updateAttributes({ data: { ...data, [key]: items } });
  const editChild = (id: string, value: D.RichText) => {
    if (readOnly) return;
    let found: { pos: number; size: number } | undefined;
    editor.state.doc.descendants((child, pos) => {
      if (child.attrs.blockId === id && child.type.name === "troveText") found = { pos, size: child.nodeSize };
    });
    if (!found) return;
    const content = richNodes(value).map(node => editor.schema.nodeFromJSON(node));
    // Keep selection/focus in the child editor; the authoritative parent owns
    // this transaction, undo history, revision guards and subsequent save.
    editor.view.dispatch(editor.state.tr.replaceWith(found.pos + 1, found.pos + found.size - 1, content));
  };
  return <div className="advanced-layout">
    <div role={tabbed ? "tablist" : undefined} aria-label={tabbed ? "Content tabs" : "Columns"}>
      {groups.map((group: any, i: number) => <button type="button" key={group.id} role={tabbed ? "tab" : undefined}
        aria-selected={tabbed ? group.id === current : undefined} onClick={() => setActive(group.id)}>{group.title ?? `Column ${i + 1}`}</button>)}
    </div>
    <div className={tabbed ? "tab-panels" : "column-panels"}>
      {groups.filter((group: any) => !tabbed || group.id === current).map((group: any, i: number) => <section key={group.id} role={tabbed ? "tabpanel" : undefined} style={!tabbed ? { flex: group.widthRatio } : undefined}>
        <label>{tabbed ? "Tab title" : "Width ratio"}<input disabled={readOnly} aria-label={tabbed ? "Tab title" : `Column ${i + 1} width`} type={tabbed ? "text" : "number"}
          min="0.1" step="0.1" value={tabbed ? group.title : group.widthRatio}
          onChange={e => update(groups.map((item: any) => item.id === group.id ? { ...item, [tabbed ? "title" : "widthRatio"]: tabbed ? e.target.value : Math.max(.1, Number(e.target.value) || 1) } : item))} /></label>
        {group.childBlockIds.map((id: string) => {
          let child: any = null;
          editor.state.doc.descendants(candidate => { if (candidate.attrs.blockId === id) child = candidate; });
          return child ? <div key={id}>
            {child.type.name === "troveText"
              ? <InlineRichEditor label={`${group.title ?? "Column"} content`} value={richText(child.content.toJSON())} owner={editor} readOnly={readOnly} onChange={value => editChild(id, value)} />
              : <p>{blockReadable({ data: child.attrs.data, type: child.attrs.kind } as D.Block) || child.attrs.kind}</p>}
            <label>Move to <select disabled={readOnly} value={group.id} onChange={e => update(groups.map((item: any) => ({ ...item, childBlockIds: [...item.childBlockIds.filter((value: string) => value !== id), ...(item.id === e.target.value ? [id] : [])] })))}>
              {groups.map((item: any, j: number) => <option key={item.id} value={item.id}>{item.title ?? `Column ${j + 1}`}</option>)}
            </select></label>
          </div> : <p key={id}>Unavailable child · {id}</p>;
        })}
      </section>)}
    </div>
    <small>Edits keep canonical child identities, rich formatting and links.</small>
  </div>;
}
export function AdvancedBlock({props,w,pageId,openPage,onError,onCommitted,readOnly=false}:{props:NodeViewProps;w:WorkspaceSnapshot|null;pageId?:string;openPage:(id:string)=>void;onError:(e:unknown)=>void;onCommitted:()=>Promise<void>;readOnly?:boolean}){
 const {node,updateAttributes}=props,d=node.attrs.data,kind=node.attrs.kind;
 const update=(patch:any)=>updateAttributes({data:{...d,...patch}});
 const [proposal,setProposal]=useState<{label:string;commands:any[]}|null>(null),[busy,setBusy]=useState(false);
 if(kind==='columns'||kind==='tab_container')return <LayoutBlock {...props} readOnly={readOnly}/>;
 if(kind==='bookmark')return <div className="bookmark-card"><a href={safeWebURL(d.url)??undefined} target="_blank" rel="noopener noreferrer">{d.title||d.url}</a><p>{d.description}</p><details><summary>Edit bookmark</summary><label>Title<input value={d.title} onChange={e=>update({title:e.target.value})}/></label><label>URL<input value={d.url} onChange={e=>update({url:e.target.value})}/></label><label>Description<textarea value={d.description??''} onChange={e=>update({description:e.target.value})}/></label></details></div>;
 if(kind==='embed')return <div className="embed-card"><iframe title={`${d.provider} offline embed`} sandbox="" referrerPolicy="no-referrer" srcDoc={inertEmbedDocument(d.offlineText)}/><p>Offline snapshot · network and scripts disabled</p><a href={safeWebURL(d.url)??undefined} target="_blank" rel="noopener noreferrer">Open source externally</a><label>Offline content<textarea value={d.offlineText} onChange={e=>update({offlineText:e.target.value})}/></label></div>;
 if(kind==='date_mention')return <label>Date <input type={d.hasTime?'datetime-local':'date'} value={d.start} onChange={e=>update({start:e.target.value})}/><span>{d.timezone}</span></label>;
 if(kind==='breadcrumb'){const path:D.Page[]=[];let p=w?.pages.find(p=>p.id===pageId);const seen=new Set<string>();while(p&&!seen.has(p.id)){path.unshift(p);seen.add(p.id);p=w?.pages.find(x=>x.id===p!.parentPageId)}return <nav aria-label="Breadcrumb">{path.map((p,i)=><span key={p.id}>{i?' / ':''}<button onClick={()=>openPage(p.id)}>{p.title}</button></span>)}</nav>}
 if(kind==='table_of_contents'){const headings:any[]=[];props.editor.state.doc.descendants(n=>{if(/^heading_[1-4]$/.test(n.attrs.kind)&&Number(n.attrs.kind.slice(-1))<=d.maxLevel)headings.push(n)});return <nav aria-label="Table of contents">{headings.map(n=><button key={n.attrs.blockId??n.attrs.clientKey} style={{display:'block',marginInlineStart:(Number(n.attrs.kind.slice(-1))-1)*12}} onClick={()=>props.editor.view.dom.querySelector(`[data-block-id="${n.attrs.blockId}"]`)?.scrollIntoView({behavior:'smooth'})}>{n.textContent||'Untitled heading'}</button>)}{!headings.length&&<p>Add headings to build a table of contents.</p>}</nav>}
 if(kind==='synced_block') return <SyncedBlock workspace={w} sourceId={d.contentSourceId} openPage={openPage}/>;
 if(kind==='button')return <div><button disabled={busy||!w} onClick={()=>w&&void previewButton(w,d.actionDefinitionId).then(setProposal).catch(onError)}>{d.label}</button>{proposal&&<dialog open><h3>Review {proposal.label}</h3><pre>{JSON.stringify(proposal.commands,null,2)}</pre><button onClick={()=>setProposal(null)}>Cancel</button><button disabled={busy} onClick={()=>{if(!w)return;setBusy(true);void runButton(w,proposal.commands).then(onCommitted).then(()=>setProposal(null)).catch(onError).finally(()=>setBusy(false))}}>Apply through workspace permissions</button></dialog>}</div>;
 return null;
}

function SyncedBlock({workspace,sourceId,openPage}:{workspace:WorkspaceSnapshot|null;sourceId:string;openPage:(id:string)=>void}) {
 const [source,setSource]=useState<D.Block>();
 const [loading,setLoading]=useState(true);
 useEffect(()=>{
  let live=true;setSource(undefined);setLoading(true);
  if(!workspace){setLoading(false);return;}
  void readBlock(workspace,sourceId).then(block=>{if(live)setSource(block);}).catch(()=>{}).finally(()=>{if(live)setLoading(false);});
  return()=>{live=false;};
 },[workspace?.workspaceId,workspace?.snapshotCursor,sourceId]);
 return <div className="synced-card"><small>Reusable block · live canonical source</small><p>{source?blockReadable(source):loading?'Loading source…':'Source is unavailable. The reference is preserved.'}</p>{source&&<button onClick={()=>openPage(source.pageId)}>Edit source</button>}</div>;
}
