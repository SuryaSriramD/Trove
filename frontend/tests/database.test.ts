import assert from 'node:assert/strict';
import test from 'node:test';
import { displayValue, groupRecords, reschedule } from '../src/database/model.ts';
test('calendar range rescheduling preserves duration and explicit timezone',()=>{
 assert.deepEqual(reschedule({start:'2026-09-30',end:'2026-10-02',hasTime:false,timezone:'Asia/Kolkata'},'2026-10-05'),{start:'2026-10-05',end:'2026-10-07',hasTime:false,timezone:'Asia/Kolkata'});
 assert.deepEqual(reschedule({start:'2026-09-30T09:30:00+05:30',end:'2026-09-30T10:15:00+05:30',hasTime:true,timezone:'Asia/Kolkata'},'2026-10-05'),{start:'2026-10-05T09:30:00+05:30',end:'2026-10-05T04:45:00.000Z',hasTime:true,timezone:'Asia/Kolkata'});
});
test('grouping and subgrouping partition references without copying record identities',()=>{
 const properties=[{id:'status',name:'Status',type:'status',config:{options:[{id:'open',label:'Open'},{id:'done',label:'Done'}]}},{id:'date',name:'Date',type:'date'}] as any;
 const a={id:'a',values:[{propertyId:'status',value:'open'},{propertyId:'date',value:{start:'2026-09-30',end:null,timezone:'UTC',hasTime:false}}]},b={id:'b',values:[{propertyId:'status',value:'done'}]},c={id:'c',values:[{propertyId:'status',value:'open'}]};
 const groups=groupRecords([a,b,c] as any,properties[0],properties[1],[]);assert.equal(groups.length,2);assert.equal(groups[0].records[0],a);assert.equal(groups[0].records[1],c);assert.equal(groups[0].children.length,2);assert.deepEqual(groups.flatMap(g=>g.records.map(r=>r.id)).sort(),['a','b','c']);
});
test('imported identities, places and author labels remain readable without interpreting raw payload',()=>{
 assert.equal(displayValue([{sourceSystem:'external',externalKind:'person',displayName:'अशा',raw:{unchanged:true}},{sourceSystem:'external',externalKind:'group',displayName:'研究',raw:{unchanged:true}}]),'अशा, 研究');assert.equal(displayValue({label:'Hyderabad',raw:{unsafe:'<script>'}}),'Hyderabad');assert.equal(displayValue({decimal:'9007199254740993.123456',format:'decimal'}),'9007199254740993.123456');
});
