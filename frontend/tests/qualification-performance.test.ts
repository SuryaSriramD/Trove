import assert from "node:assert/strict";
import test from "node:test";
import { bridgeMetric, createQualificationRecorder } from "../src/qualificationPerformance.ts";

function fixture(enabled = true) {
  let time = 10, visible = true, next = 0;
  const frames = new Map<number, FrameRequestCallback>();
  const records: [string, number][] = [];
  const recorder = createQualificationRecorder({
    QualificationPerformanceEnabled: async () => enabled,
    RecordQualificationPerformance: async (label, ms) => { records.push([label, ms]); return true; },
  }, () => time, callback => { frames.set(++next, callback); return next; }, id => { frames.delete(id); }, () => visible);
  return { recorder, records, frames, time: (n: number) => { time = n; }, visible: (v: boolean) => { visible = v; },
    frame: () => { const entries = [...frames.entries()]; frames.clear(); for (const [, fn] of entries) fn(time); } };
}

test("disabled qualification records nothing and schedules no frames", async () => {
  const f = fixture(false); await f.recorder.ready;
  assert.equal(f.recorder.start(), undefined);
  f.recorder.record("database_bridge", 1);
  f.recorder.afterFrames([["input_to_frames", 1]]);
  assert.deepEqual(f.records, []); assert.equal(f.frames.size, 0);
});
test("frame observations wait for two opportunities and reject hidden/cancelled work", async () => {
  const f = fixture(); await f.recorder.ready;
  const start = f.recorder.start();
  f.recorder.afterFrames([["database_load_to_frames", start], ["database_response_to_frames", start]]);
  f.time(20); f.frame(); assert.deepEqual(f.records, []);
  f.time(45); f.frame();
  assert.deepEqual(f.records, [["database_load_to_frames", 35], ["database_response_to_frames", 35]]);
  const cancel = f.recorder.afterFrames([["input_to_frames", start]]); cancel();
  f.frame(); f.frame(); assert.equal(f.records.length, 2);
  f.recorder.afterFrames([["input_to_frames", start]]);
  f.frame(); f.visible(false); f.frame(); assert.equal(f.records.length, 2);
  f.visible(true); f.recorder.afterFrames([["input_to_frames", start]]); f.recorder.stop();
  f.frame(); f.frame(); assert.equal(f.records.length, 2); assert.equal(f.frames.size, 0);
});
test("fixed metrics reject content and invalid durations; traffic and pending frames stay bounded", async () => {
  const f = fixture(); await f.recorder.ready;
  assert.equal(bridgeMetric("queryRecords"), "database_bridge");
  assert.equal(bridgeMetric("token=secret"), undefined);
  assert.equal(bridgeMetric("toString"), undefined);
  for (const start of [-Infinity, NaN, Infinity, 11, -3600001]) f.recorder.record("database_bridge", start);
  f.recorder.record("private title", 0); assert.deepEqual(f.records, []);
  for (let i = 0; i < 2000; i++) f.recorder.record("database_bridge", 0);
  assert.equal(f.records.length, 513); // final one signals backend sample truncation
  for (let i = 0; i < 100; i++) f.recorder.afterFrames([["input_to_frames", 0]]);
  assert.equal(f.frames.size, 16);
});
