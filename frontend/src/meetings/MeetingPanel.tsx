import { useCallback, useEffect, useRef, useState } from "react";
import { dispatch, envelope, fileURL, requestID, type D, type WorkspaceSnapshot } from "../api";
import "./meetings.css";
import { MeetingRetentionControls } from "./MeetingRetentionControls";
import { TranscriptCorrection } from "./TranscriptCorrection";
import type { NativeAudioInput, NativeRecordingDraft } from "../../../contracts/generated/service";

type Recording = NativeRecordingDraft;
type Job = { jobId: string; kind: string; state: string; revision: string; payload: { meetingId?: string }; progress?: { completed: number; total?: number; message?: string }; error?: { message: string } };
type Pack = { id: string; name: string; kind: string; qualificationOnly?: boolean };
type Claim = { text: string; segmentIds: string[] };
type Intelligence = { summary: Claim[]; keyPoints: Claim[]; decisions: Claim[]; questions: Claim[]; actionItems: Claim[] };
type Draft = { id: string; state: string; stale: boolean; evidenceHash: string; body: Intelligence };
type Extraction = { version: 1; proposalId: string; itemId: string; meetingId: string; evidenceHash: string; title: string; body: string; kind: "page" | "task"; pageId: string; appliedAt?: string; stale: boolean };
type Bridge = { MeetingRecordingDetails?(meetingId: string): Promise<Recording | null>; StartMeetingRecordingWithOptions(title: string, inputDeviceId: string, disclosureAcknowledged: boolean): Promise<Recording>; MeetingAudioInputs(): Promise<NativeAudioInput[]>; ControlMeetingRecording(id: string, action: "pause" | "resume"): Promise<Recording>; StopMeetingRecording(id: string): Promise<Recording>; SaveMeetingRecording(id: string): Promise<Recording>; MeetingRecordings(): Promise<Recording[]>; PickMeetingAudio(title: string): Promise<Recording> };
const bridge = () => window.go?.main.App as unknown as Bridge | undefined;
const timestamp = (ms: number) => `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, "0")}`;

export function MeetingPanel({ workspace: w, onRefresh, onNavigate, pageId }: { workspace: WorkspaceSnapshot; onRefresh: () => Promise<unknown> | void; onNavigate: (location: D.Location) => void; pageId?: string }) {
  const [title, setTitle] = useState("");
  const [inputs, setInputs] = useState<NativeAudioInput[]>([]);
  const [inputDeviceId, setInputDeviceId] = useState("");
  const [disclosed, setDisclosed] = useState(false);
  const [savedCapture, setSavedCapture] = useState<Recording | null>(null);
  const [recordings, setRecordings] = useState<Recording[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [packs, setPacks] = useState<Pack[]>([]);
  const [speech, setSpeech] = useState("");
  const [chat, setChat] = useState("");
  const [selected, setSelected] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [playbackError, setPlaybackError] = useState("");
  const [playbackLoading, setPlaybackLoading] = useState(false);
  const [playbackSource, setPlaybackSource] = useState<{ fileId: string; url: string } | null>(null);
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [review, setReview] = useState<Extraction | null>(null);
  const [extractions, setExtractions] = useState<Extraction[]>([]);
  const draftEpoch = useRef(0);
  const refreshEpoch = useRef(0);
  const currentScope = useRef("");
  const [draftScope, setDraftScope] = useState("");
  const audio = useRef<HTMLAudioElement>(null);
  const meetings = w.meetings.filter(m => (!pageId || m.pageId === pageId) && w.pages.some(p => p.id === m.pageId && !p.deletedAt));
  const meeting = meetings.find(m => m.id === selected) ?? meetings[0];
  const scope = `${w.workspaceId}:${meeting?.id ?? ""}`;
  currentScope.current = scope;
  const visibleDrafts = draftScope === scope ? drafts : [];
  const visibleExtractions = draftScope === scope ? extractions : [];
  useEffect(() => { setReview(null); return () => { draftEpoch.current++; refreshEpoch.current++; }; }, [scope]);
  useEffect(() => {
    let active = true; setSavedCapture(null);
    if (meeting) void bridge()?.MeetingRecordingDetails?.(meeting.id).then(result => { if (active) setSavedCapture(result); }).catch(() => undefined);
    return () => { active = false; };
  }, [scope]);
  const page = w.pages.find(p => p.id === meeting?.pageId);
  const segments = w.transcriptSegments.filter(s => s.meetingId === meeting?.id).sort((a, b) => a.startMs - b.startMs);
  const live = recordings.find(r => r.state === "recording" || r.state === "paused");
  useEffect(() => {
    const fileId = meeting?.recordingFileId;
    const controller = new AbortController();
    const player = audio.current;
    let objectURL: string | undefined;
    setPlaybackError("");
    setPlaybackSource(null);
    setPlaybackLoading(!!fileId);
    if (fileId) {
      // WKWebView's native media loader cannot reliably read Wails' custom
      // scheme. Fetch through the authorized asset route, then let it play a
      // browser-owned Blob; no base64 or ArrayBuffer copies are needed.
      void (async () => {
        const response = await fetch(fileURL(fileId), { signal: controller.signal, credentials: "same-origin", cache: "no-store" });
        if (!response.ok) throw new Error("Saved audio could not be loaded. Reopen the meeting to try again.");
        const recording = await response.blob();
        if (!recording.size) throw new Error("The saved audio file is empty.");
        if (controller.signal.aborted) return;
        objectURL = URL.createObjectURL(recording);
        setPlaybackSource({ fileId, url: objectURL });
        setPlaybackLoading(false);
      })().catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setPlaybackLoading(false);
        setPlaybackError(error instanceof Error ? error.message : "Saved audio could not be loaded.");
      });
    }
    return () => {
      controller.abort();
      player?.pause();
      player?.removeAttribute("src");
      player?.load();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [w.workspaceId, meeting?.recordingFileId]);
  const refresh = useCallback(async () => {
    const epoch = ++refreshEpoch.current;
    const [capture, list] = await Promise.all([
      bridge()?.MeetingRecordings() ?? Promise.resolve([]),
      dispatch<{ jobs: Job[] }>("jobs.list", { ...envelope(w), limit: 100 }),
    ]);
    if (epoch !== refreshEpoch.current || !currentScope.current.startsWith(`${w.workspaceId}:`)) return;
    setRecordings(capture); setJobs(list.jobs);
  }, [w.workspaceId]);
  useEffect(() => {
    let disposed = false;
    const poll = () => void refresh().catch(e => { if (!disposed) setError(String(e.message ?? e)); });
    poll(); const timer = window.setInterval(poll, 2000);
    const off = window.runtime?.EventsOn("recording:progress", poll);
    void dispatch<{ packs: { pack: Pack; state: string }[] }>("models.list").then(result => {
      if (disposed) return;
      const ready = result.packs.filter(p => p.state === "ready" || p.state === "installed").map(p => p.pack);
      setPacks(ready); setSpeech(ready.find(p => p.kind === "speech")?.id ?? ""); setChat(ready.find(p => p.id === "01990000-0000-7000-8000-000000000105")?.id ?? ready.find(p => p.kind === "chat" && !p.qualificationOnly)?.id ?? "");
    }).catch(e => { if (!disposed) setNotice(`Local models unavailable: ${e.message ?? e}`); });
    return () => { disposed = true; clearInterval(timer); off?.(); };
  }, [refresh]);
  const loadDrafts = useCallback(async () => {
    const epoch = ++draftEpoch.current;
    const requestedScope = `${w.workspaceId}:${meeting?.id ?? ""}`;
    if (!meeting) { setDrafts([]); setExtractions([]); setDraftScope(requestedScope); return; }
    const [result, extractionResult] = await Promise.all([
      dispatch<Draft[]>("meetings.drafts", { ...envelope(w), meetingId: meeting.id }),
      dispatch<Extraction[]>("meetings.extractions", { ...envelope(w), meetingId: meeting.id }),
    ]);
    if (epoch !== draftEpoch.current || currentScope.current !== requestedScope) return;
    setDrafts(result); setExtractions(extractionResult); setDraftScope(requestedScope);
  }, [meeting?.id, w.workspaceId, w.snapshotCursor]);
  useEffect(() => { let active = true; void loadDrafts().catch(e => { if (active) setError(e.message); }); return () => { active = false; draftEpoch.current++; }; }, [loadDrafts, jobs.filter(j => j.kind === "SUMMARIZE_MEETING").map(j => `${j.jobId}:${j.state}`).join(",")]);
  async function run(action: () => Promise<void>) {
    setBusy(true); setError(""); setNotice("");
    try { await action(); await onRefresh(); await refresh(); } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  }
  function navigate(ms: number, segmentId: string, blockId: string) {
    if (!meeting || !page) return;
    if (audio.current) { audio.current.currentTime = ms / 1000; void audio.current.play().catch(() => undefined); }
    onNavigate({ pageId: page.id, blockId, transcript: { meetingId: meeting.id, segmentId, startMs: ms, endMs: segments.find(s => s.id === segmentId)?.endMs ?? ms }, sourceRevision: page.revision });
  }
  async function convert(kind: "page" | "task", draftId?: string, itemIndex?: number) {
    if (!meeting) return;
    const extraction = await dispatch<Extraction>("meetings.prepareExtraction", { ...envelope(w), meetingId: meeting.id, kind, ...(draftId === undefined ? {} : { draftId, itemIndex }) });
    if (currentScope.current !== scope) return;
    if (extraction.appliedAt) { setNotice("This proposal was already applied. Opening the existing result."); onNavigate({ pageId: extraction.pageId, sourceRevision: w.pages.find(page => page.id === extraction.pageId)?.revision ?? "1" }); return; }
    setReview(extraction); await loadDrafts();
  }
  const activeJobs = jobs.filter(j => j.payload.meetingId === meeting?.id);
  return <section className={`meeting-panel ${pageId ? "meeting-inline" : ""}`} aria-label="Meeting recordings and transcripts">
    {!pageId && <><span className="eyebrow">CONVERSATIONS, WITH CONTEXT</span><h1>Meetings</h1><p className="lede">Record locally, find what was said, and review what comes next.</p>
      <div className="meeting-capture"><label>Meeting title<input value={title} onChange={e => setTitle(e.target.value)} placeholder="Team catch-up" maxLength={512}/></label>
        <label>Audio input<select aria-label="Audio input" disabled={busy || !!live} value={inputDeviceId} onChange={e => setInputDeviceId(e.target.value)}><option value="">System default microphone</option>{inputs.map(input => <option key={input.id} value={input.id}>{input.name}{input.isDefault ? " (default)" : ""}</option>)}</select></label>
        <button disabled={busy || !!live} onClick={() => void run(async () => { const result = await bridge()?.MeetingAudioInputs(); if (!result) throw new Error("Open the desktop app to choose an audio input"); setInputs(result); })}>Refresh audio inputs</button>
        <label><input type="checkbox" checked={disclosed} disabled={!!live} onChange={e => setDisclosed(e.target.checked)}/>I have told participants that this conversation will be recorded.</label>
        <div className="meeting-actions"><button className="primary" disabled={busy || !!live || !disclosed} onClick={() => void run(async () => { const result = await bridge()?.StartMeetingRecordingWithOptions(title, inputDeviceId, disclosed); if (!result) throw new Error("Open the desktop app to record"); setNotice("Recording locally. Audio is saved as you speak."); setDisclosed(false); })}>● Record microphone</button>
          <button disabled={busy || !!live} onClick={() => void run(async () => { const result = await bridge()?.PickMeetingAudio(title); if (result?.meetingId) { setSelected(result.meetingId); setNotice("Audio saved. Choose a speech pack to transcribe it."); } })}>Upload local audio</button></div>
        <small>On macOS, transcription supports mono/stereo WAV, MP3, AAC and M4A up to two hours, plus 16 kHz mono raw float32 PCM. Decoding uses a temporary local copy; your original recording is retained. Unsupported or damaged files can be exported and retried.</small>
      </div>
      {recordings.map(r => <div className="recording-recovery" key={r.id}><strong>{r.title}</strong><span>{r.state} · {timestamp(r.durationMs)} · {Math.ceil(r.bytes / 1024)} KB saved (captured audio)</span>{r.gaps?.map(gap => <small key={gap.id}>{gap.kind} at {timestamp(gap.sampleOffset / 16)} · {gap.durationMs === undefined ? "gap duration unknown or still open" : `${timestamp(gap.durationMs)} gap`}</small>)}{r.error && <small>{r.error}</small>}
        {(r.state === "recording" || r.state === "paused") && <button disabled={busy} onClick={() => void run(async () => { await bridge()?.ControlMeetingRecording(r.id, r.state === "paused" ? "resume" : "pause"); })}>{r.state === "paused" ? "Resume recording" : "Pause recording"}</button>}
        <button disabled={busy} onClick={() => void run(async () => { const result = (r.state === "recording" || r.state === "paused") ? await bridge()?.StopMeetingRecording(r.id) : await bridge()?.SaveMeetingRecording(r.id); if (result) setSelected(result.meetingId); })}>{(r.state === "recording" || r.state === "paused") ? "Stop and save meeting" : "Recover saved audio"}</button></div>)}
      {meetings.length > 0 && <div className="meeting-picker">{meetings.map(m => <button className={m.id === meeting?.id ? "selected" : ""} key={m.id} onClick={() => setSelected(m.id)}>{w.pages.find(p => p.id === m.pageId)?.title ?? "Meeting"}<small>{m.state} · {timestamp(m.durationMs)}</small></button>)}</div>}
    </>}
    {error && <p className="meeting-error" role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
    {meeting && page && <div className="meeting-detail">
      {!pageId && <h2>{page.title}</h2>}
      {savedCapture?.meetingId === meeting.id && <div aria-label="Capture history"><p>{timestamp(savedCapture.durationMs)} captured · {savedCapture.inputDeviceId ? "Selected microphone" : "System default microphone"}{savedCapture.disclosureAcknowledgedAt ? " · Participant disclosure acknowledged" : ""}</p>{savedCapture.gaps?.map(gap => <p key={gap.id}>{gap.kind} at {timestamp(gap.sampleOffset / 16)} · {gap.durationMs === undefined ? "duration unknown" : `${timestamp(gap.durationMs)} gap`}{gap.reason ? ` · ${gap.reason}` : ""}</p>)}</div>}
      <MeetingRetentionControls key={`retention:${scope}`} workspace={w} meetingId={meeting.id} onCommitted={onRefresh}/>

      {meeting.recordingFileId && <audio key={`${w.workspaceId}:${meeting.recordingFileId}`} ref={audio} controls preload="metadata" data-meeting-id={meeting.id} src={playbackSource?.fileId === meeting.recordingFileId ? playbackSource.url : undefined} aria-busy={playbackLoading} onError={() => setPlaybackError("Saved audio could not be played. The original recording remains available for transcription and backup.")} aria-label={`Recording of ${page.title}`}/>}
      {playbackLoading && <p role="status">Loading saved audio…</p>}
      {playbackError && <p role="alert">{playbackError}</p>}
      <div className="meeting-actions"><label>Speech pack<select value={speech} onChange={e => setSpeech(e.target.value)}><option value="">Choose an installed pack</option>{packs.filter(p => p.kind === "speech").map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
        <button disabled={busy || !speech || meeting.state === "ready" || activeJobs.some(j => j.kind === "TRANSCRIBE_AUDIO" && ["queued", "running", "retry_wait"].includes(j.state))} onClick={() => void run(async () => { await dispatch("meetings.transcribe", { ...envelope(w), meetingId: meeting.id, modelPackId: speech }); })}>{segments.length ? "Resume transcription" : "Transcribe recording"}</button>
        <button disabled={busy || !segments.length} onClick={() => void run(() => convert("page"))}>Review as page</button>
        <button disabled={busy || !segments.length} onClick={() => void run(() => convert("task"))}>Review as task</button>
      </div>
      {!speech && <p className="meeting-help">Install a speech pack in Settings to transcribe. Saved audio and notes remain available.</p>}
      {activeJobs.slice(0, 5).map(job => <div className="meeting-job" key={job.jobId}><span>{job.kind === "TRANSCRIBE_AUDIO" ? "Transcription" : "Meeting notes"}: {job.state}</span>{job.progress?.total ? <progress max={job.progress.total} value={job.progress.completed} aria-label="Processing progress"/> : null}<small>{job.error?.message ?? job.progress?.message}</small>{["queued", "running", "retry_wait"].includes(job.state) && <button onClick={() => void run(async () => { await dispatch("cancelJob", { ...envelope(w), jobId: job.jobId, expectedRevision: job.revision }); })}>Cancel processing</button>}</div>)}
      <div className="meeting-transcript"><h3>Transcript</h3>{segments.length ? segments.map(segment => <div className="transcript-segment" key={segment.id}><button onClick={() => navigate(segment.startMs, segment.id, segment.blockId)} title="Play from this timestamp">{timestamp(segment.startMs)}</button><p>{segment.text}</p><TranscriptCorrection key={`${scope}:${segment.id}`} workspace={w} meeting={meeting} segment={segment} onCommitted={onRefresh}/></div>) : <p>Timestamped text appears here as each audio segment finishes. You can continue editing while it runs.</p>}</div>
      <div className="meeting-actions"><label>Summary model<select value={chat} onChange={e => setChat(e.target.value)}><option value="">Choose an instruction model</option>{packs.filter(p => p.kind === "chat" && !p.qualificationOnly).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label><button disabled={busy || !chat || !segments.length || meeting.state !== "ready" || activeJobs.some(j => j.kind === "SUMMARIZE_MEETING" && ["running", "queued", "retry_wait"].includes(j.state))} onClick={() => void run(async () => { await dispatch("enqueueJob", { ...envelope(w), kind: "SUMMARIZE_MEETING", idempotencyKey: requestID(), payload: { kind: "summarize", meetingId: meeting.id, sourceRevision: page.revision, modelPackId: chat } }); })}>Generate meeting notes for review</button></div>
      {visibleExtractions.length > 0 && <section aria-label="Saved extraction reviews"><h3>Extracted pages and tasks</h3>{visibleExtractions.map(item => <div key={`${item.proposalId}:${item.itemId}`}><span>{item.title} · {item.appliedAt ? (item.stale ? "Applied · source changed; review evidence" : "Applied") : item.stale ? "Source changed" : "Awaiting review"}</span>{item.appliedAt ? <button onClick={() => onNavigate({ pageId: item.pageId, sourceRevision: w.pages.find(page => page.id === item.pageId)?.revision ?? "1" })}>Open existing {item.kind}</button> : <button disabled={busy || item.stale} onClick={() => setReview(item)}>Resume review</button>}</div>)}</section>}
      {visibleDrafts.map(draft => <div className="meeting-draft" key={draft.id}><h3>{draft.state === "applied" ? "Approved meeting notes" : "Review proposed meeting notes"}</h3>{draft.stale && <p role="alert">Transcript evidence changed. Generate a fresh draft before approval.</p>}
        {([ ["Summary", "summary"], ["Key points", "keyPoints"], ["Decisions", "decisions"], ["Questions", "questions"], ["Proposed action items", "actionItems"] ] as const).map(([name, key]) => draft.body[key]?.length > 0 && <div key={key}><h4>{name}</h4>{draft.body[key].map((claim, index) => <p key={index}>{claim.text}{claim.segmentIds.map(id => { const s = segments.find(s => s.id === id); return s && <button className="meeting-citation" key={id} onClick={() => navigate(s.startMs, s.id, s.blockId)}>[{timestamp(s.startMs)}]</button>; })}{key === "actionItems" && !draft.stale && <button disabled={busy} onClick={() => void run(() => convert("task", draft.id, index))}>Review task</button>}</p>)}</div>)}
        {draft.state === "draft" && <button className="primary" disabled={busy || draft.stale} onClick={() => void run(async () => { await dispatch("meetings.applyDraft", { ...envelope(w), draftId: draft.id }); await loadDrafts(); })}>Approve and add notes to page</button>}
      </div>)}
    </div>}
    {review && <div className="meeting-review" role="dialog" aria-label={`Review ${review.kind} from voice note`}><h3>Review new {review.kind}</h3><label>Title<input value={review.title} maxLength={512} onChange={e => setReview({ ...review, title: e.target.value })}/></label><label>Content<textarea value={review.body} onChange={e => setReview({ ...review, body: e.target.value })}/></label><p>This creates a canonical {review.kind} with a link to the source meeting.</p><button disabled={busy || !review.title.trim()} className="primary" onClick={() => void run(async () => { const result = await dispatch<{ pageId: string }>("meetings.createFromTranscript", { ...envelope(w), meetingId: review.meetingId, evidenceHash: review.evidenceHash, proposalId: review.proposalId, itemId: review.itemId, kind: review.kind, title: review.title, body: review.body }); setReview(null); await loadDrafts(); onNavigate({ pageId: result.pageId, sourceRevision: "1" }); })}>Approve and create {review.kind}</button><button onClick={() => setReview(null)}>Cancel</button></div>}
    {!pageId && !meetings.length && !recordings.length && <div className="empty-state"><h3>Keep your next conversation</h3><p>Start a microphone recording or choose a local audio file.</p></div>}
  </section>;
}
