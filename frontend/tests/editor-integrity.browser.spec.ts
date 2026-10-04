import { test, expect, type Page, type Locator } from "@playwright/test";

const id = (n: number) => `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const pageId = id(2);
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
  ],
  collections:[],properties:[],records:[],views:[],files:[],meetings:[],taskFacets:[],containers:[],containerCollections:[],transcriptSegments:[],projectScopes:[],memories:[],importMappings:[],
};
test.beforeEach(async ({page}) => {
  await page.addInitScript(({initial}) => {
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
  await page.goto(process.env.TROVE_TEST_URL??"http://127.0.0.1:4173");
  await page.getByRole("button",{name:/Editor integrity/}).first().click();
});
async function save(page:Page) {
  await page.getByRole("button",{name:"Save page",exact:true}).click();
  await expect(page.locator(".save-state")).toContainText("Saved");
}
async function insertCharacter(page:Page,field:Locator) {
  await field.click();
  await field.evaluate(element=>{
    const walker=document.createTreeWalker(element,NodeFilter.SHOW_TEXT);const text=walker.nextNode()!;
    const range=document.createRange();range.setStart(text,4);range.collapse(true);
    const selection=window.getSelection()!;selection.removeAllRanges();selection.addRange(range);
  });
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
 await expect(code.locator(".code-line").first().locator(".hljs-comment")).toContainText("/* Multi");
 await expect(code.locator(".code-line").nth(1).locator(".hljs-comment")).toContainText("comment */");
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
