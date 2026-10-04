import { textKinds, type NodeJSON } from './codec.ts';
export type LayoutGroup = {id:string;title?:string;icon?:string;widthRatio?:number;childBlockIds:string[]};
export const nodeID=(node:NodeJSON)=>node.attrs?.blockId??node.attrs?.clientKey;
export function subtree(nodes:NodeJSON[],id:string):NodeJSON[] {
 const index=nodes.findIndex(node=>nodeID(node)===id);if(index<0)return [];
 const depth=nodes[index].attrs?.depth??0;let end=index+1;while(end<nodes.length&&(nodes[end].attrs?.depth??0)>depth)end++;
 return nodes.slice(index,end);
}
export function layoutGroups(node:NodeJSON):LayoutGroup[] {return node.attrs?.data[node.attrs.kind==='tab_container'?'tabs':'columns']??[];}
export function setGroups(node:NodeJSON,groups:LayoutGroup[]):NodeJSON {return {...node,attrs:{...node.attrs,data:{...node.attrs?.data,[node.attrs?.kind==='tab_container'?'tabs':'columns']:groups}}};}
export function moveIntoLayout(document:NodeJSON,containerId:string,groupId:string,childId:string):NodeJSON {
 const nodes=structuredClone(document.content??[]),branch=subtree(nodes,childId),container=nodes.find(node=>nodeID(node)===containerId);
 if(!container||!branch.length||branch.some(node=>nodeID(node)===containerId))throw Error('A layout cannot be moved into itself or its descendant.');
 if(!layoutGroups(container).some(group=>group.id===groupId))throw Error('The destination group no longer exists.');
 const moved=new Set(branch.map(nodeID));
 const remaining=nodes.filter(node=>!moved.has(nodeID(node))).map(node=>['columns','tab_container'].includes(node.attrs?.kind)?setGroups(node,layoutGroups(node).map(group=>({...group,childBlockIds:group.childBlockIds.filter(id=>id!==childId)}))):node);
 const target=remaining.find(node=>nodeID(node)===containerId)!,depth=(target.attrs?.depth??0)+1,delta=depth-(branch[0].attrs?.depth??0);
 const copies=branch.map((node,i)=>({...node,attrs:{...node.attrs,depth:(node.attrs?.depth??0)+delta,...(i?{}:{parentBlockId:containerId})}}));
 const targetIndex=remaining.indexOf(target);remaining[targetIndex]=setGroups(target,layoutGroups(target).map(group=>group.id===groupId?{...group,childBlockIds:[...group.childBlockIds,childId]}:group));
 const last=subtree(remaining,containerId).at(-1)!;remaining.splice(remaining.indexOf(last)+1,0,...copies);
 return {...document,content:remaining};
}
export function removeLayoutChild(document:NodeJSON,id:string):NodeJSON {
 const removed=new Set(subtree(document.content??[],id).map(nodeID));
 return {...document,content:(document.content??[]).filter(node=>!removed.has(nodeID(node))).map(node=>['columns','tab_container'].includes(node.attrs?.kind)?setGroups(node,layoutGroups(node).map(group=>({...group,childBlockIds:group.childBlockIds.filter(id=>!removed.has(id))}))):node)};
}
export function duplicateLayoutChild(document:NodeJSON,containerId:string,groupId:string,id:string,allocated:string[]):NodeJSON {
 const branch=subtree(document.content??[],id);if(allocated.length<branch.length)throw Error('The source changed before duplication. Try again.');
 const mapping=new Map(branch.map((node,i)=>[nodeID(node),allocated[i]]));
 const remap=(value:any,key=''):any=>Array.isArray(value)?value.map(item=>remap(item,key)):value&&typeof value==='object'?Object.fromEntries(Object.entries(value).map(([k,v])=>[k,remap(v,k)])):typeof value==='string'&&['childBlockIds','contentSourceId'].includes(key)?mapping.get(value)??value:value;
 const copies=branch.map(node=>({...structuredClone(node),attrs:{...node.attrs,blockId:mapping.get(nodeID(node)),clientKey:crypto.randomUUID(),parentBlockId:mapping.get(node.attrs?.parentBlockId)??node.attrs?.parentBlockId,data:remap(node.attrs?.data)}}));
 return moveIntoLayout({...document,content:[...(document.content??[]),...copies]},containerId,groupId,allocated[0]);
}
export function createLayoutChild(kind:string,allocated:string[],parent:string,depth:number):NodeJSON[] {
 const [id,a,b,c,d]=allocated,attrs={blockId:id,clientKey:crypto.randomUUID(),parentBlockId:parent,depth,kind,version:1};let data:any={};
 if(textKinds.includes(kind))return [{type:'troveText',attrs:{...attrs,data:{richText:[],...(kind==='callout'?{icon:'💡',backgroundToken:'yellow'}:{})}},content:[]}];
 if(kind==='code')data={source:'// Your code here',language:'javascript',wrap:true};
 else if(kind==='equation')data={latex:'E = mc^2',display:true};
 else if(kind==='mermaid')data={source:'flowchart LR\n  Idea --> Create',renderMode:'inert_svg'};
 else if(kind==='table')data={rowCount:2,columnCount:2,headerRows:1,headerColumns:0,columnWidths:[160,160],cells:[a,b,c,d].map((id,i)=>({id,row:Math.floor(i/2),column:i%2,rowSpan:1,colSpan:1,richText:[],preMergeCells:[]}))};
 else if(kind==='columns'||kind==='tab_container')data={[kind==='columns'?'columns':'tabs']:[a,b].map((id,i)=>({id,...(kind==='columns'?{widthRatio:1}:{title:`Tab ${i+1}`}),childBlockIds:[]}))};
 else if(kind!=='divider')throw Error('Choose a supported child type, or move an existing page block.');
 return [{type:'troveAtom',attrs:{...attrs,data}}];
}
