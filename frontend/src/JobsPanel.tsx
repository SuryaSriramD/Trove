import { useCallback, useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "./api";
import type { Job } from "../../contracts/generated/jobs";

const labels: Record<string, string> = { IMPORT_NOTION: "Import", EXTRACT_DOCUMENT: "Document extraction", INDEX_CONTENT: "Keyword indexing", GENERATE_EMBEDDINGS: "Semantic indexing", TRANSCRIBE_AUDIO: "Transcription", SUMMARIZE_MEETING: "Meeting notes", BACKUP_WORKSPACE: "Workspace backup" };
export function JobsPanel({ workspace }: { workspace: WorkspaceSnapshot }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const refresh = useCallback(async () => {
    const result = await dispatch<{ jobs: Job[] }>("jobs.list", { ...envelope(workspace), limit: 100 });
    setJobs(result.jobs);
  }, [workspace.workspaceId]);
  useEffect(() => {
    let alive = true;
    const poll = () => void refresh().catch(e => { if (alive) setError(e.message); });
    poll();
    const timer = setInterval(poll, 2000);
    const off = window.runtime?.EventsOn("jobs:progress", poll);
    return () => { alive = false; clearInterval(timer); off?.(); };
  }, [refresh]);
  async function change(job: Job, method: "jobs.retry" | "cancelJob") {
    setBusy(job.jobId); setError("");
    try { await dispatch(method, { ...envelope(workspace), jobId: job.jobId, expectedRevision: job.revision }); await refresh(); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); await refresh(); }
    finally { setBusy(""); }
  }
  return <section className="jobs-panel" aria-label="Background jobs">
    <span className="eyebrow">WORK IN PROGRESS</span><h1>Jobs</h1>
    <p className="lede">Follow imports, extraction, transcription, and backups while you keep editing.</p>
    {error && <p role="alert" className="error-banner">{error}</p>}
    {!jobs.length && <p className="empty-state">No background jobs yet.</p>}
    <ol className="job-list">{jobs.map(job => <li key={job.jobId}>
      <div><strong>{labels[job.kind] ?? job.kind}</strong><span className="job-state">{job.state.replaceAll("_", " ")}</span></div>
      <small>{new Date(job.createdAt).toLocaleString()} · attempt {job.attempt}</small>
      {job.progress && <><p>{job.progress.message ?? `${job.progress.completed} ${job.progress.unit}`}</p>{job.progress.total != null && <progress value={job.progress.completed} max={job.progress.total || 1} aria-label={`${labels[job.kind] ?? job.kind} progress`} />}</>}
      {job.error && <p role="status">{job.error.message}</p>}
      <div className="action-row">
        {["queued", "running", "retry_wait"].includes(job.state) && <button disabled={busy === job.jobId} onClick={() => change(job, "cancelJob")}>Cancel</button>}
        {["failed", "cancelled", "interrupted"].includes(job.state) && <button disabled={busy === job.jobId} onClick={() => change(job, "jobs.retry")}>Retry saved inputs</button>}
      </div>
    </li>)}</ol>
    <p className="muted">Retry uses the saved input and completed checkpoints. If the source changed, start a fresh job from its page.</p>
  </section>;
}
