import { useEffect, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  pageGuards,
  type D,
  type S,
  type WorkspaceSnapshot,
} from "./api";

export function freshnessLabel(f: D.PageFreshness): string {
  return {
    never_reviewed: "Never reviewed",
    reviewed: "Human reviewed",
    stale: "Changed since review",
    expired: "Review overdue",
  }[f.state];
}
function useReviewClock() {
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const timer = setInterval(() => setTick((t) => t + 1), 30000);
    return () => clearInterval(timer);
  }, []);
  return tick;
}
export function PageFreshness({
  workspace,
  page,
  onChange,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  page: D.Page;
  onChange: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const tick = useReviewClock();
  const [result, setResult] = useState<S.PageFreshnessResult>();
  const [expiry, setExpiry] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    setResult(undefined);
    dispatch<S.PageFreshnessResult>("page.freshness", {
      ...envelope(workspace),
      pageId: page.id,
    })
      .then((r) => {
        if (!r.freshness) throw Error("Review status is unavailable");
        if (active) {
          setResult(r);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(String(e));
      });
    return () => {
      active = false;
    };
  }, [workspace.workspaceId, page.id, page.revision, tick]);
  async function review() {
    if (!result) return;
    setBusy(true);
    try {
      await batch(
        workspace,
        [
          {
            kind: "page.review",
            pageId: page.id,
            contentIdentity: result.freshness.contentIdentity,
            ...(expiry ? { expiresAt: new Date(expiry).toISOString() } : {}),
          },
        ],
        pageGuards(workspace, page),
      );
      await onChange();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <details className="page-review">
      <summary>
        {result ? freshnessLabel(result.freshness) : "Review status"}
      </summary>
      {error && <p role="alert">{error}</p>}
      <p>
        A human review covers this page’s content and record values. It does not
        verify that every statement is true.
      </p>
      {result?.freshness.review && (
        <p>
          Last reviewed{" "}
          {new Date(result.freshness.review.reviewedAt).toLocaleString()}
          {result.freshness.review.expiresAt && (
            <>
              {" "}
              · Due{" "}
              {new Date(result.freshness.review.expiresAt).toLocaleString()}
            </>
          )}
        </p>
      )}
      <label>
        Next review (optional, local time){" "}
        <input
          type="datetime-local"
          value={expiry}
          onChange={(e) => setExpiry(e.target.value)}
        />
      </label>
      <button disabled={busy || !result} onClick={() => void review()}>
        {busy ? "Recording review…" : "I reviewed the current content"}
      </button>
      {!!result?.history.length && (
        <details>
          <summary>Review history</summary>
          <ul>
            {result.history.map((r) => (
              <li key={r.id}>
                {new Date(r.reviewedAt).toLocaleString()} · Reviewer {r.actorId}
                {r.expiresAt && (
                  <> · Due {new Date(r.expiresAt).toLocaleString()}</>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
    </details>
  );
}
export function ReviewQueue({
  workspace,
  onNavigate,
}: {
  workspace: WorkspaceSnapshot;
  onNavigate: (pageId: string) => unknown;
}) {
  const tick = useReviewClock();
  const [items, setItems] = useState<S.ReviewQueueResult["items"]>([]);
  const [cursor, setCursor] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    setBusy(true);
    dispatch<S.ReviewQueueResult>("page.reviewQueue", {
      ...envelope(workspace),
      limit: 30,
    })
      .then((r) => {
        if (active) {
          setItems(r.items ?? []);
          setCursor(r.nextPageId);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(String(e));
      })
      .finally(() => {
        if (active) setBusy(false);
      });
    return () => {
      active = false;
    };
  }, [workspace.workspaceId, workspace.snapshotCursor, tick]);
  async function more() {
    setBusy(true);
    try {
      const r = await dispatch<S.ReviewQueueResult>("page.reviewQueue", {
        ...envelope(workspace),
        limit: 30,
        afterPageId: cursor,
      });
      setItems((previous) => [...previous, ...r.items]);
      setCursor(r.nextPageId);
      setError("");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section aria-label="Knowledge review queue">
      <h2>Knowledge review queue</h2>
      <p>Pages that have changed, are overdue or have never been reviewed.</p>
      {error && <p role="alert">{error}</p>}
      <ul>
        {items.map((i) => (
          <li key={i.pageId}>
            <button onClick={() => onNavigate(i.pageId)}>
              {i.title || "Untitled"}
            </button>{" "}
            · {freshnessLabel(i.freshness)}
          </li>
        ))}
      </ul>
      {!busy && !items.length && (
        <p>No pages need review in this part of the queue.</p>
      )}
      {busy && <p role="status">Loading review queue…</p>}
      {cursor && (
        <button disabled={busy} onClick={() => void more()}>
          Load more
        </button>
      )}
    </section>
  );
}
