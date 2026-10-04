import {useEffect,useState} from "react";
import {dispatch,envelope,type WorkspaceSnapshot} from "../api";
import "../imports/imports.css";
import {ClipperPanel} from "./ClipperPanel";

type Root={id:string;path:string;repository:boolean;approved:boolean;lastScan:string};
type Source={id:string;path:string;state:string;language:string;hash:string};
type CapturePlan={planId:string;planHash:string;state:string;rootPageId:string;initialized:boolean;cursor:number;items:{sourceLocator:string;outcome:string;reason:string}[]};
type Hit={sourceId:string;path:string;language:string;chunk:{startLine:number;endLine:number;symbol:string;text:string}};
type CaptureBridge={PickIndexRoot:(repository:boolean)=>Promise<string>;CaptureScreenshot:()=>Promise<string>};
export function CapturePanel({workspace,onChanged,onOpenPage}:{workspace:WorkspaceSnapshot;onChanged:()=>Promise<void>|void;onOpenPage?:(id:string)=>void}){
 const [roots,setRoots]=useState<Root[]>([]),[sources,setSources]=useState<Source[]>([]),[selected,setSelected]=useState(""),[repository,setRepository]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(""),[notice,setNotice]=useState(""),[title,setTitle]=useState(""),[url,setURL]=useState(""),[content,setContent]=useState(""),[asHTML,setAsHTML]=useState(false),[plan,setPlan]=useState<CapturePlan>(),[query,setQuery]=useState(""),[hits,setHits]=useState<Hit[]>([]);
 const request=<T,>(method:string,data:object={})=>dispatch<T>(method,{...envelope(workspace),...data});
 const refresh=async()=>setRoots(await request<Root[]>("index.roots"));
 useEffect(()=>{void refresh().catch(e=>setError(String(e)))},[workspace.workspaceId]);
 const act=async(fn:()=>Promise<void>)=>{setBusy(true);setError("");setNotice("");try{await fn()}catch(e){setError(String(e))}finally{setBusy(false)}};
 const bridge=()=>window.go!.main.App as unknown as CaptureBridge;
 const choose=()=>act(async()=>{const raw=await bridge().PickIndexRoot(repository);if(!raw)return;const root=JSON.parse(raw) as Root;setSelected(root.id);await refresh();const result=await request<{indexed:number;moved:number}>("index.scan",{rootId:root.id});setNotice(`Indexed ${result.indexed} files; tracked ${result.moved} moves.`);setSources(await request<Source[]>("index.entries",{rootId:root.id}));await refresh()});
 const scan=(root:Root)=>act(async()=>{setSelected(root.id);const result=await request<{indexed:number;moved:number}>("index.scan",{rootId:root.id});setSources(await request<Source[]>("index.entries",{rootId:root.id}));setNotice(`Indexed ${result.indexed} files; tracked ${result.moved} moves.`);await refresh()});
 const preview=()=>act(async()=>setPlan(await request<CapturePlan>("capture.preview",{title,url,...(asHTML?{html:content}:{text:content})})));
 const screenshot=()=>act(async()=>{const sourceHandle=await bridge().CaptureScreenshot();if(sourceHandle)setPlan(await request<CapturePlan>("imports.preview",{sourceHandle,sourceNamespace:`screenshot-${Date.now()}`,sourceSystem:"screenshot"}))});
 const commit=()=>act(async()=>{if(!plan)return;let current=plan;while(current.state!=="complete"){current=await request<CapturePlan>("imports.commit",{planId:current.planId,planHash:current.planHash});setPlan(current)}await onChanged();setNotice("Capture saved as offline workspace content.")});
 return <section className="import-export-panel" aria-label="Capture and external indexing">
  <h2>Capture &amp; indexed folders</h2><p>Paste a browser selection or shared HTML into a reviewed clip. Capture an area with the macOS screenshot picker.</p>
  <div className="import-controls"><label>Title<input value={title} onChange={e=>setTitle(e.target.value)}/></label><label>Source URL<input value={url} onChange={e=>setURL(e.target.value)} placeholder="https://…"/></label></div>
  <label>Shared selection<textarea rows={7} value={content} onChange={e=>setContent(e.target.value)} placeholder="Paste selected text or HTML from your browser" style={{width:"100%"}}/></label>
  <div className="import-controls"><label><input type="checkbox" checked={asHTML} onChange={e=>setAsHTML(e.target.checked)}/>Selection contains HTML</label><button disabled={busy||!content.trim()} onClick={preview}>Preview clip</button><button disabled={busy} onClick={screenshot}>Capture screenshot…</button></div>
  {plan&&<div><h3>Capture review</h3><ul>{plan.items.map((item,i)=><li key={i}>{item.sourceLocator} · {item.outcome}: {item.reason}</li>)}</ul>{plan.state!=="complete"?<button disabled={busy} onClick={commit}>Save reviewed capture</button>:<button onClick={()=>onOpenPage?.(plan.rootPageId)}>Open capture</button>}</div>}
  <hr/><ClipperPanel workspace={workspace} onChanged={onChanged} onOpenPage={onOpenPage}/><hr/><h3>Approved external folders</h3><p>Files remain references. Approved roots refresh every 30 seconds while Trove is running; Refresh index scans immediately. Missing files retain their identities and page links. Secrets, binary files, symlinks, and ignored paths are excluded.</p>
  <div className="import-controls"><label><input type="checkbox" checked={repository} onChange={e=>setRepository(e.target.checked)}/>Code repository</label><button disabled={busy} onClick={choose}>Approve folder…</button></div>
  {roots.map(root=><div key={root.id} className="import-conflict"><strong>{root.path}</strong><p>{root.repository?"Repository":"Folder"} · {root.approved?"Approved":"Revoked"} · {root.lastScan||"Not scanned"}</p><button disabled={busy||!root.approved} onClick={()=>scan(root)}>Refresh index</button> <button disabled={busy} onClick={()=>act(async()=>{setSelected(root.id);setSources(await request<Source[]>("index.entries",{rootId:root.id}))})}>Inspect references</button> <button disabled={busy||!root.approved} onClick={()=>act(async()=>{await request("index.revoke",{rootId:root.id});await refresh();setHits([]);setNotice("Folder approval revoked; indexed text removed. Page reference identities remain.")})}>Revoke</button></div>)}
  {selected&&<details open><summary>External file references ({sources.length})</summary><table><thead><tr><th>Path</th><th>State</th><th>Language</th></tr></thead><tbody>{sources.map(source=><tr key={source.id}><td>{source.path}</td><td>{source.state}</td><td>{source.language}</td></tr>)}</tbody></table></details>}
  <form onSubmit={e=>{e.preventDefault();void act(async()=>setHits(await request<Hit[]>("index.search",{query})))}}><div className="import-controls"><label>Search approved files<input value={query} onChange={e=>setQuery(e.target.value)}/></label><button disabled={busy||query.length<2}>Search files</button></div></form>
  {hits.map((hit,i)=><details key={`${hit.sourceId}-${i}`}><summary>{hit.path}:{hit.chunk.startLine}–{hit.chunk.endLine} · {hit.chunk.symbol||hit.language}</summary><pre style={{whiteSpace:"pre-wrap"}}>{hit.chunk.text}</pre></details>)}
  {error&&<p role="alert" className="import-error">{error}</p>}{notice&&<p role="status">{notice}</p>}{busy&&<p role="status">Working…</p>}
 </section>
}
