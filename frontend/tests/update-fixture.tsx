import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import UpdateRecoveryControls from "../src/recovery/UpdateRecoveryControls";
import "../src/style.css";
const calls: { method: string; args: unknown[] }[] = [];
(window as any).updateCalls = calls;
let release: ((value: string) => void) | null = null;
const identity = { version: "1.0.0", build: 10, teamId: "ABCDEFGHIJ", userVersion: 3, storageGeneration: 1 };
const call = async (method: string, ...args: unknown[]) => {
  calls.push({ method, args });
  if ((window as any).updateFailure) throw new Error("Application changed; review it again.");
  if (method === "PreviewApplicationUpdate") return JSON.stringify({ previewId: "native-update-review", current: identity, update: { ...identity, version: "1.1.0", build: 11 }, workspaceId: "workspace-current", bytes: 300 * 1024 * 1024, warnings: ["Save pending edits before continuing.", "Quit Trove before opening the verified update."] });
  if (method === "PrepareApplicationUpdate") {
    if ((window as any).delayUpdate) return new Promise<string>(resolve => { release = resolve; });
    return JSON.stringify({ previewId: "native-kit-review", destination: "/selected/Trove-update", message: "Recovery folder verified. Quit Trove, then open update/Trove.app." });
  }
  if (method === "PreviewApplicationRollback") return JSON.stringify({ previewId: "native-rollback-review", previous: identity, createdAt: "2026-10-05T12:00:00Z", snapshot: { workspaceId: "workspace-old" }, warnings: ["Restore creates a new workspace. Keep your newer workspace."] });
  if (method === "RestoreApplicationRollback") return JSON.stringify({ launchReviewId: "native-launch-review", destination: "/selected/Trove-rollback", application: "/selected/kit/previous/Trove.app", message: "Compatible workspace restored. Use the launch button to select the restored copy before the previous app starts." });
  if (method === "PreviewMigrationRecovery") return JSON.stringify({ previewId: "native-migration-review", snapshot: { workspaceId: "workspace-failed", createdAt: "2026-10-05T12:00:00Z", before: { userVersion: 2, storageGeneration: 0 }, objects: [{}, {}] }, warnings: ["Restore copies into a new folder."] });
  if (method === "RestoreMigrationRecovery") return JSON.stringify({ destination: "/selected/Trove-migration-recovery", message: "Pre-upgrade workspace restored. Use a compatible application to open this new folder." });
  if (method === "CancelUpdateOperation") { release?.(""); release = null; return ""; }
  if (method === "RevealApplicationUpdate") return "";
  if (method === "LaunchApplicationRollback") return "";
  throw new Error(`Unexpected fixture method: ${method}`);
};
const native: any = { Dispatch: async () => "{}", PickFile: async () => "" };
for (const method of ["PreviewApplicationUpdate", "PrepareApplicationUpdate", "PreviewApplicationRollback", "RestoreApplicationRollback", "RevealApplicationUpdate", "CancelUpdateOperation", "PreviewMigrationRecovery", "RestoreMigrationRecovery", "LaunchApplicationRollback"]) native[method] = (...args: unknown[]) => call(method, ...args);
window.go = { main: { App: native } };
function Fixture() { const [id, setId] = useState<string | undefined>(new URLSearchParams(location.search).has("startupFailure") ? undefined : "workspace-current"); const [error, setError] = useState(""); return <main><button onClick={() => setId("workspace-changed")}>Fixture switch workspace</button><UpdateRecoveryControls workspaceId={id} onError={error => setError(String(error))} /><p role="alert">{error}</p></main>; }
createRoot(document.getElementById("root")!).render(<Fixture />);
