import assert from 'node:assert/strict';
import test from 'node:test';
import {calendarDay, resizeDateRange} from '../src/database/dateRange.ts';

test('calendar resizing changes only the selected boundary and rejects inverted ranges',()=>{
  const old={start:'2026-10-01',end:'2026-10-03',timezone:'Asia/Kolkata',hasTime:false};
  assert.deepEqual(resizeDateRange(old,'end','2026-10-09'),{...old,end:'2026-10-09'});
  assert.deepEqual(resizeDateRange(old,'start','2026-09-30'),{...old,start:'2026-09-30'});
  assert.throws(()=>resizeDateRange(old,'end','2026-09-30'));
  assert.throws(()=>resizeDateRange(old,'end','2026-02-30'));
});
test('timed resizing preserves zone wall time across DST and rejects gaps',()=>{
  const old={start:'2026-03-06T09:30:00-05:00',end:'2026-03-06T10:15:00-05:00',timezone:'America/New_York',hasTime:true};
  assert.deepEqual(resizeDateRange(old,'end','2026-03-09'),{...old,end:'2026-03-09T14:15:00.000Z'});
  const gap={...old,end:'2026-03-07T02:30:00-05:00'};
  assert.throws(()=>resizeDateRange(gap,'end','2026-03-08'),/does not exist/);
  assert.equal(calendarDay({start:'2026-10-04T23:00:00Z',timezone:'Asia/Kolkata',hasTime:true}),'2026-10-05');
});
