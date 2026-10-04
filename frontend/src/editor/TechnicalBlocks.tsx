import { useState } from "react";
import type { Editor } from "@tiptap/core";
import type { D } from "../api";
import { fileURL } from "../api";
import { InlineRichEditor } from "./InlineRichEditor";
import { highlightedLines } from "./codePresentation";

export function CodeBlock({ data, update, owner, readOnly, onError }: {
  data: D.block_code_v1; update: (patch: Partial<D.block_code_v1>) => void; owner: Editor; readOnly: boolean; onError: (error: unknown) => void;
}) {
  const [copied,setCopied] = useState(false);
  const lines = highlightedLines(data.source, data.language);
  return <div className="technical-code">
    <div className="code-heading"><label>Language<input aria-label="Code language" disabled={readOnly} value={data.language} onChange={event=>update({language:event.target.value})}/></label>
      <label>Filename<input aria-label="Code filename" disabled={readOnly} placeholder="Optional filename" value={data.filename??""} onChange={event=>update({filename:event.target.value})}/></label>
      <button type="button" aria-label="Copy code" onClick={()=>void navigator.clipboard.writeText(data.source).then(()=>setCopied(true)).catch(onError)}>{copied?"Copied":"Copy code"}</button>
      <label><input type="checkbox" aria-label="Wrap code" disabled={readOnly} checked={data.wrap??false} onChange={event=>update({wrap:event.target.checked})}/>Wrap</label>
      <label><input type="checkbox" aria-label="Code line numbers" disabled={readOnly} checked={data.lineNumbers??false} onChange={event=>update({lineNumbers:event.target.checked})}/>Line numbers</label>
    </div>
    <pre className={`code-preview ${data.wrap?"code-wrap":"code-nowrap"}`} aria-label={data.filename?`Code ${data.filename}`:"Code preview"}>
      {lines.map((html,index)=><span className="code-line" key={index}>{data.lineNumbers&&<span className="code-line-number" aria-hidden="true">{index+1}</span>}<code dangerouslySetInnerHTML={{__html:html||"&#8203;"}}/>{index<lines.length-1?"\n":null}</span>)}
    </pre>
    <details><summary>Edit source</summary><textarea aria-label="Code source" spellCheck={false} disabled={readOnly} value={data.source} onChange={event=>{setCopied(false);update({source:event.target.value});}}/></details>
    {(data.caption?.length??0)>0&&<InlineRichEditor label="Code caption" value={data.caption??[]} onChange={caption=>update({caption})} owner={owner} readOnly={readOnly}/>}
  </div>;
}
export function ImageBlock({ data, update, originalName, readOnly }: {
  data: D.block_image_v1; update: (patch: Partial<D.block_image_v1>) => void; originalName?: string; readOnly: boolean;
}) {
  const [zoom,setZoom]=useState(false),[size,setSize]=useState({width:0,height:0});
  const alt = data.altText??originalName??"Attached image", url=fileURL(data.fileId);
  return <div className="managed-image">
    <div className="managed-image-preview" style={{justifyContent:data.alignment==="center"?"center":data.alignment==="right"?"flex-end":"flex-start"}}>
      <button type="button" className="image-zoom-trigger" aria-label={`Zoom ${alt}`} style={{width:data.width?`${data.width}px`:undefined}} onClick={()=>setZoom(true)}>
        <img src={url} alt={alt} onLoad={event=>setSize({width:event.currentTarget.naturalWidth,height:event.currentTarget.naturalHeight})}/>
      </button>
    </div>
    <div className="image-view-actions"><button type="button" onClick={()=>setZoom(true)}>Zoom image</button><a href={url} target="_blank" rel="noopener noreferrer">Open original image</a>{size.width>0&&<small>Original {size.width} × {size.height}</small>}</div>
    <details><summary>Image display</summary><fieldset disabled={readOnly}>
      <label>Width in pixels<input type="number" aria-label="Image width in pixels" min="40" max="4800" value={data.width??(size.width||640)} onChange={event=>update({width:Math.max(40,Math.min(4800,Number(event.target.value)||40))})}/></label>
      <label>Alignment<select aria-label="Image alignment" value={data.alignment??"left"} onChange={event=>update({alignment:event.target.value as D.block_image_v1["alignment"]})}><option value="left">Left</option><option value="center">Center</option><option value="right">Right</option></select></label>
      {size.width>0&&<button type="button" onClick={()=>update({width:size.width})}>Use original width</button>}
    </fieldset></details>
    {zoom&&<dialog open className="image-lightbox" aria-label="Image zoom" onKeyDown={event=>{if(event.key==="Escape"){event.preventDefault();event.stopPropagation();setZoom(false);}}}>
      <div><button type="button" autoFocus onClick={()=>setZoom(false)}>Close image zoom</button><a href={url} target="_blank" rel="noopener noreferrer">Open original image</a></div><img src={url} alt={alt}/>
    </dialog>}
  </div>;
}
