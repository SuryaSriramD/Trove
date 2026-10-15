import React from "react";
import { createRoot } from "react-dom/client";
import BackupRestoreControls from "../src/recovery/BackupRestoreControls";
import type { WorkspaceSnapshot } from "../src/api";

const calls: string[] = [];
(window as any).backupCalls = calls;
let rejectPending: ((error: Error) => void) | undefined;
const native = {
  Dispatch: async (method: string, raw: string) => { calls.push(method); if (raw.includes("test-passphrase")) throw new Error("Passphrase reached the service dispatcher"); return JSON.stringify({ ok: true, result: { historyPolicy: "Keep history.", retainedRevisions: 1, trashPolicy: "Retain trash.", recordingPolicy: "Retain audio.", backupPolicy: "Reviewed cleanup.", backupKeepCount: 0 } }); },
  PickFile: async () => "",
  PreviewNativeRestore: async () => "",
  CreateEncryptedBackup: async (secret: string) => { if (secret !== "test-passphrase") throw new Error("Unexpected fixture passphrase"); calls.push("create"); if ((window as any).holdBackup) return new Promise<string>((_, reject) => { rejectPending = reject; }); return JSON.stringify({ destination: "/chosen/backup.trove.age" }); },
  PreviewEncryptedBackup: async (secret: string) => { if (secret !== "test-passphrase") throw new Error("Unexpected fixture passphrase"); calls.push("preview"); return JSON.stringify({ previewId: "review-1", sourceName: "backup.trove.age", manifest: { backupId: "b", workspaceId: "w", createdAt: "2026-10-15T10:00:00Z", eventCursor: "12", assets: [1], recordings: [1, 2] }, warnings: ["The complete archive was authenticated before its contents were read."] }); },
  CloseNativeRestorePreview: async (id: string) => { calls.push(`close:${id}`); },
  RestoreNativeBackup: async (id: string) => { calls.push(`restore:${id}`); return JSON.stringify({ destination: "/chosen/restored" }); },
  NativeBackupProgress: async () => JSON.stringify({ version: 1, phase: "authenticating", bytes: "50", totalBytes: "100" }),
  CancelBackupOperation: async () => { calls.push("cancel"); rejectPending?.(new Error("Backup cancelled")); },
};
window.go = { main: { App: native } };
const w = { workspaceId: "fixture-workspace", scopeGrantId: "fixture-scope" } as WorkspaceSnapshot;
createRoot(document.getElementById("root")!).render(<><BackupRestoreControls workspace={w} onError={error => { document.getElementById("fixture-error")!.textContent = String(error); }} /><p id="fixture-error" role="alert" /></>);
