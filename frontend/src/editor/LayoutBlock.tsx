import { useContext, useEffect, useRef, useState } from 'react';
import type { NodeViewProps } from '@tiptap/react';
import { closeHistory } from '@tiptap/pm/history';
import { ids } from '../api';
import { EditorContext } from './nodes';
import { ProjectedBlock } from './ProjectedBlock';
import { createLayoutChild, duplicateLayoutChild, layoutGroups, moveIntoLayout, nodeID, removeLayoutChild, setGroups, subtree, type LayoutGroup } from './layoutStructure';
import { textKinds, type NodeJSON } from './codec';
const childKinds=[...textKinds,'divider','code','mermaid','equation','table','columns','tab_container'];
export function LayoutBlock(props:NodeViewProps&{readOnly?:boolean}) {
 const {node,editor,updateAttributes,readOnly=false}=props,{workspace}=useContext(EditorContext),tabbed=node.attrs.kind==='tab_container',key=tabbed?'tabs':'columns',groups:LayoutGroup[]=node.attrs.data[key]??[],containerId=node.attrs.blockId;
 const [active,setActive]=useState(groups[0]?.id),[,refresh]=useState(0),[busy,setBusy]=useState(false),[error,setError]=useState(''),[kind,setKind]=useState('paragraph'),[remove,setRemove]=useState<string|null>(null);
 const latest=useRef({readOnly});latest.current={readOnly};
 useEffect(()=>{const changed=()=>refresh(value=>value+1);editor.on('transaction',changed);return()=>{editor.off('transaction',changed);};},[editor]);
 const current=groups.some(group=>group.id===active)?active:groups[0]?.id;
 const action=(fn:()=>void)=>{try{if(readOnly)return;fn();setError('');}catch(e){setError((e as Error).message);}};
 const commit=(doc:NodeJSON)=>{if(latest.current.readOnly||!editor.isEditable)return;const content=editor.schema.nodeFromJSON(doc).content;editor.view.dispatch(closeHistory(editor.state.tr).replaceWith(0,editor.state.doc.content.size,content));editor.view.dispatch(closeHistory(editor.state.tr));};
 const update=(items:LayoutGroup[])=>action(()=>updateAttributes({data:{...node.attrs.data,[key]:items}}));
 const allocate=async(count:number,fn:(allocated:string[])=>void)=>{if(!workspace||busy||readOnly)return;setBusy(true);try{const allocated=await ids(workspace,count);if(!latest.current.readOnly&&editor.isEditable)fn(allocated);setError('');}catch(e){setError((e as Error).message);}finally{setBusy(false);}};
 const addGroup=()=>void allocate(1,([id])=>{const doc=editor.getJSON() as NodeJSON;doc.content=doc.content?.map(n=>nodeID(n)===containerId?setGroups(n,[...layoutGroups(n),{id,...(tabbed?{title:`Tab ${layoutGroups(n).length+1}`}:{widthRatio:1}),childBlockIds:[]}]):n);commit(doc);setActive(id);});
 const addChild=(groupId:string)=>void allocate(kind==='table'?5:['columns','tab_container'].includes(kind)?3:1,allocated=>{const doc=editor.getJSON() as NodeJSON,current=doc.content?.find(n=>nodeID(n)===containerId);if(!current)throw Error('The container was removed.');const children=createLayoutChild(kind,allocated,containerId,(current.attrs?.depth??0)+1);commit(moveIntoLayout({...doc,content:[...(doc.content??[]),...children]},containerId,groupId,allocated[0]));});
 const removeGroup=(id:string)=>action(()=>{if(groups.length<2)return;const source=groups.find(group=>group.id===id)!,target=groups.find(group=>group.id!==id)!;const doc=editor.getJSON() as NodeJSON;doc.content=doc.content?.map(n=>nodeID(n)===containerId?setGroups(n,groups.filter(group=>group.id!==id).map(group=>group.id===target.id?{...group,childBlockIds:[...group.childBlockIds,...source.childBlockIds]}:group)):n);commit(doc);setActive(target.id);});
 const all=(editor.getJSON() as NodeJSON).content??[],ownBranch=new Set(subtree(all,containerId).map(nodeID)),ancestors=new Set<string>();let ancestor=node.attrs.parentBlockId;while(ancestor&&!ancestors.has(ancestor)){ancestors.add(ancestor);ancestor=all.find(n=>nodeID(n)===ancestor)?.attrs?.parentBlockId;}
 const available=all.filter(n=>n.attrs?.blockId&&!ownBranch.has(nodeID(n))&&!ancestors.has(nodeID(n)));
 return <div className="advanced-layout">
  <div className="layout-structure"><button type="button" disabled={readOnly||busy} onClick={addGroup}>Add {tabbed?'tab':'column'}</button>{error&&<p role="alert">{error}</p>}</div>
  <div role={tabbed?'tablist':undefined} aria-label={tabbed?'Content tabs':'Columns'}>{groups.map((group,i)=><button type="button" key={group.id} role={tabbed?'tab':undefined} aria-selected={tabbed?group.id===current:undefined} onClick={()=>setActive(group.id)}>{group.title??`Column ${i+1}`}</button>)}</div>
  <div className={tabbed?'tab-panels':'column-panels'}>
   {groups.filter(group=>!tabbed||group.id===current).map(group=>{const index=groups.findIndex(item=>item.id===group.id),label=group.title??'Column';return <section key={group.id} role={tabbed?'tabpanel':undefined} data-layout-group={group.id} style={!tabbed?{flex:group.widthRatio}:undefined}>
    <label>{tabbed?'Tab title':'Width ratio'}<input disabled={readOnly} aria-label={tabbed?'Tab title':`Column ${index+1} width`} type={tabbed?'text':'number'} min="0.1" step="0.1" value={tabbed?group.title:group.widthRatio} onChange={event=>update(groups.map(item=>item.id===group.id?{...item,[tabbed?'title':'widthRatio']:tabbed?event.target.value:Math.max(.1,Number(event.target.value)||1)}:item))}/></label>
    <button type="button" disabled={readOnly||groups.length<2} onClick={()=>removeGroup(group.id)}>Remove {tabbed?'tab':'column'} and keep content</button>
    {group.childBlockIds.map(id=><div key={id} className="layout-child">
     <ProjectedBlock id={id} props={props} label={`${label} content`}/>
     <div className="layout-child-actions"><label>Move to<select aria-label={`Move layout block ${id}`} disabled={readOnly} value={group.id} onChange={event=>action(()=>commit(moveIntoLayout(editor.getJSON() as NodeJSON,containerId,event.target.value,id)))}>{groups.map((item,i)=><option key={item.id} value={item.id}>{item.title??`Column ${i+1}`}</option>)}</select></label>
      <button type="button" disabled={readOnly||busy} onClick={()=>void allocate(subtree(editor.getJSON().content??[],id).length,allocated=>commit(duplicateLayoutChild(editor.getJSON() as NodeJSON,containerId,group.id,id,allocated)))}>Duplicate layout block</button>
      <button type="button" disabled={readOnly} onClick={()=>setRemove(id)}>Remove layout block</button>
     </div>
    </div>)}
    {!readOnly&&<fieldset className="layout-add"><legend>Add content to {tabbed?'tab':'column'}</legend><label>Block type<select aria-label="Layout block type" value={kind} onChange={event=>setKind(event.target.value)}>{childKinds.map(type=><option key={type} value={type}>{type.replaceAll('_',' ')}</option>)}</select></label><button type="button" disabled={busy} onClick={()=>addChild(group.id)}>Add layout block</button><label>Move existing page block<select aria-label="Move existing page block" value="" onChange={event=>event.target.value&&action(()=>commit(moveIntoLayout(editor.getJSON() as NodeJSON,containerId,group.id,event.target.value)))}><option value="">Choose a block (including attachments)</option>{available.map(n=><option key={nodeID(n)} value={nodeID(n)}>{n.attrs?.kind} · {n.attrs?.data?.source??n.content?.map(t=>t.text??'').join('')??nodeID(n)}</option>)}</select></label></fieldset>}
   </section>;})}
  </div>
  {remove&&<div role="alertdialog" aria-label="Remove layout content"><p>Remove this block and its nested content? Undo restores it.</p><button type="button" onClick={()=>setRemove(null)}>Keep layout content</button><button type="button" disabled={readOnly} onClick={()=>action(()=>{commit(removeLayoutChild(editor.getJSON() as NodeJSON,remove));setRemove(null);})}>Remove nested content</button></div>}
 </div>;
}
