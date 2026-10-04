import test from 'node:test';
import assert from 'node:assert/strict';
import {mergeCells,splitCell,replaceDocument,inertEmbedDocument,safeWebURL} from '../src/editor/advanced.ts';
import {fromDocument,toDocument,mutations} from '../src/editor/codec.ts';
const table:any={rowCount:2,columnCount:2,headerRows:1,headerColumns:0,columnWidths:[120,120],cells:Array.from({length:4},(_,i)=>({id:`cell${i}`,row:Math.floor(i/2),column:i%2,rowSpan:1,colSpan:1,richText:[{text:`value${i}`,marks:['bold'],link:'https://example.test'}],preMergeCells:[]}))};
test('rectangular nested merges reversibly preserve rich text and original cell identity',()=>{
 const merged=mergeCells(table,'cell0','cell3');assert.equal(merged.cells.length,1);assert.equal(merged.cells[0].rowSpan,2);assert.deepEqual(table.cells,splitCell(merged,'cell0').cells);
 const row=mergeCells(table,'cell0','cell1'),nested=mergeCells(row,'cell0','cell3');assert.deepEqual(splitCell(nested,'cell0').cells.sort((a:any,b:any)=>a.id.localeCompare(b.id)),row.cells.sort((a:any,b:any)=>a.id.localeCompare(b.id)));
});
test('post-merge edits survive splitting without removing other original content',()=>{
 const merged=mergeCells(table,'cell0','cell3');merged.cells[0].richText=[{text:'Edited merged text'}];const split=splitCell(merged,'cell0');assert.equal(split.cells[0].richText[0].text,'Edited merged text');assert.deepEqual(split.cells[3],table.cells[3]);
});
test('incomplete and overlapping ranges do not erase source data',()=>{
 assert.throws(()=>mergeCells({...table,cells:table.cells.filter((c:any)=>c.id!=='cell1')},'cell0','cell3'),/range|cells/);
 assert.throws(()=>splitCell(table,'cell0'),/no original/);
});
test('search replace crosses formatting spans and leaves canonical IDs and raw evidence intact',()=>{
 const input:any={type:'doc',content:[{type:'troveText',attrs:{blockId:'Hello'},content:[{type:'text',text:'Hel',marks:[{type:'bold'}]},{type:'text',text:'lo world'}]},{type:'troveAtom',attrs:{data:{title:'Hello',url:'https://Hello.test',raw:{text:'Hello'},cells:[{richText:[{text:'Hello'}],preMergeCells:[{richText:[{text:'Hello'}]}]}]}}}]};
 const {doc,count}=replaceDocument(input,'Hello','Hi');assert.equal(count,3);assert.equal(doc.content![0].content![0].text,'Hi');assert.deepEqual(doc.content![0].content![0].marks,[{type:'bold'}]);assert.equal(doc.content![0].attrs!.blockId,'Hello');assert.equal(doc.content![1].attrs!.data.url,'https://Hello.test');assert.equal(doc.content![1].attrs!.data.cells[0].preMergeCells[0].richText[0].text,'Hello');assert.equal(input.content[0].content[0].text,'Hel');
});
test('embed HTML escapes executable input and has no network policy',()=>{
 const html=inertEmbedDocument('<script>fetch("https://leak.test")</script><img src=x>');assert(!html.includes('<script>'));assert(html.includes("default-src 'none'"));assert(html.includes('&lt;img'));assert.equal(safeWebURL('javascript:alert(1)'),null);assert.equal(safeWebURL('https://user:pass@example.test'),null);assert.equal(safeWebURL('https://example.test'),'https://example.test/');
});
test('advanced canonical structures round trip through editor without spurious writes',()=>{
 const data=[['heading_4',{richText:[{text:'H4'}]}],['columns',{columns:[{id:'col',widthRatio:1,childBlockIds:['child']}]}],['paragraph',{richText:[{text:'nested'}]}],['tab_container',{tabs:[]}],['synced_block',{contentSourceId:'child'}],['table',mergeCells(table,'cell0','cell3')],['embed',{url:'https://test',provider:'test',sandboxPolicy:'no_scripts_no_network',offlineText:'offline'}]];
 const blocks=data.map(([type,value],i)=>({id:i===2?'child':`block${i}`,pageId:'page',parentBlockId:i===2?'block1':null,sortKey:String(i).padStart(12,'0'),type,schemaVersion:1,revision:'1',data:value})) as any;
 const round=fromDocument(toDocument(blocks));assert.deepEqual(mutations('page',blocks,round),[]);assert.deepEqual(round.find(b=>b.id==='block5')!.data,blocks[5].data);
});
