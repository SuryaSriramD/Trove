import {useEffect,useState} from 'react';
import type {D,WorkspaceSnapshot} from '../api';
import {applyTemplate,references,saveTemplate,templates,type Reference,type Template} from './service';
export function AdvancedPanel({workspace:w,page,flush,onCommitted,onError,openPage}:{workspace:WorkspaceSnapshot;page:D.Page;flush:()=>Promise<boolean>;onCommitted:()=>Promise<void>;onError:(e:unknown)=>void;openPage:(id:string)=>void}){
 const [library,setLibrary]=useState<Template[]>([]),[refs,setRefs]=useState<Reference[]>([]),[collection,setCollection]=useState('');
 const refresh=()=>Promise.all([templates(w).then(x=>setLibrary(x.templates)),references(w,page.id).then(x=>setRefs(x.references))]).catch(onError);
 useEffect(()=>{void refresh()},[page.id,page.revision]);
 return <details className="advanced-panel"><summary>Templates and related pages</summary>
 <section><h3>Template library</h3><button onClick={()=>void(async()=>{if(!await flush())return;const name=prompt('Template name',page.title);if(!name)return;await saveTemplate(w,page,name);await refresh()})().catch(onError)}>Save this page as a template</button>
 <label>Destination <select value={collection} onChange={e=>setCollection(e.target.value)}><option value="">New page</option>{w.collections.map(c=><option key={c.id} value={c.id}>{c.title}</option>)}</select></label>
 {library.map(t=><button key={t.id} onClick={()=>void(async()=>{if(!await flush())return;const title=prompt('New page title',t.name);if(!title)return;const id=await applyTemplate(w,t.id,title,collection||undefined);await onCommitted();openPage(id)})().catch(onError)}>Use {t.name}</button>)}{!library.length&&<p>No saved templates yet.</p>}</section>
 <section><h3>Unlinked references and related pages</h3>{refs.map(r=><p key={r.pageId}><button onClick={()=>openPage(r.pageId)}>{r.title}</button> <small>{r.kind} · {r.reason}</small></p>)}{!refs.length&&<p>No references found in the current scope.</p>}</section></details>
}
