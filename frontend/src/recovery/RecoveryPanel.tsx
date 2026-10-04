import { useState } from "react";
import BackupRestoreControls from "./BackupRestoreControls";
import HistoricalValueReview from "./HistoricalValueReview";
import { batch, dispatch, envelope, requestID, type S, type WorkspaceSnapshot } from "../api";

function readable(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(readable).join("");
  if (value && typeof value === "object") {
    const data = value as Record<string, unknown>;
    for (const key of ["richText", "text", "readableText", "code", "source", "latex", "caption"])
      if (data[key] !== undefined) return readable(data[key]);
  }
  return "";
}

export default function RecoveryPanel({ workspace: w, onCommitted, onError }: {
  workspace: WorkspaceSnapshot;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const [pageId, setPageId] = useState("");
  const [history, setHistory] = useState<S.HistoryListResult | null>(null);
  const [preview, setPreview] = useState<S.HistoryPreviewRestoreResult | null>(null);
  const [propertyDecisions, setPropertyDecisions] = useState<Record<string, S.HistoryPropertyDecision>>({});
  const [decisionsChanged, setDecisionsChanged] = useState(false);
  const [subtree, setSubtree] = useState<{ pages: S.HistoryPreviewRestoreResult[]; commands: S.Command[]; expectedRevisions: S.ExpectedRevision[]; warnings: string[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  async function run(action: () => Promise<void>) {
    setBusy(true); setMessage("");
    try { await action(); } catch (error) { onError(error); }
    finally { setBusy(false); }
  }
  return <section className="recovery-panel" aria-label="History and recovery">
    <h2>History and recovery</h2>
    <p>Restore a saved page revision, including its attachments. Preview the content before restoring.</p>
    <label>Page <select aria-label="Page history" disabled={busy} value={pageId} onChange={e => {
      setPageId(e.target.value); setHistory(null); setPreview(null); setSubtree(null); setPropertyDecisions({}); setDecisionsChanged(false);
    }}><option value="">Choose a page</option>{w.pages.map(p => <option key={p.id} value={p.id}>{p.title || "Untitled"}{p.deletedAt ? " (in trash)" : ""}</option>)}</select></label>
    <button disabled={!pageId || busy} onClick={() => run(async () => {
      setHistory(await dispatch<S.HistoryListResult>("history.list", { ...envelope(w), pageId }));
      setPreview(null);
    })}>Load revisions</button>
    {history && <ul>{history.revisions.map(r => <li key={r.revision}>
      <button disabled={busy} onClick={() => run(async () => {
        setPropertyDecisions({}); setDecisionsChanged(false);
        setPreview(await dispatch<S.HistoryPreviewRestoreResult>("history.previewRestore", { ...envelope(w), pageId, restoreRevision: r.revision }));
      })}>Preview revision {r.revision} · {new Date(r.createdAt).toLocaleString()} · {r.title}{r.deleted ? " · trashed" : ""}</button>
    </li>)}</ul>}
    {preview && <article aria-label="Restoration preview">
      <h3>{preview.snapshot.page.title || "Untitled"} · revision {preview.restoreRevision}</h3>
      <p>{preview.snapshot.blocks.length} blocks · {preview.affectedFileIds.length} retained files. Restoring creates a new revision.</p>
      {preview.warnings.map((warning, index) => <p key={index}>{warning}</p>)}
      {!!preview.incompatibleValues?.length && <>
        <HistoricalValueReview issues={preview.incompatibleValues} targets={preview.targetProperties ?? []} decisions={propertyDecisions} workspace={w} disabled={busy} onDecision={decision => { setPropertyDecisions(current => ({ ...current, [decision.sourcePropertyId]: decision })); setDecisionsChanged(true); }} />
        <button disabled={busy || !preview.incompatibleValues.every(issue => { const decision = propertyDecisions[issue.historicalValue.propertyId]; return decision && (decision.action === "retain" || decision.targetPropertyId); })} onClick={() => run(async () => {
          setPreview(await dispatch<S.HistoryPreviewRestoreResult>("history.previewRestore", { ...envelope(w), pageId: preview.pageId, restoreRevision: preview.restoreRevision, propertyDecisions: Object.values(propertyDecisions) }));
          setDecisionsChanged(false);
        })}>Review property decisions</button>
        {decisionsChanged && <p>Review these changes before restoring.</p>}
      </>}
      <div style={{ maxHeight: 320, overflow: "auto", whiteSpace: "pre-wrap" }}>{preview.snapshot.blocks.map(block => <div key={block.id}><small>{block.type}</small><p>{readable(block.data) || "Saved content retained"}</p></div>)}</div>
      <button disabled={busy || preview.canRestore === false || decisionsChanged} onClick={() => run(async () => {
        await batch(w, [{ kind: "history.restore", pageId: preview.pageId, restoreRevision: preview.restoreRevision, previewId: preview.previewId }], preview.expectedRevisions);
        setPreview(null); setHistory(null); await onCommitted(); setMessage("Page restored as a new revision.");
      })}>Restore this revision</button>
    </article>}
    <button disabled={busy || !pageId || !w.pages.find(page => page.id === pageId)?.deletedAt} onClick={() => run(async () => {
      setSubtree(await dispatch("history.previewSubtree", { ...envelope(w), pageId }));
    })}>Preview trash subtree recovery</button>
    {subtree && <article aria-label="Subtree restoration preview"><h3>Restore {subtree.pages.length} pages together</h3><ul>{subtree.pages.map(page => <li key={page.pageId}>{page.snapshot.page.title || "Untitled"} · revision {page.restoreRevision} · {page.snapshot.blocks.length} blocks</li>)}</ul>{subtree.warnings.map(warning => <p key={warning}>{warning}</p>)}<button disabled={busy} onClick={() => run(async () => { await batch(w, subtree.commands, subtree.expectedRevisions); setSubtree(null); setPreview(null); setHistory(null); await onCommitted(); setMessage("Subtree restored as new page revisions."); })}>Restore reviewed subtree</button><button disabled={busy} onClick={() => setSubtree(null)}>Cancel subtree review</button></article>}
    <h3>Recovery points and backups</h3>
    <p>A recovery point marks the current workspace history. A backup saves a consistent database and its retained files to a folder you choose.</p>
    <button disabled={busy} onClick={() => run(async () => {
      const result = await dispatch<S.HistoryRecoveryPointResult>("history.recoveryPoint", { ...envelope(w), label: `Manual recovery point ${new Date().toLocaleString()}`, idempotencyKey: requestID() });
      setMessage(`Recovery point saved at ${new Date(result.createdAt).toLocaleString()}.`);
    })}>Create recovery point</button>
    <button disabled={busy} onClick={() => run(async () => {
      const bridge = window.go?.main.App as { CreateBackup?: () => Promise<string> } | undefined;
      if (!bridge?.CreateBackup) throw new Error("Backup creation is available in the desktop application.");
      const result = await bridge.CreateBackup();
      if (result) setMessage("Backup created and verified in the selected folder.");
    })}>Create backup…</button>
    <BackupRestoreControls workspace={w} onError={onError} />
    <p role="status" aria-live="polite">{busy ? "Working…" : message}</p>
  </section>;
}
