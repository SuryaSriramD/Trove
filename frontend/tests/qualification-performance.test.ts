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

test("separate diagnostics distinguish hidden, cancelled, limited and acknowledged probes without content", async () => {
  let visible = true, focus = true, next = 0;
  const frames = new Map<number, FrameRequestCallback>();
  const states: any[] = [];
  const recorder = createQualificationRecorder({
    QualificationPerformanceEnabled: async () => true,
    RecordQualificationPerformance: async () => true,
    RecordQualificationPerformanceDiagnostics: async raw => { states.push(JSON.parse(raw)); return true; },
  }, () => 10, cb => { frames.set(++next, cb); return next; }, id => { frames.delete(id); }, () => visible, () => focus);
  await recorder.ready;
  assert.equal(await recorder.diagnostics(), true);
  assert.equal(states[0].ready, true); assert.equal(states[0].focused, true);
  recorder.event("input_trusted");
  const cancel = recorder.afterFrames([["input_to_frames", 1]]); cancel(); cancel();
  visible = false; recorder.afterFrames([["scroll_to_frames", 1]]);
  visible = true; recorder.afterFrames([["input_to_frames", undefined]]);
  for (let i = 0; i < 17; i++) recorder.afterFrames([["input_to_frames", 1]]);
  for (let i = 0; i < 2; i++) { const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(fn => fn(10)); }
  await new Promise(resolve => setImmediate(resolve));
  focus = false;
  await recorder.diagnostics();
  const final = states.at(-1);
  assert.equal(final.focused, false); assert.equal(final.pendingFrames, 0); assert.equal(final.pendingTransports, 0);
  assert.equal(final.counters.probes_cancelled, 1); assert.equal(final.counters.probes_hidden, 1);
  assert.equal(final.counters.probes_missing_start, 1); assert.equal(final.counters.probes_limited, 1);
  assert.equal(final.counters.probes_completed, 16); assert.equal(final.counters.records_accepted, 16);
  assert.equal(final.counters.input_trusted, 1);
  assert.deepEqual(Object.keys(final).sort(), ["counters", "focused", "pendingFrames", "pendingTransports", "ready", "stopped", "visible"]);
  recorder.stop(); await recorder.diagnostics();
  assert.equal(states.at(-1).stopped, true); assert.equal(states.at(-1).ready, false);
});

test("diagnostics preserve rejected and failed transports and bound in-flight reporting", async () => {
  let mode = "reject", release: (value: boolean) => void = () => {};
  const states: any[] = [];
  const recorder = createQualificationRecorder({
    QualificationPerformanceEnabled: async () => true,
    RecordQualificationPerformance: async () => { if (mode === "fail") throw new Error("private payload"); return false; },
    RecordQualificationPerformanceDiagnostics: raw => {
      states.push(JSON.parse(raw)); return new Promise(resolve => { release = resolve; });
    },
  }, () => 10, () => 1, () => {}, () => true);
  await recorder.ready;
  recorder.record("database_bridge", 1); await new Promise(resolve => setImmediate(resolve));
  mode = "fail"; recorder.record("mutation_bridge", 1); await new Promise(resolve => setImmediate(resolve));
  const pending = recorder.diagnostics();
  assert.equal(await recorder.diagnostics(), false); assert.equal(states.length, 1);
  assert.equal(states[0].counters.records_rejected, 1); assert.equal(states[0].counters.records_failed, 1);
  assert.equal(JSON.stringify(states).includes("private"), false);
  release(true); assert.equal(await pending, true);
  recorder.stop();
});

test("disabled recorder does not emit diagnostic traffic", async () => {
  let calls = 0;
  const recorder = createQualificationRecorder({
    QualificationPerformanceEnabled: async () => false,
    RecordQualificationPerformanceDiagnostics: async () => { calls++; return true; },
  }, () => 0, () => 1, () => {}, () => true);
  await recorder.ready; recorder.event("input_trusted"); recorder.stop();
  assert.equal(await recorder.diagnostics(), false); assert.equal(calls, 0);
});
