import {test} from 'node:test';import assert from 'node:assert/strict';
import {moveIntoLayout,duplicateLayoutChild,removeLayoutChild} from '../src/editor/layoutStructure.ts';
import {fromDocument,mutations,toDocument,type NodeJSON} from '../src/editor/codec.ts';
const node=(id:string,kind:string,depth:number,parentBlockId:string|null,data:any={},content?:any[]):NodeJSON=>({type:kind==='paragraph'?'troveText':'troveAtom',attrs:{blockId:id,kind,depth,parentBlockId,version:1,data},...(content?{content}:{})});
const doc:NodeJSON={type:'doc',content:[node('a','columns',0,null,{columns:[{id:'ga',widthRatio:1,childBlockIds:['code']}]}),node('code','code',1,'a',{source:'<script>keep</script>',language:'js'}),node('b','tab_container',0,null,{tabs:[{id:'gb',title:'Nested',childBlockIds:['text']}]}),node('text','paragraph',1,'b',{richText:[{text:'bold',marks:['bold']}],raw:{retained:true}},[{type:'text',text:'bold',marks:[{type:'bold'}]}])]};
test('same-depth layout moves change canonical ownership and preserve source data',()=>{
 const changed=moveIntoLayout(doc,'b','gb','code');assert.equal(changed.content!.find(n=>n.attrs?.blockId==='code')!.attrs!.parentBlockId,'b');
 const old=(fromDocument(doc).map((b,i)=>({id:b.id,pageId:'page',parentBlockId:b.parentBlockId,sortKey:String(i),type:b.type,data:b.data,schemaVersion:1,revision:'1'}))) as any;
 const commands=mutations('page',old,fromDocument(changed));assert.ok(commands.some(c=>c.kind==='block.move'&&c.blockId==='code'&&c.parentBlockId==='b'));
 assert.deepEqual(changed.content!.find(n=>n.attrs?.blockId==='a')!.attrs!.data.columns[0].childBlockIds,[]);assert.equal(changed.content!.at(-1)!.attrs!.data.source,'<script>keep</script>');
});
test('duplicating a rich subtree remaps its owned references without mutating original evidence',()=>{
 const nested:NodeJSON={type:'doc',content:[node('outer','columns',0,null,{columns:[{id:'g',widthRatio:1,childBlockIds:['inner']}]}),node('inner','tab_container',1,'outer',{tabs:[{id:'t',title:'Tab',childBlockIds:['rich']}]}),node('rich','paragraph',2,'inner',{richText:[],raw:{retained:true}},[{type:'text',text:'keep',marks:[{type:'link',attrs:{href:'https://example.test'}}]}])]};
 const result=duplicateLayoutChild(nested,'outer','g','inner',['copy','copytext']);const copy=result.content!.find(n=>n.attrs?.blockId==='copy')!;
 assert.deepEqual(copy.attrs!.data.tabs[0].childBlockIds,['copytext']);assert.equal(result.content!.find(n=>n.attrs?.blockId==='copytext')!.attrs!.parentBlockId,'copy');assert.deepEqual(nested.content![0].attrs!.data.columns[0].childBlockIds,['inner']);
 assert.deepEqual(removeLayoutChild(result,'copy'),nested);
});
test('a layout cannot move into its owned descendant',()=>{assert.throws(()=>moveIntoLayout(doc,'code','missing','a'),/itself or its descendant/);});
