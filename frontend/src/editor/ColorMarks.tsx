import { Mark, type Editor } from "@tiptap/core";
import { useEditorState } from "@tiptap/react";
import type { EditorView } from "@tiptap/pm/view";
import type { Slice } from "@tiptap/pm/model";
import { background, foreground, colorToken, colorTokens, cssColorToken } from "./palette";

function elementToken(element: HTMLElement, highlight: boolean) {
  return colorToken(element.getAttribute(highlight?"data-trove-highlight":"data-trove-color"))
    ?? cssColorToken(highlight?element.style.backgroundColor:element.style.color,highlight)
    ?? (highlight&&element.tagName==="MARK"?"yellow":null);
}
const colorMark=(highlight:boolean)=>Mark.create({
  name:highlight?"textHighlight":"textColor",
  addAttributes(){return {token:{default:null,parseHTML:element=>elementToken(element,highlight),renderHTML:()=>({})}};},
  parseHTML(){return [{consuming:false,tag:highlight?'[data-trove-highlight], [style], mark':'[data-trove-color], [style]',getAttrs:(element:HTMLElement)=>elementToken(element,highlight)?null:false}];},
  renderHTML({mark}){
    const token=colorToken(mark.attrs.token);
    return ["span",token?{[highlight?"data-trove-highlight":"data-trove-color"]:token,style:`${highlight?"background-color":"color"}:${(highlight?background:foreground)[token]}`}:{},0];
  },
});
export const TextColor=colorMark(false),TextHighlight=colorMark(true);
export function ColorControls({editor,label="Text"}:{editor:Editor;label?:string}) {
  const active=useEditorState({editor,selector:({editor})=>({color:editor.getAttributes("textColor").token??"",highlight:editor.getAttributes("textHighlight").token??""})});
  return <>{(["color","highlight"] as const).map(kind=><label className="color-control" key={kind}><span>{kind==="color"?"Color":"Highlight"}</span><select aria-label={`${label} ${kind}`} value={active[kind]} onChange={event=>{
    const mark=kind==="color"?"textColor":"textHighlight",token=colorToken(event.target.value);
    if(token)editor.chain().focus().setMark(mark,{token}).run();else editor.chain().focus().unsetMark(mark).run();
  }}><option value="">Default</option>{colorTokens.map(token=><option key={token} value={token}>{token}</option>)}</select></label>)}</>;
}
function unsupportedStyle(html: string): boolean {
  const root=new DOMParser().parseFromString(html,"text/html");
  return [...root.querySelectorAll<HTMLElement>("[style],[data-trove-color],[data-trove-highlight]")].some(element=>{
    const declarations=element.getAttribute("style")??"";
    return (["color","highlight"] as const).some(kind=>{
      const value=element.getAttribute(`data-trove-${kind}`);
      if(value!==null&&!colorToken(value))return true;
      const property=kind==="color"?"color":"background(?:-color)?";
      const raw=new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`,"i").exec(declarations)?.[1];
      return raw!==undefined&&!cssColorToken(raw,kind==="highlight");
    });
  });
}
export function preserveStyledPaste(owner?:()=>Editor) {
  return (view:EditorView,event:ClipboardEvent,slice:Slice):boolean=>{
    const html=event.clipboardData?.getData("text/html");
    if(!html||!unsupportedStyle(html))return false;
    const editor=owner?.(),target=editor?.view??view;
    const node=target.state.schema.nodes.troveAtom.create({kind:"unsupported_external",blockId:null,clientKey:crypto.randomUUID(),depth:0,parentBlockId:null,version:1,
      data:{sourceType:"clipboard_html",sourceVersion:1,sourceMediaType:"text/html",raw:{html},readableText:"Unsupported pasted colors are retained in the original HTML.",reason:"unsupported_behavior"}});
    if(editor){target.dispatch(target.state.tr.insert(target.state.doc.content.size,node));return false;}
    const tr=view.state.tr.replaceSelection(slice);tr.insert(tr.doc.content.size,node);tr.setMeta("paste",true);view.dispatch(tr.scrollIntoView());return true;
  };
}
