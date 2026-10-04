import {batch,dispatch,envelope,ids,pageGuards,requestID,readBlock,type D,type S,type WorkspaceSnapshot} from '../api';
import {orderedBlocks} from '../editor/codec';
export type PageLock={locked:boolean;revision:string};
export const getLock=(w:WorkspaceSnapshot,pageId:string)=>dispatch<PageLock>('editor.lock',{...envelope(w),pageId});
export const setLock=(w:WorkspaceSnapshot,p:D.Page,lock:PageLock)=>dispatch<PageLock>('editor.setLock',{...envelope(w),pageId:p.id,locked:!lock.locked,expectedRevision:lock.revision,expectedPageRevision:p.revision});
export type Template={id:string;name:string;sourcePageId:string};
export const templates=(w:WorkspaceSnapshot)=>dispatch<{templates:Template[]}>('editor.templates',envelope(w));
export const saveTemplate=(w:WorkspaceSnapshot,p:D.Page,name:string)=>dispatch('editor.saveTemplate',{...envelope(w),pageId:p.id,name,expectedPageRevision:p.revision});
export type Reference={pageId:string;title:string;kind:'unlinked'|'related';reason:string};
export const references=(w:WorkspaceSnapshot,pageId:string)=>dispatch<{references:Reference[]}>('editor.references',{...envelope(w),pageId});

// A template is immutable captured canonical content. New identities are allocated
// by the kernel; references internal to the template are remapped as a unit.
export async function applyTemplate(w:WorkspaceSnapshot,templateId:string,title:string,collectionId?:string){
 const source=await dispatch<S.ReadPageResult>('editor.template',{...envelope(w),templateId});
 const allocated=await ids(w,source.blocks.length+2),pageId=allocated[0],recordId=allocated[1];
 const remap=new Map(source.blocks.map((b,i)=>[b.id,allocated[i+2]]));remap.set(source.page.id,pageId);
 const replace=(value:any,key=''):any=>Array.isArray(value)?value.map(v=>replace(v,key)):value&&typeof value==='object'?Object.fromEntries(Object.entries(value).map(([k,v])=>[k,replace(v,k)])):typeof value==='string'&&(['childBlockIds','contentSourceId','targetPageId'].includes(key))?(remap.get(value)??value):value;
 const commands:S.Command[]=collectionId?[{kind:'record.create',recordId,pageId,collectionId,title}]:[{kind:'page.create',pageId,title}];
 const guards:S.ExpectedRevision[]=[{target:{entityType:'page',entityId:pageId},revision:'0'}];
 if(collectionId){const c=w.collections.find(c=>c.id===collectionId);if(!c)throw new Error('Collection is unavailable');guards.push({target:{entityType:'record',entityId:recordId},revision:'0'},{target:{entityType:'collection',entityId:collectionId},revision:c.revision});}
 for(const b of orderedBlocks(source.blocks)){
  commands.push({kind:'block.insert',block:{blockId:remap.get(b.id)!,pageId,...(b.parentBlockId?{parentBlockId:remap.get(b.parentBlockId)}:{}),type:b.type,schemaVersion:1,data:replace(b.data)}});
  guards.push({target:{entityType:'block',entityId:remap.get(b.id)!},revision:'0'});
 }
 await batch(w,commands,guards);return pageId;
}
export async function previewButton(w:WorkspaceSnapshot,actionDefinitionId:string){return dispatch<{label:string;commands:S.Command[]}>('editor.button',{...envelope(w),actionDefinitionId});}
export async function runButton(w:WorkspaceSnapshot,commands:S.Command[]){
 // Overguarding all visible canonical entities is intentional for reviewed button
 // actions: no race may silently rebase a stored action onto changed content.
 const guards:S.ExpectedRevision[]=[];
 for(const [key,entityType] of Object.entries({pages:'page',blocks:'block',collections:'collection',properties:'property',records:'record',views:'view',containers:'container',files:'file',meetings:'meeting',memories:'memory'})){
  for(const value of (w as any)[key]??[])guards.push({target:{entityType:entityType as S.EntityRef['entityType'],entityId:value.id},revision:value.revision});
 }
 // Shell snapshots omit block bodies. Fetch only pages actually referenced by
 // the reviewed action, preserving existing block guards without global preload.
 if(w.contentMode==='shell'){
  const inserted=new Set(commands.flatMap(c=>c.kind==='block.insert'?[c.block.blockId]:[]));
  const referenced=new Set<string>();
  for(const command of commands){const c=command as any;for(const id of [c.blockId,c.parentBlockId,c.block?.parentBlockId,c.block?.afterBlockId,...(c.orderedBlockIds??[])])if(id&&!inserted.has(id))referenced.add(id);}
  const pages=new Set<string>();
  for(const id of referenced)pages.add((await readBlock(w,id)).pageId);
  for(const pageId of pages){const content=await dispatch<S.ReadPageResult>('readPage',{...envelope(w),pageId});for(const block of content.blocks)if(!guards.some(g=>g.target.entityType==='block'&&g.target.entityId===block.id))guards.push({target:{entityType:'block',entityId:block.id},revision:block.revision});}
 }
 const created=new Set<string>();
 for(const c of commands){const v=c as any;const add=(kind:S.EntityRef['entityType'],id:string|undefined)=>{if(id&&!guards.some(g=>g.target.entityId===id)){created.add(id);guards.push({target:{entityType:kind,entityId:id},revision:'0'});}};
  if(v.kind==='page.create')add('page',v.pageId);if(v.kind==='block.insert')add('block',v.block.blockId);
  if(v.kind==='record.create'){add('record',v.recordId);add('page',v.pageId);}
 }
 return batch(w,commands,guards);
}
