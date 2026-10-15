import { useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  pageGuards,
  type D,
  type S,
  type WorkspaceSnapshot,
} from "../api";

export function TranscriptCorrection({
  workspace: w,
  meeting,
  segment,
  onCommitted,
}: {
  workspace: WorkspaceSnapshot;
  meeting: D.Meeting;
  segment: D.TranscriptSegment;
  onCommitted: () => Promise<unknown> | void;
}) {
  const [edit, setEdit] = useState<{
    text: string;
    startMs: number;
    endMs: number;
    guards: S.ExpectedRevision[];
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function begin() {
    setBusy(true);
    setError("");
    try {
      const result = await dispatch<S.ReadPageResult>("readPage", {
        ...envelope(w),
        pageId: meeting.pageId,
      });
      const page = result.page,
        block = result.blocks.find((block) => block.id === segment.blockId);
      if (!page || !block) {
        setError(
          "Open the meeting page to load the current transcript before correcting it.",
        );
        return;
      }
      setError("");
      setEdit({
        text: segment.text,
        startMs: segment.startMs,
        endMs: segment.endMs,
        guards: [
          ...pageGuards(w, page),
          {
            target: { entityType: "meeting", entityId: meeting.id },
            revision: meeting.revision,
          },
          {
            target: { entityType: "block", entityId: block.id },
            revision: block.revision,
          },
          {
            target: { entityType: "transcript_segment", entityId: segment.id },
            revision: segment.revision,
          },
        ],
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (!edit) return;
    setBusy(true);
    setError("");
    try {
      await batch(
        w,
        [
          {
            kind: "meeting.correctSegment",
            meetingId: meeting.id,
            segmentId: segment.id,
            text: edit.text,
            startMs: edit.startMs,
            endMs: edit.endMs,
          },
        ],
        edit.guards,
      );
      setEdit(null);
      await onCommitted();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div>
      {!edit && (
        <button disabled={busy} onClick={begin}>
          Correct transcript segment
        </button>
      )}
      {error && <p role="alert">{error}</p>}
      {edit && (
        <div role="group" aria-label="Transcript correction">
          <label>
            Corrected text
            <textarea
              value={edit.text}
              maxLength={16000}
              disabled={busy}
              onChange={(e) => setEdit({ ...edit, text: e.target.value })}
            />
          </label>
          <label>
            Start in milliseconds
            <input
              type="number"
              min={0}
              max={meeting.durationMs}
              step={1}
              value={edit.startMs}
              disabled={busy}
              onChange={(e) =>
                setEdit({ ...edit, startMs: e.target.valueAsNumber })
              }
            />
          </label>
          <label>
            End in milliseconds
            <input
              type="number"
              min={edit.startMs}
              max={meeting.durationMs}
              step={1}
              value={edit.endMs}
              disabled={busy}
              onChange={(e) =>
                setEdit({ ...edit, endMs: e.target.valueAsNumber })
              }
            />
          </label>
          <p>
            Earlier text and timing remain in history. Related summaries,
            proposals and memory will require current evidence. Approved tasks
            keep their edits.
          </p>
          <button
            disabled={
              busy ||
              !edit.text.trim() ||
              !Number.isSafeInteger(edit.startMs) ||
              !Number.isSafeInteger(edit.endMs) ||
              edit.endMs < edit.startMs
            }
            onClick={() => void save()}
          >
            Save transcript correction
          </button>
          <button disabled={busy} onClick={() => setEdit(null)}>
            Cancel correction
          </button>
        </div>
      )}
    </div>
  );
}
