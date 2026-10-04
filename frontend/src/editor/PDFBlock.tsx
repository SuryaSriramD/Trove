import { useEffect, useRef, useState } from "react";
import type { PDFDocumentProxy, RenderTask } from "pdfjs-dist";
import { fileURL } from "../api";
import pdfWorkerURL from "pdfjs-dist/legacy/build/pdf.worker.min.mjs?url";

export function PDFBlock({fileId,title,previewPage=1,readOnly,onPageChange}:{fileId:string;title:string;previewPage?:number;readOnly:boolean;onPageChange:(page:number)=>void}) {
  const [document,setDocument]=useState<PDFDocumentProxy|null>(null),[page,setPage]=useState(previewPage),[scale,setScale]=useState(1),[error,setError]=useState(""),[text,setText]=useState(""),[busy,setBusy]=useState(true);
  const canvas=useRef<HTMLCanvasElement>(null),annotationMode=useRef<number>(2);
  useEffect(()=>{setPage(previewPage);},[previewPage]);
  useEffect(()=>{
    let active=true;let destroy:(()=>Promise<void>)|undefined;setDocument(null);setError("");setBusy(true);
    void import("pdfjs-dist/legacy/build/pdf.mjs").then(pdfjs=>{
      if(!active)return;
      pdfjs.GlobalWorkerOptions.workerSrc=pdfWorkerURL;
      // Keep ordinary appearance annotations, but do not render interactive form fields.
      annotationMode.current=pdfjs.AnnotationMode.ENABLE_FORMS;
      const local=(path:string)=>new URL(`/pdfjs-assets/${path}/`,window.location.href).href;
      const loading=pdfjs.getDocument({url:fileURL(fileId),enableXfa:false,maxImageSize:16_000_000,canvasMaxAreaInBytes:32_000_000,useWorkerFetch:false,cMapUrl:local("cmaps"),cMapPacked:true,standardFontDataUrl:local("standard_fonts"),wasmUrl:local("wasm"),iccUrl:local("iccs")});
      destroy=()=>loading.destroy();
      return loading.promise.then(pdf=>{if(active){setDocument(pdf);setPage(current=>Math.min(pdf.numPages,Math.max(1,current)));}});
    }).catch(()=>{if(active){setError("PDF preview is unavailable. You can still open the managed original.");setBusy(false);}});
    return()=>{active=false;if(destroy)void destroy().catch(()=>{});};
  },[fileId]);
  useEffect(()=>{
    if(!document||!canvas.current)return;
    let active=true;let render:RenderTask|undefined;setBusy(true);setError("");setText("");
    void document.getPage(page).then(async pdfPage=>{
      if(!active||!canvas.current)return;
      const viewport=pdfPage.getViewport({scale}),output=Math.min(window.devicePixelRatio||1,2,Math.sqrt(8_000_000/(viewport.width*viewport.height)));
      const node=canvas.current;node.width=Math.max(1,Math.floor(viewport.width*output));node.height=Math.max(1,Math.floor(viewport.height*output));node.style.width=`${viewport.width}px`;node.style.height=`${viewport.height}px`;
      const context=node.getContext("2d");if(!context)throw Error("Canvas unavailable");
      render=pdfPage.render({canvas:node,canvasContext:context,viewport,annotationMode:annotationMode.current,transform:output===1?undefined:[output,0,0,output,0,0]});
      const [,content]=await Promise.all([render.promise,pdfPage.getTextContent()]);
      if(active){setText(content.items.map(item=>"str" in item?item.str+(item.hasEOL?"\n":" "):"").join(""));setBusy(false);}
    }).catch(()=>{if(active){setError("This PDF page could not be rendered. The original file is preserved.");setBusy(false);}});
    return()=>{active=false;render?.cancel();};
  },[document,page,scale]);
  const navigate=(next:number)=>{if(!document)return;const bounded=Math.max(1,Math.min(document.numPages,Math.floor(next)||1));setPage(bounded);if(!readOnly)onPageChange(bounded);};
  return <section className="pdf-viewer" aria-label={`PDF preview ${title}`}>
    <div className="pdf-controls"><button type="button" disabled={!document||page<=1} onClick={()=>navigate(page-1)}>Previous PDF page</button><label>Page<input type="number" aria-label="PDF page" min="1" max={document?.numPages??1} value={page} onChange={event=>navigate(Number(event.target.value))}/></label><span>of {document?.numPages??"…"}</span><button type="button" disabled={!document||page>=document.numPages} onClick={()=>navigate(page+1)}>Next PDF page</button><label>Zoom<select aria-label="PDF zoom" value={scale} onChange={event=>setScale(Number(event.target.value))}>{[.5,.75,1,1.25,1.5,2].map(value=><option key={value} value={value}>{value*100}%</option>)}</select></label><a href={fileURL(fileId)} target="_blank" rel="noopener noreferrer">Open original PDF</a></div>
    {busy&&<p role="status">Rendering PDF page…</p>}{error&&<p role="alert">{error}</p>}
    <div className="pdf-canvas-scroll"><canvas ref={canvas} aria-label={`${title}, page ${page}`} role="img"/></div>
    {text&&<details><summary>PDF page text</summary><div className="pdf-page-text">{text}</div></details>}
  </section>;
}
