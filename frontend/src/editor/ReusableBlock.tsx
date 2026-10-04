import { useContext, useEffect, useState } from 'react';
import { useEditor, type NodeViewProps } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import { dispatch, envelope, type D, type S, type WorkspaceSnapshot } from '../api';
import { EditorContext, TroveAtom, TroveText } from './nodes';
import { TextColor, TextHighlight } from './ColorMarks';
import { ProjectedBlock } from './ProjectedBlock';
import { subtree } from './layoutStructure';
import { toDocument, type NodeJSON } from './codec';

function RichSource({document,sourceId,pageId}:{document:NodeJSON;sourceId:string;pageId:string}) {
 const context=useContext(EditorContext);
 const editor=useEditor({shouldRerenderOnTransaction:true,extensions:[StarterKit.configure({paragraph:false,heading:false,blockquote:false,bulletList:false,orderedList:false,listItem:false,codeBlock:false,horizontalRule:false,trailingNode:false,undoRedo:false,link:{openOnClick:false,protocols:['https','http','mailto','trove']}}),TextColor,TextHighlight,TroveText,TroveAtom],content:document,editable:false});
 useEffect(()=>{if(editor&&JSON.stringify(editor.getJSON())!==JSON.stringify(document))editor.commands.setContent(document,{emitUpdate:false});},[editor,document]);
 if(!editor)return null;
 return <EditorContext.Provider value={{...context,pageId,readOnly:true}}><fieldset disabled className="reusable-rich-content"><ProjectedBlock id={sourceId} label="Reusable source content" props={{editor} as NodeViewProps}/></fieldset></EditorContext.Provider>;
}
export function ReusableBlock({workspace,sourceId,props,openPage}:{workspace:WorkspaceSnapshot|null;sourceId:string;props:NodeViewProps;openPage:(id:string)=>void}) {
 const {pageId}=useContext(EditorContext),[remote,setRemote]=useState<{source:D.Block;document:NodeJSON}|null>(null),[status,setStatus]=useState('Loading canonical source…'),[,refresh]=useState(0);
 const nodes=(props.editor.getJSON() as NodeJSON).content??[],local=subtree(nodes,sourceId),isLocal=local.length>0;
 useEffect(()=>{const changed=()=>refresh(value=>value+1);props.editor.on('transaction',changed);return()=>{props.editor.off('transaction',changed);};},[props.editor]);
 useEffect(()=>{let active=true;setRemote(null);if(isLocal||!workspace)return;setStatus('Loading canonical source…');
  // Resolve one authorized source, then its scoped owning page for real nested
  // descendants. This never scans a global workspace block snapshot.
  void dispatch<{block:D.Block}>('readBlock',{...envelope(workspace),blockId:sourceId}).then(async({block})=>{
   const page=await dispatch<S.ReadPageResult>('readPage',{...envelope(workspace),pageId:block.pageId});
   const all=toDocument(page.blocks),branch=subtree(all.content??[],sourceId);if(!branch.length)throw Error('Unavailable source');
   const depth=branch[0].attrs?.depth??0,document={type:'doc',content:branch.map(node=>({...node,attrs:{...node.attrs,depth:(node.attrs?.depth??0)-depth}}))};
   if(active)setRemote({source:block,document});
  }).catch(()=>{if(active)setStatus('Source is unavailable or outside this view. The reference is preserved.');});
  return()=>{active=false;};
 },[workspace?.workspaceId,workspace?.scopeGrantId,workspace?.snapshotCursor,sourceId,isLocal]);
 const depth=local[0]?.attrs?.depth??0,document=isLocal?{type:'doc',content:local.map(node=>({...node,attrs:{...node.attrs,depth:(node.attrs?.depth??0)-depth}}))}:remote?.document,targetPage=isLocal?pageId:remote?.source.pageId;
 return <div className="synced-card"><small>Reusable block · live canonical source</small>{document&&targetPage?<RichSource document={document} sourceId={sourceId} pageId={targetPage}/>:<p>{status}</p>}{targetPage&&<button type="button" onClick={()=>openPage(targetPage)}>Edit source</button>}</div>;
}
