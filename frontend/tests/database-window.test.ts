import assert from "node:assert/strict";
import test from "node:test";
import { loadRecordWindow } from "../src/database/recordWindow.ts";

test("fresh clock walks only the prefix needed for the requested page", async () => {
  const calls: (string | undefined)[] = [];
  const result = await loadRecordWindow(
    async (cursor) => {
      calls.push(cursor);
      const index = Number(cursor || 0);
      return { records: [index], nextCursor: String(index + 1) };
    },
    2,
    [""],
    () => false,
  );
  assert.deepEqual(calls, ["", "1", "2"]);
  assert.deepEqual(result, {
    records: [2],
    nextCursor: "3",
    index: 2,
    history: ["", "1", "2"],
  });
});

test("current-clock navigation reuses its cursor; a shrinking result stops at its last page", async () => {
  const calls: (string | undefined)[] = [];
  const load = async (cursor?: string) => {
    calls.push(cursor);
    return { records: [42] };
  };
  assert.equal(
    (await loadRecordWindow(load, 1, ["", "fresh"], () => false)).index,
    1,
  );
  assert.deepEqual(calls, ["fresh"]);
  calls.length = 0;
  assert.equal((await loadRecordWindow(load, 50, [""], () => false)).index, 0);
  assert.deepEqual(calls, [""]);
});

test("cancellation during a pending page prevents the next request and discards its response", async () => {
  let cancelled = false;
  let requests = 0;
  const pending = loadRecordWindow(
    async () => {
      requests++;
      cancelled = true;
      return { records: ["old"], nextCursor: "old-next" };
    },
    10,
    [""],
    () => cancelled,
  );
  await assert.rejects(pending, /cancelled/);
  assert.equal(requests, 1);
});

test("service snapshot/clock conflicts propagate without a retry or accepting stale records", async () => {
  const conflict = Error("Records, view or clock changed; restart the query");
  let calls = 0;
  await assert.rejects(
    loadRecordWindow(
      async () => {
        calls++;
        throw conflict;
      },
      2,
      ["", "old", "stale"],
      () => false,
    ),
    (error) => error === conflict,
  );
  assert.equal(calls, 1);
});
