import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type { NativeBackupProgress, NativeBackupSummary } from "../../../contracts/generated/service";

type Manifest = { backupId: string; workspaceId: string; createdAt: string; eventCursor: string; assets: unknown[]; recordings?: unknown[] };
type Summary = NativeBackupSummary;
type Review = { previewId: string; sourceName: string; manifest: Manifest; warnings: string[]; summary?: Summary };
type Progress = NativeBackupProgress;
type Settings = { historyPolicy: string; trashPolicy: string; recordingPolicy: string; backupPolicy: string; backupKeepCount: number; retainedRevisions: number; backups: number };
type Cleanup = { previewId: string; plan: { keepCount: number; remove: { name: string; manifest: Manifest }[]; warnings: string[] } };
type Bridge = { PreviewNativeRestore(): Promise<string>; RestoreNativeBackup(id: string): Promise<string>; PreviewBackupCleanup(): Promise<string>; ApplyBackupCleanup(id: string): Promise<number>; CreateEncryptedBackup(passphrase: string): Promise<string>; PreviewEncryptedBackup(passphrase: string): Promise<string>; CloseNativeRestorePreview(id: string): Promise<void>; NativeBackupProgress(): Promise<string>; CancelBackupOperation(): Promise<void> };
function bridge(): Bridge { const value = window.go?.main.App as unknown as Partial<Bridge> | undefined; if (!value?.PreviewNativeRestore) throw new Error("Native backup restore is available in the desktop application."); return value as Bridge; }

export default function BackupRestoreControls({ workspace: w, onError }: { workspace: WorkspaceSnapshot; onError(error: unknown): void }) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [keep, setKeep] = useState(0);
  const [review, setReview] = useState<Review | null>(null);
  const [cleanup, setCleanup] = useState<Cleanup | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [progress, setProgress] = useState<Progress | null>(null);
  const password = useRef<HTMLInputElement>(null);
  const confirmation = useRef<HTMLInputElement>(null);
  const restorePassword = useRef<HTMLInputElement>(null);
  const currentReview = useRef<Review | null>(null);
  currentReview.current = review;
  useEffect(() => { let cancelled = false; dispatch<Settings>("recovery.settings", envelope(w)).then(value => { if (!cancelled) { setSettings(value); setKeep(value.backupKeepCount); } }).catch(onError); return () => { cancelled = true; }; }, [w.workspaceId]);
  useEffect(() => {
    if (!busy) { setProgress(null); return; }
    let alive = true;
    const timer = setInterval(() => { bridge().NativeBackupProgress().then(raw => { if (alive) setProgress(JSON.parse(raw)); }).catch(() => {}); }, 500);
    return () => { alive = false; clearInterval(timer); };
  }, [busy]);
  useEffect(() => () => { const app = window.go?.main.App as unknown as Partial<Bridge> | undefined; void app?.CancelBackupOperation?.(); if (currentReview.current) void app?.CloseNativeRestorePreview?.(currentReview.current.previewId); }, [w.workspaceId]);
  async function replaceReview(value: Review | null) { if (review) await bridge().CloseNativeRestorePreview(review.previewId); setReview(value); }
  async function run(action: () => Promise<void>) { setBusy(true); setMessage(""); try { await action(); } catch (error) { onError(error); } finally { setBusy(false); } }
  return <div aria-label="Native backup restoration and retention">
    <h3>Encrypted portable backup</h3>
    <p>Save a passphrase-protected .trove.age file containing the complete workspace database, retained managed files and recordings. The database includes search tables and saved approval/job state. Native credential-store secrets, model downloads and original external files you have not acquired are excluded.</p>
    <p>Restoration rebuilds search, revokes action approvals and requires fresh authorization for external folders. Indexed excerpts of external files can be present in the encrypted database snapshot.</p>
    <p>Keep the passphrase somewhere safe. Trove does not save it and cannot recover it. Private temporary files are removed on completion, cancellation or the next launch after interruption.</p>
    <label>New backup passphrase <input ref={password} type="password" autoComplete="new-password" disabled={busy} maxLength={4096} /></label>
    <label>Confirm backup passphrase <input ref={confirmation} type="password" autoComplete="new-password" disabled={busy} maxLength={4096} /></label>
    <button disabled={busy} onClick={() => run(async () => {
      let secret = password.current?.value ?? "";
      const matches = secret === confirmation.current?.value;
      if (password.current) password.current.value = "";
      if (confirmation.current) confirmation.current.value = "";
      if (!secret || !matches) throw new Error("Enter a passphrase and matching confirmation.");
      try { const request = bridge().CreateEncryptedBackup(secret); secret = ""; const raw = await request; if (raw) { const result = JSON.parse(raw) as { destination: string }; setMessage(`Encrypted backup saved at ${result.destination}.`); } } finally { secret = ""; }
    })}>Create encrypted backup…</button>
    <h3>Restore a native backup</h3>
    <p>Choose a verified backup, review its contents, then select a destination. Trove creates a new workspace folder and rebuilds search.</p>
    <label>Encrypted backup passphrase <input ref={restorePassword} type="password" autoComplete="off" disabled={busy} maxLength={4096} /></label>
    <button disabled={busy} onClick={() => run(async () => {
      let secret = restorePassword.current?.value ?? "";
      if (restorePassword.current) restorePassword.current.value = "";
      if (!secret) throw new Error("Enter the encrypted backup passphrase.");
      try { await replaceReview(null); const request = bridge().PreviewEncryptedBackup(secret); secret = ""; const raw = await request; if (raw) setReview(JSON.parse(raw)); } finally { secret = ""; }
    })}>Choose encrypted backup to review…</button>
    <button disabled={busy} onClick={() => run(async () => { await replaceReview(null); const raw = await bridge().PreviewNativeRestore(); if (raw) setReview(JSON.parse(raw)); })}>Choose legacy backup folder…</button>
    {review && <article aria-label="Native restore preview">
      <h4>{review.sourceName}</h4>
      <p>Created {new Date(review.manifest.createdAt).toLocaleString()} · {review.manifest.assets.length} managed files · {review.manifest.recordings?.length ?? 0} recording objects.</p>
      <p>Workspace {review.manifest.workspaceId} · saved through event {review.manifest.eventCursor}.</p>
      {review.warnings.map(text => <p key={text}>{text}</p>)}
      {review.summary && <><p>Included: {review.summary.included.join("; ")}.</p><p>Excluded: {review.summary.excluded.join("; ")}.</p><p>On restoration: {review.summary.restoration.join("; ")}.</p></>}
      <button disabled={busy} onClick={() => run(async () => { const raw = await bridge().RestoreNativeBackup(review.previewId); setReview(null); if (raw) { const result = JSON.parse(raw) as { destination: string }; setMessage(`Restored and verified at ${result.destination}. Use Open workspace to open this installation.`); } })}>Restore into a new folder…</button>
      <button disabled={busy} onClick={() => run(() => replaceReview(null))}>Cancel review</button>
    </article>}
    <h3>Retention settings</h3>
    <p>Reviewed automatic cleanup applies to verified legacy backup folders. Encrypted archives are retained; delete those files explicitly when you no longer need them.</p>
    {settings && <><p>{settings.historyPolicy} {settings.retainedRevisions} revisions retained.</p><p>{settings.trashPolicy} {settings.recordingPolicy}</p><p>{settings.backupPolicy}</p></>}
    <label>External backup cleanup target <select aria-label="Backup retention count" value={keep} disabled={busy} onChange={e => setKeep(Number(e.target.value))}><option value={0}>Keep all backups</option>{[1, 3, 5, 10, 20, 50].map(value => <option key={value} value={value}>Keep newest {value} verified backups</option>)}</select></label>
    <button disabled={busy || !settings} onClick={() => run(async () => { const value = await dispatch<Settings>("recovery.setRetention", { ...envelope(w), backupKeepCount: keep }); setSettings(value); setCleanup(null); setMessage("Retention target saved. Backups are only deleted after review."); })}>Save retention setting</button>
    <button disabled={busy || !settings?.backupKeepCount} onClick={() => run(async () => { const raw = await bridge().PreviewBackupCleanup(); if (raw) setCleanup(JSON.parse(raw)); })}>Review external backup cleanup…</button>
    {cleanup && <article aria-label="Backup cleanup preview"><p>Keep the newest {cleanup.plan.keepCount} verified backups for this workspace. Delete {cleanup.plan.remove.length} older backups in the selected folder.</p><ul>{cleanup.plan.remove.map(item => <li key={item.name}>{item.name} · {new Date(item.manifest.createdAt).toLocaleString()}</li>)}</ul>{cleanup.plan.warnings.map((text, i) => <p key={i}>{text}</p>)}<button disabled={busy || !cleanup.plan.remove.length} onClick={() => run(async () => { const count = await bridge().ApplyBackupCleanup(cleanup.previewId); setCleanup(null); setMessage(`Deleted ${count} reviewed external backups.`); })}>Delete these reviewed backups</button><button disabled={busy} onClick={() => setCleanup(null)}>Cancel cleanup</button></article>}
    {busy && <button onClick={() => bridge().CancelBackupOperation().catch(onError)}>Cancel backup operation</button>}
    <p role="status" aria-live="polite">{busy ? (({ snapshot: "Creating a consistent workspace snapshot…", encrypting: "Encrypting backup…", authenticating: "Authenticating the entire encrypted archive…", verifying: "Verifying archive contents…", restoring: "Restoring into a new workspace…" } as Partial<Record<Progress["phase"], string>>)[progress?.phase ?? "idle"] ?? "Preparing recovery operation…") : message}</p>
    {busy && progress && Number(progress.totalBytes) > 0 && <progress aria-label="Backup operation progress" max={Number(progress.totalBytes)} value={Number(progress.bytes)} />}
  </div>;
}
