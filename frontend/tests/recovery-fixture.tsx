import React from "react";
import { createRoot } from "react-dom/client";
import RecoveryPanel from "../src/recovery/RecoveryPanel";
import type { WorkspaceSnapshot } from "../src/api";

const id = (n: number) => `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const page = { id: id(2), workspaceId: id(1), parentPageId: null, title: "Historical record", revision: "5", createdAt: "2026-10-04T10:00:00Z", updatedAt: "2026-10-04T11:00:00Z", deletedAt: null };
const property = { id: id(4), collectionId: id(3), name: "Amount", type: "number", schemaVersion: 1, revision: "2", config: { format: "percent" } };
const w = { workspaceId: id(1), scopeGrantId: id(5), snapshotCursor: "10", contractVersion: "1.0.0", pages: [page], properties: [property], blocks: [], files: [], records: [], collections: [], views: [], containers: [], containerCollections: [], meetings: [], transcriptSegments: [], memoryEntries: [], importMappings: [], importObservations: [], taskFacets: [] } as unknown as WorkspaceSnapshot;
const calls: { method: string; request: any }[] = [];
(window as any).recoveryCalls = calls;
let previewCount = 0;
window.go = { main: { App: { PickFile: async () => "", Dispatch: async (method, raw) => {
  const request = JSON.parse(raw); calls.push({ method, request });
  const ok = (result: unknown) => JSON.stringify({ ok: true, result });
  if (method === "recovery.settings") return ok({ historyPolicy: "Keep history.", retainedRevisions: 5, trashPolicy: "Retain trash.", recordingPolicy: "Retain audio.", backupPolicy: "Review before cleanup.", backupKeepCount: 0 });
  if (method === "history.list") return ok({ revisions: [{ revision: "2", createdAt: page.createdAt, title: page.title, deleted: false }], snapshotCursor: "10" });
  if (method === "history.previewRestore") {
    if ((window as any).failNextRecoveryReview) { (window as any).failNextRecoveryReview = false; return JSON.stringify({ ok: false, error: { code: "CONFLICT", message: "Property changed; review again.", retryable: false, requestId: request.requestId } }); }
    previewCount++;
    const opaque = (window as any).opaqueHistoricalEvidence;
    const historicalValue = { propertyId: property.id, schemaVersion: opaque ? 9 : 1, value: opaque ? { sourcePayload: "legacy opaque value" } : { decimal: "12", format: "number" } };
    const historicalProperty = { ...property, schemaVersion: opaque ? 9 : 1, type: opaque ? "multi_select" : "number", config: { format: "number" }, revision: "1" };
    return ok({ previewId: id(20 + previewCount), pageId: page.id, restoreRevision: "2", expiresAt: "2030-10-04T11:10:00Z", expectedRevisions: [{ target: { entityType: "page", entityId: page.id }, revision: "5" }], affectedFileIds: [], snapshot: { page: { ...page, revision: "2" }, blocks: [], snapshotCursor: "10" }, incompatibleValues: [{ historicalValue, historicalProperty, currentProperty: property, reason: "Historical property schema changed." }], targetProperties: [property], canRestore: !!request.propertyDecisions?.length, warnings: request.propertyDecisions?.[0]?.action === "retain" ? ["Retain old value as inert evidence; keep current value."] : [] });
  }
  if (method === "batch") return ok({ transactionId: id(50), committedEventCursor: "11", revisions: [], createdIds: [] });
  throw new Error(`Unexpected fixture method ${method}`);
} } } };
createRoot(document.getElementById("root")!).render(<><RecoveryPanel workspace={w} onCommitted={async () => {}} onError={error => { document.getElementById("fixture-error")!.textContent = String(error); }} /><p id="fixture-error" role="alert" /></>);
