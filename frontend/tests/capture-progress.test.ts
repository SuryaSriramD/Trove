import assert from "node:assert/strict";
import test from "node:test";
import { assertCaptureImportProgress, type CaptureImportProgress } from "../src/capture/progress.ts";

const reviewed: CaptureImportProgress = { planId: "reviewed-plan", planHash: "frozen-review-hash", initialized: false, cursor: 0, state: "preview", items: ["first", "second"] };
const initialized = { ...reviewed, initialized: true, state: "running" };
const firstItem = { ...initialized, cursor: 1 };

test("capture progress follows initialization and durable items with one frozen review hash", () => {
  assert.doesNotThrow(() => assertCaptureImportProgress(reviewed, initialized));
  assert.doesNotThrow(() => assertCaptureImportProgress(initialized, firstItem));
  assert.doesNotThrow(() => assertCaptureImportProgress(firstItem, { ...firstItem, cursor: 2, state: "complete" }));
});

test("capture progress accepts complete empty and no-content plans in one durable unit", () => {
  assert.doesNotThrow(() => assertCaptureImportProgress({ ...reviewed, items: [] }, { ...initialized, items: [], state: "complete" }));
  assert.doesNotThrow(() => assertCaptureImportProgress(reviewed, { ...initialized, cursor: 2, state: "complete" }));
});

test("capture progress rejects unchanged progress and status-only changes", () => {
  for (const next of [initialized, { ...initialized, state: "preview" }])
    assert.throws(() => assertCaptureImportProgress(initialized, next), /made no progress/);
});

test("capture progress rejects changed review identity even alongside cursor progress", () => {
  for (const next of [{ ...firstItem, planHash: "unreviewed-hash" }, { ...firstItem, planId: "another-plan" }, { ...firstItem, items: ["first", "second", "unreviewed"] }])
    assert.throws(() => assertCaptureImportProgress(initialized, next), /review changed/);
});

test("capture progress rejects regressions, invalid cursor ranges and premature completion", () => {
  for (const next of [{ ...initialized }, { ...firstItem, initialized: false }, { ...firstItem, cursor: -1 },
    { ...firstItem, cursor: 0.5 }, { ...firstItem, cursor: 3 }, { ...firstItem, cursor: Number.NaN },
    { ...firstItem, state: "complete" }, { ...reviewed, state: "complete" }])
    assert.throws(() => assertCaptureImportProgress(firstItem, next), /made no progress/);
});

test("capture progress rejects malformed initialization and unexpected states even with cursor advance", () => {
  for (const initialized of [1, "true", null, undefined])
    assert.throws(() => assertCaptureImportProgress(reviewed, { ...firstItem, initialized } as unknown as CaptureImportProgress), /made no progress/);
  for (const state of ["preview", "committing", "failed", "cancelled", ""])
    assert.throws(() => assertCaptureImportProgress(initialized, { ...firstItem, state }), /made no progress/);
});
