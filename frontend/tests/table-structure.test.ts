import test from "node:test";
import assert from "node:assert/strict";
import {insertTableLine,removeTableLine,tableLineRemoval,tableWidths} from "../src/editor/tableStructure.ts";
import {mergeCells,splitCell} from "../src/editor/advanced.ts";
const table:any={rowCount:2,columnCount:2,headerRows:1,headerColumns:1,columnWidths:[120,240],cells:Array.from({length:4},(_,i)=>({id:`cell${i}`,row:Math.floor(i/2),column:i%2,rowSpan:1,colSpan:1,richText:[{text:`value${i}`,marks:["bold"],link:"https://example.test"}],preMergeCells:[]}))};
test("row and column insertions retain rich cells, headers, and normalized widths",()=>{
 const row=insertTableLine(table,"row",0,["new0","new1"]);
 assert.equal(row.rowCount,3);assert.equal(row.headerRows,2);assert.deepEqual(row.cells.find(c=>c.id==="cell0")?.richText,table.cells[0].richText);
 const column=insertTableLine(row,"column",1,["new2","new3","new4"]);
 assert.deepEqual(column.columnWidths,[120,120,240]);assert.equal(column.cells.length,9);assert.equal(column.cells.find(c=>c.id==="cell3")?.column,2);
 assert.deepEqual(tableWidths({...table,columnWidths:[]}),[180,180]);
});
test("removing populated lines exposes their evidence before an explicit mutation",()=>{
 assert.equal(tableLineRemoval(table,"row",0).length,2);
 const shrunk=removeTableLine(table,"row",0);assert.equal(shrunk.headerRows,0);assert.equal(shrunk.cells[0].id,"cell2");assert.equal(shrunk.cells[0].row,0);
 assert.equal(table.cells.length,4);assert.throws(()=>removeTableLine(shrunk,"row",0),/at least one/);
});
test("an intact shifted merge restores its original rich cells at the shifted coordinates",()=>{
 const merged=mergeCells(table,"cell0","cell3"),shifted=insertTableLine(merged,"row",0,["new0","new1"]),split=splitCell(shifted,"cell0");
 for(const old of table.cells){const current=split.cells.find(c=>c.id===old.id)!;assert.equal(current.row,old.row+1);assert.deepEqual(current.richText,old.richText);}
 const removed=removeTableLine(shifted,"row",0);assert.deepEqual(splitCell(removed,"cell0").cells,table.cells);
});
test("insertion and removal through merged spans refuse to erase source content",()=>{
 const merged=mergeCells(table,"cell0","cell3");
 assert.throws(()=>insertTableLine(merged,"row",1,["new0","new1"]),/Restore cells/);
 assert.throws(()=>removeTableLine(merged,"column",0),/Restore cells/);
 assert.deepEqual(merged.cells[0].preMergeCells,table.cells);
});
test("opaque merge evidence is retained by rejecting structural shifts that cannot map it",()=>{
 const opaque={...table,cells:[{...table.cells[0],preMergeCells:[{raw:"retained"}]}]};
 assert.throws(()=>insertTableLine(opaque,"row",0,["new0","new1"]),/evidence/);
 assert.deepEqual(opaque.cells[0].preMergeCells,[{raw:"retained"}]);
});
