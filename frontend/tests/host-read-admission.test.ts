import test from "node:test";
import assert from "node:assert/strict";
import { HostReadAdmission, dispatchWithHostReadAdmission } from "../src/hostReadAdmission.ts";
const tick = () => new Promise<void>(resolve => setImmediate(resolve));
function held<T>() { let resolve!: (value: T) => void, reject!: (error: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }

test("page read fan-out stays at two active sends and completes each call once", async () => {
  const methods = ["readPage", "page.freshness", "pageDisplay.get", "operations.list", "links.backlinks", "ai.runs", "models.list", "editor.lock", "editor.templates", "editor.references"];
  let active = 0, peak = 0; const sent: string[] = [], releases: (() => void)[] = [];
  const results = methods.map(method => dispatchWithHostReadAdmission(true, method, () => {
    sent.push(method); active++; peak = Math.max(active, peak);
    return new Promise<string>(resolve => releases.push(() => { active--; resolve(method); }));
  }));
  await tick(); assert.equal(sent.length, 2);
  for (let i = 0; i < methods.length; i++) { releases.shift()!(); await tick(); }
  assert.deepEqual(await Promise.all(results), methods); assert.deepEqual(sent, methods); assert.equal(peak, 2);
});

test("cancelled queued and pre-aborted reads never reach transport", async () => {
  const admission = new HostReadAdmission(), a = held<string>(), b = held<string>();
  const first = admission.run(() => a.promise), second = admission.run(() => b.promise);
  const controller = new AbortController(); let sent = 0;
  const cancelled = admission.run(async () => { sent++; return "obsolete"; }, controller.signal);
  const rejected = assert.rejects(cancelled, { name: "AbortError" }); controller.abort(); await rejected;
  await assert.rejects(admission.run(async () => { sent++; return "pre-aborted"; }, controller.signal), { name: "AbortError" });
  a.resolve("a"); b.resolve("b"); await Promise.all([first, second]); await tick(); assert.equal(sent, 0);
});

test("aborting an in-flight consumer holds capacity until the real send settles", async () => {
  const admission = new HostReadAdmission(), a = held<string>(), b = held<string>(), controller = new AbortController();
  let sent = 0;
  const first = admission.run(() => { sent++; return a.promise; }, controller.signal);
  const second = admission.run(() => { sent++; return b.promise; }); await tick();
  const third = admission.run(async () => { sent++; return "third"; });
  const rejected = assert.rejects(first, { name: "AbortError" }); controller.abort(); await rejected; await tick(); assert.equal(sent, 2);
  a.resolve("obsolete response"); assert.equal(await third, "third"); b.resolve("second"); await second; await tick(); assert.equal(sent, 3);
});

test("transport failures release capacity without retry or error substitution", async () => {
  const admission = new HostReadAdmission(), failure = new Error("Host busy"), heldRead = held<string>(); let sends = 0;
  const first = admission.run(async () => { sends++; throw failure; });
  const second = admission.run(() => heldRead.promise), third = admission.run(async () => "later");
  await assert.rejects(first, error => error === failure); assert.equal(await third, "later");
  heldRead.resolve("held"); await second; assert.equal(sends, 1);
});

test("queue inventory is bounded and cancellation returns queued capacity", async () => {
  const admission = new HostReadAdmission(), a = held<number>(), b = held<number>();
  const running = [admission.run(() => a.promise), admission.run(() => b.promise)];
  const controller = new AbortController();
  const queued = Array.from({ length: 64 }, () => admission.run(async () => 1, controller.signal));
  const rejected = queued.map(value => assert.rejects(value, { name: "AbortError" }));
  await assert.rejects(admission.run(async () => 2), /Too many pending workspace reads/);
  controller.abort(); await Promise.all(rejected);
  const accepted = admission.run(async () => 3); a.resolve(1); b.resolve(2);
  assert.equal(await accepted, 3); await Promise.all(running);
});

test("native calls and hosted writes, inference and controls bypass read admission", async () => {
  const a = held<string>(), b = held<string>(), controller = new AbortController();
  const running = [dispatchWithHostReadAdmission(true, "readPage", () => a.promise), dispatchWithHostReadAdmission(true, "page.freshness", () => b.promise)];
  controller.abort(); let calls = 0;
  for (const [hosted, method] of [[false, "readPage"], [true, "batch"], [true, "enqueueJob"], [true, "cancelJob"], [true, "editor.setLock"]] as const)
    assert.equal(await dispatchWithHostReadAdmission(hosted, method, async () => { calls++; return method; }, controller.signal), method);
  assert.equal(calls, 5); a.resolve("a"); b.resolve("b"); await Promise.all(running);
});
