import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type { Location } from "../../../contracts/generated/domain";

export type ReviewGeneration = {
  modelPackId: string;
  categories: string[];
  allowedActions: string[];
  actionPageIds: string[];
};
export const reviewCategories = [
  ["summary", "Summary"],
  ["risks", "Risks"],
  ["decisions", "Decisions"],
  ["next_steps", "Suggested next steps"],
];
export const reviewActions = [
  ["append_review", "Append the review to its plan page"],
  ["rename_page", "Rename allowed pages"],
  ["append_page", "Append text to allowed pages"],
  ["create_task", "Create tasks in allowed Tasks pages"],
  ["set_task_status", "Update allowed task status"],
  ["set_task_due", "Update allowed task due date"],
];
export const newReviewGeneration = (): ReviewGeneration => ({
  modelPackId: "",
  categories: ["summary", "risks"],
  allowedActions: ["append_review"],
  actionPageIds: [],
});
export function ReviewGenerationOptions({
  workspace,
  value,
  onChange,
  consent,
  onConsent,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  value: ReviewGeneration;
  onChange: (v: ReviewGeneration) => void;
  consent: boolean;
  onConsent: (v: boolean) => void;
  onError: (e: unknown) => void;
}) {
  const [models, setModels] = useState<{ id: string; name: string }[]>([]);
  useEffect(() => {
    let alive = true;
    dispatch<{
      packs: {
        pack: {
          id: string;
          name: string;
          kind: string;
          qualificationOnly?: boolean;
        };
        state: string;
      }[];
    }>("models.list")
      .then((r) => {
        if (!alive) return;
        setModels(
          r.packs
            .filter(
              (p) =>
                p.state === "ready" &&
                p.pack.kind === "chat" &&
                !p.pack.qualificationOnly,
            )
            .map((p) => p.pack),
        );
      })
      .catch(onError);
    return () => {
      alive = false;
    };
  }, []);
  const choices = (
    key: "categories" | "allowedActions",
    options: string[][],
  ) => (
    <fieldset>
      <legend>
        {key === "categories"
          ? "Review categories"
          : "Changes that may be proposed"}
      </legend>
      {options.map(([id, label]) => (
        <label key={id} style={{ display: "block" }}>
          <input
            type="checkbox"
            checked={value[key].includes(id)}
            onChange={(e) => {
              onConsent(false);
              onChange({
                ...value,
                [key]: e.target.checked
                  ? [...value[key], id]
                  : value[key].filter((x) => x !== id),
              });
            }}
          />
          {label}
        </label>
      ))}
    </fieldset>
  );
  return (
    <fieldset aria-label="Local model review configuration">
      <legend>Local model analysis</legend>
      <p>
        Scheduled analysis reads selected evidence locally. Every proposed page
        or task change needs a separate approval. Quotes show the source basis;
        check whether the model’s interpretation follows from them.
      </p>
      <label>
        Review model
        <select
          value={value.modelPackId}
          onChange={(e) => {
            onConsent(false);
            onChange({ ...value, modelPackId: e.target.value });
          }}
        >
          <option value="">Choose an installed instruction model</option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
      </label>
      {models.length === 0 && (
        <p>Install and verify a chat model in Models first.</p>
      )}
      {choices("categories", reviewCategories)}
      {choices("allowedActions", reviewActions)}
      <label>
        Allowed action destinations
        <select
          multiple
          value={value.actionPageIds}
          onChange={(e) => {
            onConsent(false);
            onChange({
              ...value,
              actionPageIds: Array.from(
                e.target.selectedOptions,
                (o) => o.value,
              ).slice(0, 32),
            });
          }}
        >
          {workspace.pages
            .filter((p) => !p.deletedAt)
            .map((p) => (
              <option key={p.id} value={p.id}>
                {p.title || "Untitled"}
              </option>
            ))}
        </select>
      </label>
      <p>
        Select existing pages or task pages; task creation requires the
        canonical Tasks page. Matching uses unique exact titles. Maximum 32
        destinations. Clearing this list permits only the optional review
        append.
      </p>
      <label>
        <input
          type="checkbox"
          checked={consent}
          onChange={(e) => onConsent(e.target.checked)}
        />
        I allow this selected local model to analyze this review’s selected
        pages and optional approved memories on this schedule.
      </label>
    </fieldset>
  );
}
export type GeneratedReviewRun = {
  id: string;
  pageId: string;
  planBlockId: string;
  status: string;
  text: string;
  sources: Location[];
  createdAt: string;
  generative: ReviewGeneration;
  warning?: string;
  error?: string;
  attempt?: number;
  actions?: unknown[];
  citations?: { number: number; title: string; location: Location }[];
};
type ReviewProposal = {
  id: string;
  changes: {
    label: string;
    destination: string;
    before?: string;
    after: string;
  }[];
};
export function GeneratedReviewCard({
  workspace,
  review,
  onReload,
  onRefresh,
  onNavigate,
  onDismiss,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  review: GeneratedReviewRun;
  onReload: () => Promise<void>;
  onRefresh: () => Promise<void>;
  onNavigate: (id: string) => void;
  onDismiss: () => Promise<void>;
  onError: (e: unknown) => void;
}) {
  const [busy, setBusy] = useState(false),
    [proposal, setProposal] = useState<ReviewProposal | null>(null);
  const perform = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
      await onReload();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };
  const request = (method: string) =>
    dispatch(method, { ...envelope(workspace), runId: review.id });
  const active = ["queued", "generating"].includes(review.status),
    ready = review.status === "ready";
  return (
    <article aria-label="Generated personal review">
      <h3>
        {review.status} · {new Date(review.createdAt).toLocaleString()}
      </h3>
      {review.warning && <p role="status">{review.warning}</p>}
      {review.error && <p role="alert">{review.error}</p>}
      {active ? (
        <p>
          Preparing read-only local analysis. Canonical pages and tasks have not
          changed.
        </p>
      ) : (
        <pre style={{ whiteSpace: "pre-wrap" }}>{review.text}</pre>
      )}
      <ul>
        {(
          review.citations ??
          review.sources.map((location, i) => ({
            number: i + 1,
            title: "Source",
            location,
          }))
        ).map((c, i) => (
          <li key={i}>
            <button onClick={() => onNavigate(c.location.pageId)}>
              [{c.number}] {c.title} · revision {c.location.sourceRevision}
            </button>
          </li>
        ))}
      </ul>
      {active && (
        <button
          disabled={busy}
          onClick={() =>
            void perform(async () => {
              await request("review.cancel");
            })
          }
        >
          Cancel analysis
        </button>
      )}
      {["failed", "cancelled"].includes(review.status) && (
        <button
          disabled={busy || (review.attempt ?? 0) >= 3}
          onClick={() =>
            void perform(async () => {
              setProposal(null);
              await request("review.retry");
            })
          }
        >
          Retry with current captured evidence
        </button>
      )}
      {ready && (
        <>
          <p>This is a draft. No changes have been applied.</p>
          {review.generative.allowedActions.includes("append_review") ||
          Boolean(review.actions?.length) ? (
            <button
              disabled={busy}
              onClick={() =>
                void perform(async () => {
                  setProposal(
                    await dispatch<ReviewProposal>("review.propose", {
                      ...envelope(workspace),
                      runId: review.id,
                    }),
                  );
                })
              }
            >
              Review proposed changes
            </button>
          ) : (
            <p>No changes were proposed or configured.</p>
          )}
          {proposal && (
            <section aria-label="Personal review proposed changes">
              <h4>Confirm these exact changes</h4>
              {proposal.changes.map((change, i) => (
                <article key={i}>
                  <strong>
                    {change.label} · {change.destination}
                  </strong>
                  {change.before && (
                    <>
                      <p>Before</p>
                      <pre style={{ whiteSpace: "pre-wrap" }}>
                        {change.before}
                      </pre>
                    </>
                  )}
                  <p>After</p>
                  <pre style={{ whiteSpace: "pre-wrap" }}>{change.after}</pre>
                </article>
              ))}
              <button
                disabled={busy}
                onClick={() =>
                  void perform(async () => {
                    await request("review.apply");
                    setProposal(null);
                    await onRefresh();
                  })
                }
              >
                Approve these changes
              </button>
              <button disabled={busy} onClick={() => setProposal(null)}>
                Keep draft without applying
              </button>
            </section>
          )}
        </>
      )}
      {!active && !["accepted", "dismissed"].includes(review.status) && (
        <button disabled={busy} onClick={() => void perform(onDismiss)}>
          Dismiss generated review
        </button>
      )}
    </article>
  );
}
