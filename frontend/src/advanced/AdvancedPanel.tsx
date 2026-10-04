import {useEffect,useState} from 'react';
import type {D,WorkspaceSnapshot} from '../api';
import {references,type Reference} from './service';
import {TemplateLibrary} from './TemplateLibrary';
export function AdvancedPanel({workspace:w,page,flush,onCommitted,onError,openPage}:{workspace:WorkspaceSnapshot;page:D.Page;flush:()=>Promise<boolean>;onCommitted:()=>Promise<void>;onError:(e:unknown)=>void;openPage:(id:string)=>void}){
 const [refs,setRefs]=useState<Reference[]>([]);
 const refresh=()=>references(w,page.id).then(x=>setRefs(x.references)).catch(onError);
 useEffect(()=>{void refresh()},[page.id,page.revision]);
 return <details className="advanced-panel"><summary>Templates and related pages</summary>
 <TemplateLibrary workspace={w} sourcePages={[page]} flush={flush} onCommitted={onCommitted} onError={onError} openPage={openPage}/>
 <section><h3>Unlinked references and related pages</h3>{refs.map(r=><p key={r.pageId}><button onClick={()=>openPage(r.pageId)}>{r.title}</button> <small>{r.kind} · {r.reason}</small></p>)}{!refs.length&&<p>No references found in the current scope.</p>}</section></details>
}
