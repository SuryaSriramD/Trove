import { useState } from "react";

type AppIdentity = { version: string; build: number; teamId: string; userVersion: number; storageGeneration: number };
type UpdateReview = { previewId: string; current: AppIdentity; update: AppIdentity; workspaceId: string; bytes: number; warnings: string[] };
type RollbackReview = { previewId: string; previous: AppIdentity; createdAt: string; snapshot: { workspaceId: string }; warnings: string[] };
type MigrationReview = { previewId: string; snapshot: { workspaceId: string; createdAt: string; before: { userVersion: number; storageGeneration: number }; objects: unknown[] }; warnings: string[] };
type Result = { previewId?: string; launchReviewId?: string; destination: string; application?: string; message: string };
type Bridge = {
  PreviewApplicationUpdate(): Promise<string>; PrepareApplicationUpdate(id: string): Promise<string>;
  PreviewApplicationRollback(): Promise<string>; RestoreApplicationRollback(id: string): Promise<string>;
  RevealApplicationUpdate(id: string): Promise<void>; CancelUpdateOperation(): Promise<void>;
  PreviewMigrationRecovery(): Promise<string>; RestoreMigrationRecovery(id: string): Promise<string>;
  LaunchApplicationRollback(id: string): Promise<void>;
};
function bridge(): Bridge { const value = window.go?.main.App as unknown as Partial<Bridge>; if (!value?.PreviewMigrationRecovery) throw new Error("Update recovery is available in the desktop application."); return value as Bridge; }

export default function UpdateRecoveryControls({ workspaceId, onError }: { workspaceId?: string; onError(error: unknown): void }) {
  const [busy, setBusy] = useState(false);
  const [update, setUpdate] = useState<UpdateReview | null>(null);
  const [rollback, setRollback] = useState<RollbackReview | null>(null);
  const [migration, setMigration] = useState<MigrationReview | null>(null);
  const [result, setResult] = useState<Result | null>(null);
  const reset = () => { setUpdate(null); setRollback(null); setMigration(null); setResult(null); };
  const run = async (fn: () => Promise<void>) => { setBusy(true); try { await fn(); } catch (error) { onError(error); } finally { setBusy(false); } };
  return <section className="settings-section" aria-label="Application update and recovery">
    <h2>Update and recovery</h2>
    <p>Review a signed macOS update and save a recovery folder containing both apps and a matching workspace backup. Installation uses a manual restart.</p>
    <div className="action-row">
      <button disabled={busy || !workspaceId} onClick={() => void run(async () => { reset(); const raw = await bridge().PreviewApplicationUpdate(); if (raw) setUpdate(JSON.parse(raw)); })}>Choose update to verify…</button>
      <button disabled={busy} onClick={() => void run(async () => { reset(); const raw = await bridge().PreviewApplicationRollback(); if (raw) setRollback(JSON.parse(raw)); })}>Review compatible rollback…</button>
      <button disabled={busy} onClick={() => void run(async () => { reset(); const raw = await bridge().PreviewMigrationRecovery(); if (raw) setMigration(JSON.parse(raw)); })}>Recover an interrupted upgrade…</button>
      {busy && <button onClick={() => void bridge().CancelUpdateOperation().catch(onError)}>Cancel recovery operation</button>}
    </div>
    {update && <article aria-label="Application update review">
      <h3>Review update</h3>
      <p>Version {update.current.version} (build {update.current.build}) → {update.update.version} (build {update.update.build})</p>
      <p>Verified publisher: {update.update.teamId}. Application copies need {(update.bytes / 1024 / 1024).toFixed(1)} MiB, plus the workspace backup.</p>
      {update.warnings.map(text => <p key={text}>{text}</p>)}
      {workspaceId !== update.workspaceId && <p role="alert">The workspace changed. Choose the update again.</p>}
      <button disabled={busy || workspaceId !== update.workspaceId} onClick={() => void run(async () => { const id = update.previewId; setUpdate(null); const raw = await bridge().PrepareApplicationUpdate(id); if (raw) setResult(JSON.parse(raw)); })}>Save verified apps and workspace backup…</button>
      <button disabled={busy} onClick={reset}>Discard review</button>
    </article>}
    {rollback && <article aria-label="Compatible rollback review">
      <h3>Restore for version {rollback.previous.version} (build {rollback.previous.build})</h3>
      <p>Backup saved {new Date(rollback.createdAt).toLocaleString()}.</p>
      {rollback.warnings.map(text => <p key={text}>{text}</p>)}
      <button disabled={busy} onClick={() => void run(async () => { const id = rollback.previewId; setRollback(null); const raw = await bridge().RestoreApplicationRollback(id); if (raw) setResult(JSON.parse(raw)); })}>Restore compatible workspace into a new folder…</button>
      <button disabled={busy} onClick={reset}>Discard review</button>
    </article>}
    {migration && <article aria-label="Migration recovery review">
      <h3>Restore the pre-upgrade workspace</h3>
      <p>Snapshot saved {new Date(migration.snapshot.createdAt).toLocaleString()}, including {migration.snapshot.objects.length} managed file and recording objects.</p>
      {migration.warnings.map(text => <p key={text}>{text}</p>)}
      <button disabled={busy} onClick={() => void run(async () => { const id = migration.previewId; setMigration(null); const raw = await bridge().RestoreMigrationRecovery(id); if (raw) setResult(JSON.parse(raw)); })}>Restore pre-upgrade workspace into a new folder…</button>
      <button disabled={busy} onClick={reset}>Discard review</button>
    </article>}
    {result && <article aria-label="Recovery result" role="status"><p>{result.message}</p><p>Workspace or recovery folder: <code>{result.destination}</code></p>{result.application && <p>Saved application: <code>{result.application}</code></p>}
      {result.previewId && <button disabled={busy} onClick={() => void run(async () => { const id = result.previewId!; setResult({ ...result, previewId: undefined }); await bridge().RevealApplicationUpdate(id); })}>Verify again and show update in Finder</button>}
      {result.launchReviewId && <button disabled={busy} onClick={() => void run(async () => { const id = result.launchReviewId!; setResult({ ...result, launchReviewId: undefined }); await bridge().LaunchApplicationRollback(id); })}>Open restored workspace with previous app and quit</button>}
    </article>}
  </section>;
}
