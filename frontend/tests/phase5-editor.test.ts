import test from 'node:test';
import assert from 'node:assert/strict';
import {mergeCells, splitCell, replaceDocument} from '../src/editor/advanced.ts';
import {fromDocument, toDocument, mutations} from '../src/editor/codec.ts';

test('Phase 5 editor replacement crosses table formatting spans without changing merged evidence', () => {
  const original = [{text:'עברית ',marks:['bold']},{text:'🌱漢',marks:['italic']},{text:'字 tail',link:'https://example.test'}];
  const doc:any = {type:'doc',content:[{type:'troveAtom',attrs:{blockId:'stable-table',data:{cells:[{id:'stable-cell',richText:original,preMergeCells:[{id:'original',richText:structuredClone(original)}]}]}}}]};
  const result = replaceDocument(doc,'🌱漢字','తెలుగు');
  assert.equal(result.count,1);
  const cell = result.doc.content![0].attrs!.data.cells[0];
  assert.deepEqual(cell.richText,[{text:'עברית ',marks:['bold']},{text:'తెలుగు',marks:['italic']},{text:' tail',link:'https://example.test'}]);
  assert.equal(cell.id,'stable-cell');
  assert.deepEqual(cell.preMergeCells,[{id:'original',richText:original}]);
  assert.deepEqual(doc.content[0].attrs.data.cells[0].richText,original);
});

test('Phase 5 editor merged tables and nested tabs keep canonical identities across unrelated edits', () => {
  const table:any = {rowCount:1,columnCount:2,headerRows:0,headerColumns:0,columnWidths:[120,180],cells:[
    {id:'left',row:0,column:0,rowSpan:1,colSpan:1,richText:[{text:'Left עברית',marks:['bold']}],preMergeCells:[]},
    {id:'right',row:0,column:1,rowSpan:1,colSpan:1,richText:[{text:'Right 🌱',link:'https://example.test'}],preMergeCells:[]},
  ]};
  const make=(id:string,type:string,data:any,parentBlockId:string|null=null,sortKey='000000000001')=>({id,pageId:'page',parentBlockId,sortKey,type,schemaVersion:1,revision:'1',data});
  const blocks:any = [
    make('tabs','tab_container',{tabs:[{id:'tab-a',title:'中文',childBlockIds:['table']},{id:'tab-b',title:'العربية',childBlockIds:['child']}]}),
    make('table','table',mergeCells(table,'left','right'),'tabs'),
    make('child','paragraph',{richText:[{text:'Preserved Telugu తెలుగు'}]},'tabs','000000000002'),
    make('ordinary','paragraph',{richText:[{text:'Change only this paragraph'}]},null,'000000000002'),
  ];
  const round = fromDocument(toDocument(blocks));
  assert.deepEqual(mutations('page',blocks,round),[]);
  const doc = toDocument(blocks);
  const text = doc.content!.find(n=>n.attrs?.blockId==='ordinary')!;
  text.content = [{type:'text',text:'Edited paragraph'}];
  const changes = mutations('page',blocks,fromDocument(doc));
  assert.equal(changes.length,1);
  assert.equal(changes[0].kind,'block.update');
  assert.equal((changes[0] as any).blockId,'ordinary');
  assert.deepEqual(splitCell(round.find(b=>b.id==='table')!.data as any,'left').cells,table.cells);
});
