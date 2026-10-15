import { useEffect, useRef, useState } from "react";
import { batch, dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type { MeetingRetentionPreview } from "../../../contracts/generated/service";

export function MeetingRetentionControls({
  workspace: w,
  meetingId,
  onCommitted,
}: {
  workspace: WorkspaceSnapshot;
  meetingId: string;
  onCommitted: () => Promise<unknown> | void;
}) {
  const [preview, setPreview] = useState<MeetingRetentionPreview | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [reviewed, setReviewed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const epoch = useRef(0);
  useEffect(
    () => () => {
      epoch.current++;
    },
    [],
  );
  async function load(choices: string[], forReview: boolean) {
    const request = ++epoch.current;
    setBusy(true);
    setError("");
    try {
      const result = await dispatch<MeetingRetentionPreview>(
        "meetings.retentionPreview",
        { ...envelope(w), meetingId, selected: choices },
      );
      if (request !== epoch.current) return;
      setPreview(result);
      setReviewed(forReview);
    } catch (e) {
      if (request === epoch.current)
        setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (request === epoch.current) setBusy(false);
    }
  }
  async function apply() {
    if (!preview?.canApply || !reviewed) return;
    setBusy(true);
    setError("");
    try {
      await batch(w, [preview.command], preview.expectedRevisions);
      setPreview(null);
      setSelected([]);
      setReviewed(false);
      await onCommitted();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setReviewed(false);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section aria-label="Meeting retention">
      <button disabled={busy} onClick={() => void load([], false)}>
        Review meeting retention
      </button>
      {error && <p role="alert">{error}</p>}
      {preview && (
        <div
          className="meeting-review"
          role="dialog"
          aria-label="Review meeting retention choices"
        >
          <h3>Meeting retention</h3>
          {preview.categories.map((category) => (
            <div key={category.id}>
              <label>
                <input
                  type="checkbox"
                  checked={selected.includes(category.id)}
                  disabled={busy || !category.selectable}
                  onChange={(e) => {
                    setSelected(
                      e.target.checked
                        ? [...selected, category.id]
                        : selected.filter((id) => id !== category.id),
                    );
                    setReviewed(false);
                  }}
                />
                {category.action} · {category.count}
              </label>
              <p>{category.detail}</p>
            </div>
          ))}
          {preview.warnings.map((warning) => (
            <p key={warning}>{warning}</p>
          ))}
          {reviewed && (
            <p role="status">
              Reviewed choices: {preview.selected.join(", ")}. The service
              rechecks these exact effects before applying them.
            </p>
          )}
          <button
            disabled={busy || !selected.length}
            onClick={() => void load(selected, true)}
          >
            Preview selected retention
          </button>
          <button
            disabled={busy || !reviewed || !preview.canApply}
            onClick={() => void apply()}
          >
            Apply reviewed retention
          </button>
          <button
            disabled={busy}
            onClick={() => {
              epoch.current++;
              setPreview(null);
              setSelected([]);
              setReviewed(false);
            }}
          >
            Cancel
          </button>
        </div>
      )}
    </section>
  );
}
