import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";

type Manifest = { backupId: string; workspaceId: string; createdAt: string; eventCursor: string; assets: unknown[]; recordings?: unknown[] };
type Review = { previewId: string; sourceName: string; manifest: Manifest; warnings: string[] };
type Settings = { historyPolicy: string; trashPolicy: string; recordingPolicy: string; backupPolicy: string; backupKeepCount: number; retainedRevisions: number; backups: number };
type Cleanup = { previewId: string; plan: { keepCount: number; remove: { name: string; manifest: Manifest }[]; warnings: string[] } };
type Bridge = { PreviewNativeRestore(): Promise<string>; RestoreNativeBackup(id: string): Promise<string>; PreviewBackupCleanup(): Promise<string>; ApplyBackupCleanup(id: string): Promise<number> };
function bridge(): Bridge { const value = window.go?.main.App as unknown as Partial<Bridge> | undefined; if (!value?.PreviewNativeRestore) throw new Error("Native backup restore is available in the desktop application."); return value as Bridge; }

export default function BackupRestoreControls({ workspace: w, onError }: { workspace: WorkspaceSnapshot; onError(error: unknown): void }) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [keep, setKeep] = useState(0);
  const [review, setReview] = useState<Review | null>(null);
  const [cleanup, setCleanup] = useState<Cleanup | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  useEffect(() => { let cancelled = false; dispatch<Settings>("recovery.settings", envelope(w)).then(value => { if (!cancelled) { setSettings(value); setKeep(value.backupKeepCount); } }).catch(onError); return () => { cancelled = true; }; }, [w.workspaceId]);
  async function run(action: () => Promise<void>) { setBusy(true); setMessage(""); try { await action(); } catch (error) { onError(error); } finally { setBusy(false); } }
  return <div aria-label="Native backup restoration and retention">
    <h3>Restore a native backup</h3>
    <p>Choose a verified backup, review its contents, then select a destination. Trove creates a new workspace folder and rebuilds search.</p>
    <button disabled={busy} onClick={() => run(async () => { const raw = await bridge().PreviewNativeRestore(); if (raw) setReview(JSON.parse(raw)); })}>Choose backup to review…</button>
    {review && <article aria-label="Native restore preview">
      <h4>{review.sourceName}</h4>
      <p>Created {new Date(review.manifest.createdAt).toLocaleString()} · {review.manifest.assets.length} managed files · {review.manifest.recordings?.length ?? 0} recording objects.</p>
      <p>Workspace {review.manifest.workspaceId} · saved through event {review.manifest.eventCursor}.</p>
      {review.warnings.map(text => <p key={text}>{text}</p>)}
      <button disabled={busy} onClick={() => run(async () => { const raw = await bridge().RestoreNativeBackup(review.previewId); setReview(null); if (raw) { const result = JSON.parse(raw) as { destination: string }; setMessage(`Restored and verified at ${result.destination}. Use Open workspace to open this installation.`); } })}>Restore into a new folder…</button>
      <button disabled={busy} onClick={() => setReview(null)}>Cancel review</button>
    </article>}
    <h3>Retention settings</h3>
    {settings && <><p>{settings.historyPolicy} {settings.retainedRevisions} revisions retained.</p><p>{settings.trashPolicy} {settings.recordingPolicy}</p><p>{settings.backupPolicy}</p></>}
    <label>External backup cleanup target <select aria-label="Backup retention count" value={keep} disabled={busy} onChange={e => setKeep(Number(e.target.value))}><option value={0}>Keep all backups</option>{[1, 3, 5, 10, 20, 50].map(value => <option key={value} value={value}>Keep newest {value} verified backups</option>)}</select></label>
    <button disabled={busy || !settings} onClick={() => run(async () => { const value = await dispatch<Settings>("recovery.setRetention", { ...envelope(w), backupKeepCount: keep }); setSettings(value); setCleanup(null); setMessage("Retention target saved. Backups are only deleted after review."); })}>Save retention setting</button>
    <button disabled={busy || !settings?.backupKeepCount} onClick={() => run(async () => { const raw = await bridge().PreviewBackupCleanup(); if (raw) setCleanup(JSON.parse(raw)); })}>Review external backup cleanup…</button>
    {cleanup && <article aria-label="Backup cleanup preview"><p>Keep the newest {cleanup.plan.keepCount} verified backups for this workspace. Delete {cleanup.plan.remove.length} older backups in the selected folder.</p><ul>{cleanup.plan.remove.map(item => <li key={item.name}>{item.name} · {new Date(item.manifest.createdAt).toLocaleString()}</li>)}</ul>{cleanup.plan.warnings.map((text, i) => <p key={i}>{text}</p>)}<button disabled={busy || !cleanup.plan.remove.length} onClick={() => run(async () => { const count = await bridge().ApplyBackupCleanup(cleanup.previewId); setCleanup(null); setMessage(`Deleted ${count} reviewed external backups.`); })}>Delete these reviewed backups</button><button disabled={busy} onClick={() => setCleanup(null)}>Cancel cleanup</button></article>}
    <p role="status" aria-live="polite">{busy ? "Verifying recovery data…" : message}</p>
  </div>;
}
