import { test, expect, type Page, type Locator } from "@playwright/test";

const id = (n: number) => `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const pageId = id(2);
function pdfFixture() {
 const first="BT /F1 18 Tf 30 140 Td (First offline PDF page) Tj ET",second="BT /F1 18 Tf 30 140 Td (Second offline PDF page) Tj ET";
 const objects=["<< /Type /Catalog /Pages 2 0 R /OpenAction 8 0 R >>","<< /Type /Pages /Count 2 /Kids [3 0 R 5 0 R] >>","<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 7 0 R >> >> /Contents 4 0 R >>",`<< /Length ${first.length} >>\nstream\n${first}\nendstream`,"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>",`<< /Length ${second.length} >>\nstream\n${second}\nendstream`,"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>","<< /S /JavaScript /JS (globalThis.pdfScriptExecuted=true) >>"];
 let body="%PDF-1.7\n";const offsets=[0];objects.forEach((object,index)=>{offsets.push(Buffer.byteLength(body));body+=`${index+1} 0 obj\n${object}\nendobj\n`;});const xref=Buffer.byteLength(body);body+=`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(offset=>`${String(offset).padStart(10,"0")} 00000 n \n`).join("")+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;
 return Buffer.from(body);
}

const rich = [{ text: "Keep ", marks: ["bold"], color:"purple", highlight:"yellow" }, { text: "linked", link: "https://example.test/source" }, { text: " tail" }];
const block = (n: number, type: string, data: unknown, parent: number | null = null) => ({
  id: id(n), pageId, parentBlockId: parent === null ? null : id(parent), sortKey: String(n).padStart(12, "0"), type, data, schemaVersion: 1, revision: "1",
});
const initial = {
  contractVersion: "1.0.0", workspaceId: id(1), scopeGrantId: id(3), snapshotCursor: "1",
  pages: [{id: pageId,workspaceId:id(1),parentPageId:null,title:"Editor integrity",revision:"1",createdAt:"2026-10-04T10:00:00Z",updatedAt:"2026-10-04T10:00:00Z",deletedAt:null,icon:null}],
  blocks: [
    block(10,"table",{rowCount:1,columnCount:2,headerRows:1,headerColumns:0,columnWidths:[160,200],cells:[
      {id:id(11),row:0,column:0,rowSpan:1,colSpan:2,richText:rich,preMergeCells:[{id:id(11),row:0,column:0,rowSpan:1,colSpan:1,richText:rich,preMergeCells:[]},{id:id(12),row:0,column:1,rowSpan:1,colSpan:1,richText:[{text:"Original retained cell"}],preMergeCells:[]}]},
    ]}),
    block(20,"columns",{columns:[{id:id(23),widthRatio:1,childBlockIds:[id(21)]},{id:id(24),widthRatio:1,childBlockIds:[id(22)]}]}),
    block(21,"paragraph",{richText:rich},20), block(22,"paragraph",{richText:[{text:"Other column"}]},20),
    block(30,"toggle",{richText:[{text:"Outer toggle"}],collapsed:false}),
    block(31,"toggle_heading",{richText:[{text:"Nested toggle"}],collapsed:true},30),
    block(32,"paragraph",{richText:[{text:"Hidden grandchild"}]},31),
    block(33,"code",{source:"const retained = 42",language:"javascript"},31),
    block(34,"bulleted_list_item",{richText:[{text:"Direct child"}]},30),
    block(40,"paragraph",{richText:[{text:"Visible sibling"}]}),
    block(41,"code",{source:'/* Multi\ncomment */\nconst text = "<script>🌱";\n',language:"javascript",filename:"imported.js",wrap:false,lineNumbers:true}),
    block(42,"image",{fileId:id(91),caption:[{text:"Managed image"}],altText:"A retained image",width:320,alignment:"right"}),
    block(43,"pdf",{fileId:id(92),caption:[{text:"Offline PDF"}],previewPage:1}),
  ],
  collections:[],properties:[],records:[],views:[],files:[],meetings:[],taskFacets:[],containers:[],containerCollections:[],transcriptSegments:[],projectScopes:[],memories:[],importMappings:[],
};
test.beforeEach(async ({page}) => {
  await page.addInitScript(({initial}) => {
    if(window!==window.top)return;
    const state = JSON.parse(localStorage.getItem("editor-integrity") ?? "null") ?? initial;
    let sequence = 500;
    const makeID = () => `019a1234-5678-7000-8000-${String(++sequence).padStart(12,"0")}`;
    const listeners = new Map<string, Set<(...args:unknown[])=>void>>();
    const persist = () => localStorage.setItem("editor-integrity",JSON.stringify(state));
    (window as any).integrityState = () => state;
    (window as any).runtime = {EventsOn(event:string,callback:(...args:unknown[])=>void) { const set=listeners.get(event)??new Set();set.add(callback);listeners.set(event,set);return()=>set.delete(callback); }};
    (window as any).go = {main:{App:{ShellReady:async()=>{},SetEditing:async()=>{},PickFile:async()=>"",Dispatch:async(method:string,raw:string)=>{
      const req=JSON.parse(raw),ok=(result:unknown)=>JSON.stringify({ok:true,result});
      if(method==="workspace.snapshot") return ok(state);
      if(method==="editor.bookmarkPreview") {state.bookmarkRequests=(state.bookmarkRequests??0)+1;if(req.consent!=="fetch_once")throw Error("Explicit consent missing");if(state.failBookmark)throw Error("Source unavailable");return ok({sourceUrl:req.url,title:"Cached <script>text</script>",description:"Safe cached description 🌱",fetchedAt:"2026-10-04T12:00:00Z"});}
      if(method==="readBlock") {const block=[...state.blocks,...(state.remoteBlocks??[])].find((block:any)=>block.id===req.blockId);return block?ok({block}):JSON.stringify({ok:false,error:{code:"NOT_FOUND",message:"Unavailable source",retryable:false,requestId:req.requestId}});}
      if(method==="readPage") return ok({page:state.pages.find((page:any)=>page.id===req.pageId)??{...state.pages[0],id:req.pageId,title:"Scoped source page"},blocks:[...state.blocks,...(state.remoteBlocks??[])].filter((block:any)=>block.pageId===req.pageId),snapshotCursor:state.snapshotCursor});
      if(method==="pageDisplay.get") return ok({pageId:req.pageId,fullWidth:false,presentation:false,revision:state.pages[0].revision});
      if(method==="editor.lock") return ok(state.lock??{locked:false,revision:"0"});
      if(method==="editor.setLock") {state.lock={locked:req.locked,revision:String(Number(req.expectedRevision)+1)};persist();return ok(state.lock);}
      if(method==="editor.templates") return ok({templates:[]});
      if(method==="editor.references") return ok({references:[]});
      if(method==="ai.runs"||method==="imports.list") return ok([]);
      if(method==="memory.list") return ok({entries:[]});
      if(method==="search.semanticSettings") return ok({enabled:false,modelPackId:"",dimension:0});
      if(method==="models.list") return ok({packs:[],catalog:[],runtime:{available:false,error:"Fixture runtime"}});
      if(method==="jobs.list") return ok({jobs:[]});
      if(method==="allocateIds") return ok({ids:Array.from({length:req.count},makeID)});
      if(method!=="batch") throw Error("Unexpected editor fixture service: "+method);
      if(state.lock?.locked) throw Error("Locked fixture page cannot be mutated");
      const revisions:any[]=[],before=new Map(state.blocks.map((b:any)=>[b.id,b.revision])),touched=new Set<string>();
      for(const guard of req.mutation.expectedRevisions) {
        const entity=[...state.pages,...state.blocks].find((item:any)=>item.id===guard.target.entityId);
        if(entity&&entity.revision!==guard.revision) return JSON.stringify({ok:false,error:{code:"CONFLICT",message:"Stale fixture revision",retryable:false,requestId:req.requestId}});
      }
      for(const command of req.commands) {
        if(command.kind==="block.update") {const b=state.blocks.find((b:any)=>b.id===command.blockId);Object.assign(b,{type:command.type,data:command.data});touched.add(b.id);}
        else if(command.kind==="block.insert") {state.blocks.push({...command.block,id:command.block.blockId,parentBlockId:command.block.parentBlockId??null,sortKey:"999999999999",revision:"0"});touched.add(command.block.blockId);}
        else if(command.kind==="block.move") {state.blocks.find((b:any)=>b.id===command.blockId).parentBlockId=command.parentBlockId;touched.add(command.blockId);}
        else if(command.kind==="block.reorder") command.orderedBlockIds.forEach((id:string,i:number)=>{state.blocks.find((b:any)=>b.id===id).sortKey=String(i*1000).padStart(12,"0");touched.add(id);});
        else if(command.kind==="block.delete") state.blocks=state.blocks.filter((b:any)=>b.id!==command.blockId);
        else if(command.kind==="page.update") Object.assign(state.pages[0],{title:command.title??state.pages[0].title});
        else throw Error("Unexpected editor mutation: "+command.kind);
      }
      for(const b of state.blocks) if(touched.has(b.id)){const old=String(before.get(b.id)??"0");b.revision=String(Number(old)+1);revisions.push({target:{entityType:"block",entityId:b.id},before:old,after:b.revision,deleted:false});}
      const p=state.pages[0],old=p.revision;p.revision=String(Number(old)+1);
      revisions.push({target:{entityType:"page",entityId:p.id},before:old,after:p.revision,deleted:false});
      state.snapshotCursor=String(Number(state.snapshotCursor)+1);persist();
      return ok({requestId:req.requestId,transactionId:makeID(),eventCursor:state.snapshotCursor,revisions,replayed:false});
    }}}};
  },{initial});
  await page.route("**/files/019a1234-5678-7000-8000-000000000091", route=>route.fulfill({contentType:"image/png",body:Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a2ioAAAAASUVORK5CYII=","base64")}));
  await page.route("**/files/019a1234-5678-7000-8000-000000000092", route=>route.fulfill({contentType:"application/pdf",body:pdfFixture()}));
  await page.goto(process.env.TROVE_TEST_URL??"http://127.0.0.1:4173");
  await page.getByRole("button",{name:/Editor integrity/}).first().click();
});
async function save(page:Page) {
  await page.getByRole("button",{name:"Save page",exact:true}).click();
  await expect(page.locator(".save-state")).toContainText("Saved");
}
async function insertCharacter(page:Page,field:Locator) {
  await field.click();
  const caret=await field.evaluate(element=>{
    const walker=document.createTreeWalker(element,NodeFilter.SHOW_TEXT);const text=walker.nextNode()!;
    const editor=(element as HTMLElement & {editor?:any}).editor;
    if(!editor)throw Error("The nested rich editor is not ready");
    // A DOM Range alone races ProseMirror's deferred focus reconciliation.
    // Update its selection transaction first, then synchronize the DOM before
    // real keyboard input. Focus reconciliation must preserve this exact caret.
    const position=editor.view.posAtDOM(text,4);
    editor.commands.setTextSelection(position);
    editor.view.focus();
    const selection=window.getSelection()!;
    return {focused:document.activeElement===element,modelFrom:editor.state.selection.from,
      modelTo:editor.state.selection.to,position,domOffset:selection.anchorOffset,
      domText:selection.anchorNode?.textContent,collapsed:selection.isCollapsed};
  });
  expect(caret).toEqual({focused:true,modelFrom:caret.position,modelTo:caret.position,
    position:caret.position,domOffset:4,domText:"Keep ",collapsed:true});
  await page.keyboard.insertText("!");
}
async function persistedRich(page:Page,kind:"table"|"layout") {
  return page.evaluate(kind=>{
    const blocks=(window as any).integrityState().blocks;
    return kind==="table"?blocks.find((b:any)=>b.type==="table").data.cells[0].richText:blocks.find((b:any)=>b.id.endsWith("000000000021")).data.richText;
  },kind);
}
for(const kind of ["table","layout"] as const) test(`actual ${kind} editor preserves links and marks through one-character edit, outer undo and reload`,async({page})=>{
  const field=page.getByRole("textbox",{name:kind==="table"?"Row 1 column 1":"Column content",exact:true}).first();
  await expect(field.locator("strong")).toHaveText("Keep ");
  await insertCharacter(page,field);
  await expect(field).toContainText("Keep! linked tail");
  await field.press("ControlOrMeta+z");
  await expect(field).toContainText("Keep linked tail");
  await expect(field).not.toContainText("Keep!");
  await field.press("ControlOrMeta+Shift+z");
  await expect(field).toContainText("Keep! linked tail");
  await save(page);
  const value=await persistedRich(page,kind);
  expect(value.map((run:any)=>run.text).join("")).toBe("Keep! linked tail");
  expect(value.find((run:any)=>run.text.includes("Keep"))?.marks).toContain("bold");
  expect(value.find((run:any)=>run.text.includes("Keep"))?.color).toBe("purple");
  expect(value.find((run:any)=>run.text.includes("Keep"))?.highlight).toBe("yellow");
  expect(value.find((run:any)=>run.text==="linked")?.link).toBe("https://example.test/source");
  if(kind==="table") expect(await page.evaluate(()=>(window as any).integrityState().blocks.find((b:any)=>b.type==="table").data.cells[0].preMergeCells[1].richText)).toEqual([{text:"Original retained cell"}]);
  await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
  await expect(field).toContainText("Keep! linked tail");
  await expect(field.locator('a[href="https://example.test/source"]')).toHaveText("linked");
});
test("nested toggles retain their own states, move safely, and reveal citations without writing a locked page",async({page})=>{
  const outer=page.locator(`[data-block-id="${id(30)}"]`),inner=page.locator(`[data-block-id="${id(31)}"]`),grandchild=page.locator(`[data-block-id="${id(32)}"]`),code=page.locator(`[data-block-id="${id(33)}"]`);
  await expect(inner).toBeVisible();await expect(grandchild).toBeHidden();
  const moved=page.locator(`[data-block-id="${id(34)}"]`);
  await moved.locator(".block-text").click();await page.keyboard.press("Home");await page.keyboard.press("Shift+Tab");await save(page);
  expect(await page.evaluate(id=>(window as any).integrityState().blocks.find((b:any)=>b.id===id).parentBlockId,id(34))).toBeNull();
  await outer.getByRole("button",{name:"Toggle content",exact:true}).click();
  await expect(inner).toBeHidden();await expect(grandchild).toBeHidden();await expect(code).toBeHidden();await expect(moved).toBeVisible();
  await page.getByRole("button",{name:"Undo",exact:true}).click();await expect(inner).toBeVisible();await expect(grandchild).toBeHidden();
  await page.getByRole("button",{name:"Redo",exact:true}).click();await expect(inner).toBeHidden();
  await save(page);await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
  await expect(inner).toBeHidden();
  await outer.getByRole("button",{name:"Toggle content",exact:true}).click();
  await expect(inner).toBeVisible();await expect(grandchild).toBeHidden();
  await inner.getByRole("button",{name:"Toggle content",exact:true}).click();
  await expect(grandchild).toBeVisible();await expect(code).toBeVisible();
  await outer.getByRole("button",{name:"Toggle content",exact:true}).click();await save(page);
  await page.getByRole("button",{name:"Lock page",exact:true}).click();
  const before=await page.evaluate(()=>JSON.stringify((window as any).integrityState().blocks));
  await page.evaluate(blockId=>window.dispatchEvent(new CustomEvent("trove:reveal-block",{detail:{blockId}})),id(32));
  await expect(grandchild).toBeVisible();
  expect(await page.evaluate(()=>JSON.stringify((window as any).integrityState().blocks))).toBe(before);
  await expect(page.getByRole("textbox",{name:"Row 1 column 1",exact:true})).toHaveAttribute("contenteditable","false");
  await expect(page.getByRole("textbox",{name:"Column content",exact:true}).first()).toHaveAttribute("contenteditable","false");
});

test("outer page clipboard preserves table rich data and allocates new identities",async({page,context})=>{
 await context.grantPermissions(["clipboard-read","clipboard-write"]);
 await page.locator(`[data-block-id="${id(40)}"] .block-text`).click();
 await page.keyboard.press("Meta+a");await page.keyboard.press("Meta+c");
 await page.keyboard.press("ArrowRight");await page.keyboard.press("End");
 await page.keyboard.press("Meta+v");
 await expect(page.getByRole("textbox",{name:"Row 1 column 1",exact:true})).toHaveCount(2);
 await save(page);
 const tables=await page.evaluate(()=>(window as any).integrityState().blocks.filter((b:any)=>b.type==="table"));
 expect(tables).toHaveLength(2);
 expect(new Set(tables.map((b:any)=>b.id)).size).toBe(2);
 expect(tables[1].data).toEqual(tables[0].data);
});

test("table structure controls preserve header widths, confirm populated removal, and undo through reload",async({page})=>{
 const table=page.locator(".block-table");
 await table.getByText("Table structure",{exact:true}).click();
 await table.getByRole("button",{name:"Insert row after",exact:true}).click();
 const added=table.getByRole("textbox",{name:"Row 2 column 1",exact:true});await expect(added).toBeVisible();
 await added.fill("Retain this cell");
 await table.getByRole("spinbutton",{name:"Header columns",exact:true}).fill("1");
 await table.getByRole("spinbutton",{name:"Column 1 width in pixels",exact:true}).fill("220");
 await expect(table.locator('th[scope="row"]')).toHaveCount(1);
 await expect(table.locator("col").first()).toHaveCSS("width","220px");
 await table.getByRole("combobox",{name:"Selected table row",exact:true}).selectOption("1");
 await table.getByRole("button",{name:"Remove row",exact:true}).click();
 const confirm=table.getByRole("alertdialog",{name:"Remove table row"});await expect(confirm).toBeVisible();
 await expect(added).toHaveText("Retain this cell");await confirm.getByRole("button",{name:"Keep cells",exact:true}).click();
 await table.getByRole("button",{name:"Remove row",exact:true}).click();await confirm.getByRole("button",{name:"Remove populated row",exact:true}).click();
 await expect(added).toHaveCount(0);await page.getByRole("button",{name:"Undo",exact:true}).click();await expect(added).toHaveText("Retain this cell");
 await save(page);await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
 await expect(added).toHaveText("Retain this cell");await expect(table.locator('th[scope="col"]')).toHaveCount(1);await expect(table.locator('th[scope="row"]')).toHaveCount(1);
 const data=await page.evaluate(()=>(window as any).integrityState().blocks.find((b:any)=>b.type==="table").data);
 expect(data.rowCount).toBe(2);expect(data.columnWidths).toEqual([220,200]);expect(data.cells[0].preMergeCells[1].richText).toEqual([{text:"Original retained cell"}]);
});

test("code controls copy exact source bytes and persist filename, wrapping and line numbers",async({page,context})=>{
 await context.grantPermissions(["clipboard-read","clipboard-write"]);
 const code=page.locator(`[data-block-id="${id(41)}"]`),source=initial.blocks.find(b=>b.id===id(41))!.data as any;
 await expect(code.locator(".code-line-number")).toHaveCount(4);
 await expect(code.locator(".technical-code")).toHaveAttribute("data-highlighter","shiki");
 await expect(code.locator(".code-line").first().locator(".syntax-token")).toContainText("/* Multi");
 await expect(code.locator(".code-line").nth(1).locator(".syntax-token")).toContainText("comment */");
 expect(await code.locator(".code-line").first().locator(".syntax-token").first().evaluate(element=>element.getAttribute("style"))).toBeTruthy();
 await code.getByRole("button",{name:"Copy code",exact:true}).click();
 expect(await page.evaluate(()=>navigator.clipboard.readText())).toBe(source.source);
 await code.getByRole("textbox",{name:"Code filename",exact:true}).fill("retained.js");
 await code.getByRole("checkbox",{name:"Wrap code",exact:true}).check();
 await code.getByRole("checkbox",{name:"Code line numbers",exact:true}).uncheck();
 await expect(code.locator(".code-line-number")).toHaveCount(0);await expect(code.locator(".code-line code").first()).toHaveCSS("white-space","pre-wrap");
 await save(page);await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
 await expect(code.getByRole("textbox",{name:"Code filename",exact:true})).toHaveValue("retained.js");await expect(code.getByRole("checkbox",{name:"Wrap code",exact:true})).toBeChecked();
 expect(await page.evaluate(id=>(window as any).integrityState().blocks.find((b:any)=>b.id===id).data.source,id(41))).toBe(source.source);
});

test("managed images honor dimensions and alignment with zoom and a scoped original link",async({page})=>{
 const image=page.locator(`[data-block-id="${id(42)}"]`);
 await expect(image.locator(".image-zoom-trigger")).toHaveCSS("width","320px");await expect(image.locator(".managed-image-preview")).toHaveCSS("justify-content","flex-end");
 await image.getByText("Image display",{exact:true}).click();await image.getByRole("spinbutton",{name:"Image width in pixels",exact:true}).fill("240");await image.getByRole("combobox",{name:"Image alignment",exact:true}).selectOption("center");
 await image.getByRole("button",{name:"Zoom image",exact:true}).click();const zoom=page.getByRole("dialog",{name:"Image zoom",exact:true});await expect(zoom).toBeVisible();
 await expect(zoom.getByRole("img")).toHaveAttribute("src",`/files/${id(91)}`);await expect(zoom.getByRole("link",{name:"Open original image"})).toHaveAttribute("href",`/files/${id(91)}`);
 await zoom.getByRole("button",{name:"Close image zoom"}).click();await expect(zoom).toHaveCount(0);
 await save(page);await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
 await expect(image.locator(".image-zoom-trigger")).toHaveCSS("width","240px");await expect(image.locator(".managed-image-preview")).toHaveCSS("justify-content","center");await expect(image.locator(".image-zoom-trigger img")).toHaveAttribute("alt","A retained image");
});

test("outer text colors and highlights survive live selection, undo and reload",async({page})=>{
 const paragraph=page.locator(`[data-block-id="${id(40)}"] .block-text`);
 await paragraph.click();await page.keyboard.press("Home");await page.keyboard.press("Shift+End");
 await page.getByRole("combobox",{name:"Text color",exact:true}).selectOption("blue");
 await page.getByRole("combobox",{name:"Text highlight",exact:true}).selectOption("yellow");
 await expect(paragraph.locator('[data-trove-color="blue"]')).toContainText("Visible sibling");
 await expect(paragraph.locator('[data-trove-highlight="yellow"]')).toContainText("Visible sibling");
 await page.getByRole("button",{name:"Undo",exact:true}).click();await expect(paragraph.locator('[data-trove-highlight="yellow"]')).toHaveCount(0);
 await page.getByRole("button",{name:"Redo",exact:true}).click();await expect(paragraph.locator('[data-trove-highlight="yellow"]')).toContainText("Visible sibling");
 await save(page);await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
 await expect(paragraph.locator('[data-trove-color="blue"]')).toContainText("Visible sibling");await expect(paragraph.locator('[data-trove-highlight="yellow"]')).toContainText("Visible sibling");
});

test("known pasted styles become finite tokens while unknown CSS remains inert source evidence",async({page,context})=>{
 await context.grantPermissions(["clipboard-read","clipboard-write"]);
 const html='<p><span style="color:rgb(255,0,0);background-color:#ffff00">Known color</span> <span style="color:#123456">Unknown color</span></p>';
 await page.evaluate(async html=>navigator.clipboard.write([new ClipboardItem({"text/html":new Blob([html],{type:"text/html"}),"text/plain":new Blob(["Known color Unknown color"],{type:"text/plain"})})]),html);
 const field=page.getByRole("textbox",{name:"Row 1 column 1",exact:true});await field.click();await field.press("End");await field.press("Meta+v");
 await expect(field.locator('[data-trove-color="red"]')).toContainText("Known color");await expect(field.locator('[data-trove-highlight="yellow"]').filter({hasText:"Known color"})).toContainText("Known color");
 await expect(page.locator(".preserved")).toContainText("Unsupported pasted colors are retained");await save(page);
 const blocks=await page.evaluate(()=>(window as any).integrityState().blocks),cell=blocks.find((b:any)=>b.type==="table").data.cells[0];
 expect(cell.richText.find((run:any)=>run.text.includes("Known color"))).toMatchObject({color:"red",highlight:"yellow"});
 expect(cell.richText.find((run:any)=>run.text.includes("Unknown color"))).not.toHaveProperty("color");
 expect(blocks.find((b:any)=>b.type==="unsupported_external").data.raw.html).toContain(html);
 await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();await expect(field.locator('[data-trove-color="red"]')).toContainText("Known color");
});

test("PDF.js renders managed pages offline, remembers the page and never executes PDF JavaScript",async({page})=>{
 const pdf=page.locator(`[data-block-id="${id(43)}"]`);await pdf.scrollIntoViewIfNeeded();
 await expect(pdf.getByRole("status")).toHaveCount(0);await expect(pdf.getByRole("alert")).toHaveCount(0);
 await pdf.getByText("PDF page text",{exact:true}).click();await expect(pdf.locator(".pdf-page-text")).toContainText("First offline PDF page");
 expect(await pdf.locator("canvas").evaluate(element=>{const canvas=element as HTMLCanvasElement;return canvas.getContext("2d")!.getImageData(0,0,canvas.width,canvas.height).data.some((value,index)=>index%4!==3&&value<100);})).toBe(true);
 await pdf.getByRole("button",{name:"Next PDF page",exact:true}).click();await expect(pdf.locator(".pdf-page-text")).toContainText("Second offline PDF page");
 await pdf.getByRole("combobox",{name:"PDF zoom",exact:true}).selectOption("1.5");await expect(pdf.locator("canvas")).toHaveCSS("width","450px");
 await expect(pdf.getByRole("link",{name:"Open original PDF",exact:true})).toHaveAttribute("href",`/files/${id(92)}`);
 expect(await page.evaluate(()=>(window as any).pdfScriptExecuted)).toBeUndefined();
 expect(await page.evaluate(()=>performance.getEntriesByType("resource").map(entry=>entry.name).filter(url=>/^https?:/.test(url)&&!url.startsWith(location.origin)))).toEqual([]);
 await save(page);await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();await pdf.scrollIntoViewIfNeeded();
 await expect(pdf.getByRole("spinbutton",{name:"PDF page",exact:true})).toHaveValue("2");await pdf.getByText("PDF page text",{exact:true}).click();await expect(pdf.locator(".pdf-page-text")).toContainText("Second offline PDF page");
});


test("a damaged PDF reports a bounded preview error and retains the managed original",async({page})=>{
 await page.route("**/files/019a1234-5678-7000-8000-000000000092",route=>route.fulfill({contentType:"application/pdf",body:"%PDF-1.7\ncorrupted fixture"}));
 await page.reload();await page.getByRole("button",{name:/Editor integrity/}).first().click();
 const pdf=page.locator(`[data-block-id="${id(43)}"]`);await pdf.scrollIntoViewIfNeeded();
 await expect(pdf.getByRole("alert")).toContainText("PDF preview is unavailable");
 await expect(pdf.getByRole("status")).toHaveCount(0);
 await expect(pdf.getByRole("link",{name:"Open original PDF",exact:true})).toHaveAttribute("href",`/files/${id(92)}`);
 expect(await page.evaluate(()=>(window as any).integrityState().blocks.find((block:any)=>block.id.endsWith("000000000043")).data.fileId)).toBe(id(92));
});


test("rich layout controls move and edit code, tables, images and nested columns without flattening",async({page})=>{
 const layout=page.locator(`[data-block-id="${id(20)}"]`),group=layout.locator(`[data-layout-group="${id(23)}"]`);
 await group.getByRole("combobox",{name:"Move existing page block",exact:true}).selectOption(id(41));
 const code=group.locator(`[data-projected-block-id="${id(41)}"]`);await expect(code.locator('.technical-code')).toBeVisible();await code.getByText("Edit source",{exact:true}).click();await code.getByRole('textbox',{name:'Code source',exact:true}).fill('const inside = "rich🌱";');
 await group.getByRole("combobox",{name:"Move existing page block",exact:true}).selectOption(id(10));
 const table=group.locator(`[data-projected-block-id="${id(10)}"]`);const cell=table.getByRole('textbox',{name:'Row 1 column 1',exact:true});await insertCharacter(page,cell);await expect(cell.locator('strong')).toHaveText('Keep! ');
 await group.getByRole("combobox",{name:"Move existing page block",exact:true}).selectOption(id(42));const image=group.locator(`[data-projected-block-id="${id(42)}"]`);await expect(image.getByRole('img',{name:'A retained image'})).toBeVisible();await image.getByText('Image display',{exact:true}).click();await image.getByRole('combobox',{name:'Image alignment',exact:true}).selectOption('left');
 await group.getByRole('combobox',{name:'Layout block type',exact:true}).selectOption('columns');await group.getByRole('button',{name:'Add layout block',exact:true}).click();await expect(group.locator('.advanced-layout')).toHaveCount(1);
 await save(page);const values=await page.evaluate(()=>(window as any).integrityState().blocks);expect(values.find((b:any)=>b.id.endsWith('000000000041')).data.source).toBe('const inside = "rich🌱";');expect(values.find((b:any)=>b.id.endsWith('000000000010')).parentBlockId).toBe(id(20));expect(values.find((b:any)=>b.id.endsWith('000000000010')).data.cells[0].preMergeCells[1].richText[0].text).toBe('Original retained cell');
 await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();await expect(code.locator('.code-preview')).toContainText('rich🌱');await expect(table.locator('strong').first()).toHaveText('Keep! ');
});

test("layout group removal keeps content and subtree duplication/removal are undoable",async({page})=>{
 const layout=page.locator(`[data-block-id="${id(20)}"]`);await layout.getByRole('button',{name:'Add column',exact:true}).click();await expect(layout.locator(':scope > .advanced-layout > .column-panels > section')).toHaveCount(3);
 const first=layout.locator(`[data-layout-group="${id(23)}"]`);await first.getByRole('button',{name:'Duplicate layout block',exact:true}).click();await expect(first.locator('.layout-child')).toHaveCount(2);await expect(first.locator('strong')).toHaveText(['Keep ','Keep ']);
 await first.getByRole('button',{name:'Remove column and keep content',exact:true}).click();const remaining=layout.locator(`[data-layout-group="${id(24)}"]`);await expect(remaining.locator('.layout-child')).toHaveCount(3);
 await remaining.getByRole('button',{name:'Remove layout block',exact:true}).last().click();await layout.getByRole('button',{name:'Remove nested content',exact:true}).click();await expect(remaining.locator('.layout-child')).toHaveCount(2);
 const field=remaining.getByRole('textbox',{name:'Column content',exact:true}).first();await field.press('ControlOrMeta+z');await expect(remaining.locator('.layout-child')).toHaveCount(3);
 await save(page);const ids=await page.evaluate(()=>(window as any).integrityState().blocks.map((b:any)=>b.id));expect(new Set(ids).size).toBe(ids.length);
});


test("reusable blocks show rich canonical sources, update live and retain unavailable references",async({page})=>{
 await page.evaluate(({id44,id45,pageId})=>{const state=(window as any).integrityState();state.blocks.push({id:id44,pageId,parentBlockId:null,sortKey:'000000000044',type:'synced_block',schemaVersion:1,revision:'1',data:{contentSourceId:state.blocks.find((block:any)=>block.type==='code'&&block.id.endsWith('000000000041')).id}},{id:id45,pageId,parentBlockId:null,sortKey:'000000000045',type:'synced_block',schemaVersion:1,revision:'1',data:{contentSourceId:state.blocks.find((block:any)=>block.type==='columns').id}});localStorage.setItem('editor-integrity',JSON.stringify(state));},{id44:id(44),id45:id(45),pageId});
 await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();const reference=page.locator(`[data-block-id="${id(44)}"]`),source=page.locator(`[data-block-id="${id(41)}"]`);await expect(reference.locator('.code-preview')).toContainText('Multi');
 const layoutReference=page.locator(`[data-block-id="${id(45)}"]`);await expect(layoutReference.locator('.column-panels')).toBeVisible();await expect(layoutReference.locator('strong').first()).toHaveText('Keep ');await expect(layoutReference.getByRole('textbox',{name:'Column content',exact:true}).first()).toHaveAttribute('contenteditable','false');
 await source.getByText('Edit source',{exact:true}).click();await source.getByRole('textbox',{name:'Code source',exact:true}).fill('const sourceUpdate = 73;');await expect(reference.locator('.code-preview')).toContainText('sourceUpdate = 73');await save(page);
 await page.evaluate(()=>{const state=(window as any).integrityState();state.blocks=state.blocks.filter((block:any)=>!block.id.endsWith('000000000041'));localStorage.setItem('editor-integrity',JSON.stringify(state));});await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();await expect(reference).toContainText('Source is unavailable');
 expect(await page.evaluate(()=>(window as any).integrityState().blocks.find((block:any)=>block.id.endsWith('000000000044')).data.contentSourceId)).toBe(id(41));
});

test("a reusable layout resolves its scoped remote source subtree without a workspace block preload",async({page})=>{
 await page.evaluate(({reference,remotePage,source,child,group,pageId})=>{const state=(window as any).integrityState();const base={pageId:remotePage,schemaVersion:1,revision:'1'};state.remoteBlocks=[{...base,id:source,parentBlockId:null,sortKey:'1',type:'tab_container',data:{tabs:[{id:group,title:'Remote tab',childBlockIds:[child]}]}},{...base,id:child,parentBlockId:source,sortKey:'2',type:'code',data:{source:'const remoteRich = true;',language:'typescript'}}];state.blocks.push({id:reference,pageId,parentBlockId:null,sortKey:'000000000044',type:'synced_block',schemaVersion:1,revision:'1',data:{contentSourceId:source}});localStorage.setItem('editor-integrity',JSON.stringify(state));},{reference:id(44),remotePage:id(70),source:id(71),child:id(72),group:id(73),pageId});
 await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();const reference=page.locator(`[data-block-id="${id(44)}"]`);await expect(reference.getByRole('tab',{name:'Remote tab',exact:true})).toBeVisible();await expect(reference.locator('.code-preview')).toContainText('remoteRich = true');
});


async function addOnlineFixtures(page:Page) {
 await page.evaluate(({pageId,bookmark,embed})=>{const state=(window as any).integrityState();const base={pageId,parentBlockId:null,schemaVersion:1,revision:'1'};state.blocks.push({...base,id:bookmark,sortKey:'000000000050',type:'bookmark',data:{url:'https://source.example/article',title:'Saved offline title',description:'Saved offline description'}},{...base,id:embed,sortKey:'000000000051',type:'embed',data:{url:'https://www.youtube.com/watch?v=M7lc1UVf-VE',provider:'youtube.com',sandboxPolicy:'no_scripts_no_network',offlineText:'<script>window.importedRan=true</script> Saved offline evidence'}});localStorage.setItem('editor-integrity',JSON.stringify(state));},{pageId,bookmark:id(50),embed:id(51)});
 await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();
}
test("bookmarks fetch only on explicit action and keep source-matched offline metadata",async({page})=>{
 await addOnlineFixtures(page);const bookmark=page.locator(`[data-block-id="${id(50)}"]`);await expect(bookmark).toContainText('Saved offline title');expect(await page.evaluate(()=>(window as any).integrityState().bookmarkRequests??0)).toBe(0);
 await bookmark.getByRole('button',{name:'Fetch metadata once',exact:true}).click();await expect(bookmark).toContainText('Cached <script>text</script>');await expect(bookmark.locator('script')).toHaveCount(0);await save(page);
 await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();await expect(bookmark).toContainText('available offline');expect(await page.evaluate(()=>(window as any).integrityState().bookmarkRequests)).toBe(1);
 await page.evaluate(()=>(window as any).integrityState().failBookmark=true);await bookmark.getByRole('button',{name:'Fetch metadata once',exact:true}).click();await expect(bookmark.getByRole('alert')).toContainText('saved content is unchanged');await expect(bookmark).toContainText('Safe cached description');
 await bookmark.getByText('Edit bookmark',{exact:true}).click();await bookmark.getByRole('textbox',{name:'Bookmark URL',exact:true}).fill('https://different.example/article');await expect(bookmark.getByRole('link').first()).toHaveText('https://different.example/article');await expect(bookmark).toContainText('Saved metadata belongs to a previous URL');
});
test("online embeds require per-view consent, isolate provider scripts and keep inert offline fallback",async({page,context})=>{
 const requests:string[]=[];await context.route('https://www.youtube-nocookie.com/**',route=>{requests.push(route.request().url());return route.fulfill({contentType:'text/html',body:'<div id="player">Provider fixture</div><script>try{top.go.main.App.Dispatch("batch","{}");document.body.dataset.parentAccess="allowed"}catch{document.body.dataset.parentAccess="blocked"};document.body.dataset.ownBridge=typeof window.go</script>'});});
 await addOnlineFixtures(page);const embed=page.locator(`[data-block-id="${id(51)}"]`);await expect(embed.getByRole('button',{name:'Enable YouTube for this view',exact:true})).toBeVisible();expect(requests).toEqual([]);await expect(embed.locator('iframe[title="youtube.com offline embed"]').contentFrame().locator('body')).toContainText('Saved offline evidence');expect(await page.evaluate(()=>(window as any).importedRan)).toBeUndefined();
 await embed.getByRole('button',{name:'Enable YouTube for this view',exact:true}).click();const outer=embed.locator('iframe[title="YouTube enabled online embed"]');await expect(outer).toHaveAttribute('sandbox','allow-scripts allow-same-origin allow-presentation');const provider=outer.contentFrame();await expect(provider.locator('body')).toHaveAttribute('data-parent-access','blocked');expect(requests).toHaveLength(1);await expect(provider.locator('body')).toHaveAttribute('data-own-bridge','undefined');await expect(embed).toContainText('Saved offline evidence');
 await embed.getByRole('button',{name:'Stop online embed',exact:true}).click();await expect(outer).toHaveCount(0);expect(requests).toHaveLength(1);
 await embed.getByRole('button',{name:'Enable YouTube for this view',exact:true}).click();await expect.poll(()=>requests.length).toBe(2);await context.setOffline(true);await expect(outer).toHaveCount(0);await expect(embed).toContainText('Offline · saved text is available');await context.setOffline(false);
 await page.reload();await page.getByRole('button',{name:/Editor integrity/}).first().click();await expect(embed.getByRole('button',{name:'Enable YouTube for this view',exact:true})).toBeVisible();expect(requests).toHaveLength(2);
 await embed.getByText('Edit embed',{exact:true}).click();await embed.getByRole('textbox',{name:'Embed source URL',exact:true}).fill('https://youtube.com.attacker.example/watch?v=M7lc1UVf-VE');await expect(embed).toContainText('no supported live player');await expect(embed.getByRole('button',{name:/Enable .* for this view/})).toHaveCount(0);expect(requests).toHaveLength(2);
});

for(const providerName of ["YouTube","Vimeo"] as const) test(`optional real ${providerName} provider smoke for the consented sandbox`,async({page})=>{
 test.skip(process.env.TROVE_LIVE_EMBED_SMOKE!=="1","Requires an explicit network qualification run");
 const diagnostics:string[]=[];page.on('pageerror',error=>diagnostics.push(error.message));page.on('requestfailed',request=>{if(request.url().includes('youtube'))diagnostics.push(`${request.url()} ${request.failure()?.errorText}`);});page.on('console',message=>{if(/sandbox|youtube|Refused|blocked/i.test(message.text()))diagnostics.push(message.text());});
 await addOnlineFixtures(page);const embed=page.locator(`[data-block-id="${id(51)}"]`);
 if(providerName==='Vimeo'){await embed.getByText('Edit embed',{exact:true}).click();await embed.getByRole('textbox',{name:'Embed source URL',exact:true}).fill('https://player.vimeo.com/video/76979871?h=8272103f6e');}
 await embed.getByRole('button',{name:`Enable ${providerName} for this view`,exact:true}).click();
 const player=embed.locator(`iframe[title="${providerName} enabled online embed"]`).contentFrame();
 try {if(providerName==='YouTube'){await expect(player.locator('.html5-video-player')).toBeVisible({timeout:20000});await expect(player.locator('.ytp-error-content-wrap')).not.toBeVisible();}else{await expect(player.locator('video')).toBeAttached({timeout:20000});}}
 finally {for(const frame of page.frames())if(frame.url().includes('youtube')||frame.url().includes('vimeo')||frame.url().includes('chrome-error'))diagnostics.push(frame.url()+' '+await frame.locator('body').innerText().catch(()=>''));await test.info().attach('live-provider-diagnostics',{body:JSON.stringify(diagnostics),contentType:'application/json'});console.log(JSON.stringify(diagnostics));}
});


test("provider frames cannot navigate into the Trove app origin",async({page,context})=>{
 const response=await page.request.get(process.env.TROVE_TEST_URL??'http://127.0.0.1:4173');expect(response.headers()['content-security-policy']).toContain("frame-ancestors 'none'");expect(response.headers()['x-frame-options']).toBe('DENY');
 await context.route('https://www.youtube-nocookie.com/**',route=>route.fulfill({contentType:'text/html',body:'<p id="remote">Provider fixture</p>'}));await addOnlineFixtures(page);
 await page.evaluate(()=>{(window as any).frameViolations=[];document.addEventListener('securitypolicyviolation',event=>{(window as any).frameViolations.push(event.blockedURI);});});
 const embed=page.locator(`[data-block-id="${id(51)}"]`);await embed.getByRole('button',{name:'Enable YouTube for this view',exact:true}).click();const provider=embed.locator('iframe[title="YouTube enabled online embed"]').contentFrame();await expect(provider.locator('#remote')).toBeVisible();
 await provider.locator('body').evaluate((element,url)=>{window.location.href=url;},process.env.TROVE_TEST_URL??'http://127.0.0.1:4173');
 await expect.poll(()=>page.evaluate(()=>(window as any).frameViolations.some((uri:string)=>uri.startsWith(location.origin)))).toBe(true);
 await expect(page.getByRole('textbox',{name:'Page title',exact:true})).toHaveValue('Editor integrity');
});


test("changing an enabled embed URL or provider revokes consent before another request",async({page,context})=>{
 const calls:string[]=[];for(const origin of ['https://www.youtube-nocookie.com/**','https://player.vimeo.com/**'])await context.route(origin,route=>{calls.push(route.request().url());return route.fulfill({contentType:'text/html',body:'<p>Player fixture</p>'});});
 await addOnlineFixtures(page);const embed=page.locator(`[data-block-id="${id(51)}"]`);await embed.getByRole('button',{name:'Enable YouTube for this view',exact:true}).click();await expect.poll(()=>calls.length).toBe(1);
 await embed.getByText('Edit embed',{exact:true}).click();await embed.getByRole('textbox',{name:'Embed source URL',exact:true}).fill('https://vimeo.com/76979871');await expect(embed.locator('iframe[title$="enabled online embed"]')).toHaveCount(0);await expect(embed.getByRole('button',{name:'Enable Vimeo for this view',exact:true})).toBeVisible();expect(calls).toHaveLength(1);
 await embed.getByRole('button',{name:'Enable Vimeo for this view',exact:true}).click();await expect.poll(()=>calls.length).toBe(2);expect(calls[1]).toBe('https://player.vimeo.com/video/76979871?autoplay=0&dnt=1');
});


test("outer toolbar follows stored formatting and selection without canonical edits", async ({ page }) => {
  const before = await page.evaluate(() => structuredClone((window as any).integrityState().blocks));
  const paragraph = page.locator(`[data-block-id="${id(40)}"] .block-text`);
  await paragraph.click();
  const toolbar = page.getByRole("toolbar", { name: "Text formatting" });
  await expect(toolbar.getByLabel("Block type")).toHaveValue("paragraph");
  for (const mark of ["bold", "italic", "underline", "strike"]) {
    const button = toolbar.getByRole("button", { name: mark, exact: true });
    await expect(button).toHaveAttribute("aria-pressed", "false");
    await button.click();
    await expect(button).toHaveAttribute("aria-pressed", "true");
    await button.click();
    await expect(button).toHaveAttribute("aria-pressed", "false");
  }
  await page.locator(`[data-block-id="${id(30)}"] .block-text`).click();
  await expect(toolbar.getByLabel("Block type")).toHaveValue("toggle");
  await paragraph.click();
  await expect(toolbar.getByLabel("Block type")).toHaveValue("paragraph");
  expect(await page.evaluate(() => (window as any).integrityState().blocks)).toEqual(before);
});
