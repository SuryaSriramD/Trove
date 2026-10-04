import { createContext, useContext } from 'react';
import type { NodeViewProps } from '@tiptap/react';
import { AtomBody, EditorContext } from './nodes';
import { InlineRichEditor } from './InlineRichEditor';
import { richNodes, richText } from './codec';

const ProjectionPath=createContext<string[]>([]);
// Render the actual canonical node against its owning editor. Every edit enters
// that editor's history; no independently writable copy of a block is created.
export function ProjectedBlock({id,props,label,ancestors}:{id:string;props:NodeViewProps;label:string;ancestors?:string[]}) {
 const inherited=useContext(ProjectionPath);ancestors=ancestors??inherited;
 const {readOnly}=useContext(EditorContext),editor=props.editor;
 let node:any=null;editor.state.doc.forEach(candidate=>{if(candidate.attrs.blockId===id)node=candidate;});
 if(!node)return <p>Unavailable child · {id}</p>;
 if(ancestors.includes(id)||ancestors.length>32)return <p role="alert">Cyclic or excessively deep content is preserved but cannot be expanded.</p>;
 const position=()=>{let found:number|undefined;editor.state.doc.forEach((candidate,pos)=>{if(candidate.attrs.blockId===id)found=pos;});return found;};
 const updateAttributes=(patch:any)=>{const pos=position();if(readOnly||pos===undefined)return;const current=editor.state.doc.nodeAt(pos)!;editor.view.dispatch(editor.state.tr.setNodeMarkup(pos,undefined,{...current.attrs,...patch}));};
 const projected={...props,node,getPos:position,updateAttributes} as NodeViewProps;
 const kind=node.attrs.kind,toggle=['toggle','toggle_heading'].includes(kind),collapsed=node.attrs.data.collapsed===true;
 const children:string[]=[];editor.state.doc.forEach(candidate=>{if(candidate.attrs.parentBlockId===id&&candidate.attrs.blockId)children.push(candidate.attrs.blockId);});
 return <ProjectionPath.Provider value={[...ancestors,id]}><div className={`projected-block block-${kind}`} data-projected-block-id={id}>
  {node.type.name==='troveText'?<>
   {toggle&&<button type="button" disabled={readOnly} aria-label="Toggle layout content" aria-expanded={!collapsed} onClick={()=>updateAttributes({data:{...node.attrs.data,collapsed:!collapsed}})}>▸</button>}
   <div role={kind.startsWith('heading_')?'heading':undefined} aria-level={kind.startsWith('heading_')?Number(kind.slice(-1)):undefined}>
   <InlineRichEditor label={label} value={richText(node.content.toJSON())} owner={editor} readOnly={readOnly} onChange={value=>{const pos=position();if(pos===undefined||readOnly)return;const current=editor.state.doc.nodeAt(pos)!;editor.view.dispatch(editor.state.tr.replaceWith(pos+1,pos+current.nodeSize-1,richNodes(value).map(node=>editor.schema.nodeFromJSON(node))));}}/>
   </div>
  </>:<AtomBody {...projected}/>}
  {!['columns','tab_container'].includes(kind)&&!collapsed&&children.map(child=><ProjectedBlock key={child} id={child} props={props} label={label} ancestors={[...ancestors,id]}/>)}
 </div></ProjectionPath.Provider>;
}
