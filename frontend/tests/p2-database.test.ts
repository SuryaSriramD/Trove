import assert from 'node:assert/strict';
import test from 'node:test';
import { chartRatios, formFieldEmpty } from '../src/database/P2Model.ts';
test('chart normalization preserves huge decimal ratios without Number overflow', () => {
  const huge = '1' + '0'.repeat(400);
  assert.deepEqual(chartRatios([huge, '-' + huge, null], 'bar'), [1, -1, 0]);
  assert.deepEqual(chartRatios(['0.000000000000000001', '0.000000000000000002'], 'donut'), [.333333, .666666]);
  assert.deepEqual(chartRatios(['0', null], 'donut'), [0, 0]);
});
test('donut cannot misrepresent signed aggregates as positive areas', () => {
  assert.throws(() => chartRatios(['-1', '2'], 'donut'), /nonnegative/);
  assert.throws(() => chartRatios(['NaN'], 'bar'), /Invalid chart decimal/);
});
test('required form values accept false and zero but reject blank rich text', () => {
  assert.equal(formFieldEmpty(false), false);
  assert.equal(formFieldEmpty({ decimal: '0', format: 'decimal' }), false);
  assert.equal(formFieldEmpty([{ text: '   ' }]), true);
  assert.equal(formFieldEmpty([]), true);
  assert.equal(formFieldEmpty(['option']), false);
});
