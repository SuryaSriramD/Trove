import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type S, type WorkspaceSnapshot } from "../api";

export function TaskDetachPanel({
  workspace: w,
  recordId,
  title,
  onCommitted,
  onError,
  disabled = false,
}: {
  workspace: WorkspaceSnapshot;
  recordId: string;
  title: string;
  onCommitted: () => Promise<unknown> | void;
  onError: (error: unknown) => void;
  disabled?: boolean;
}) {
  const [plan, setPlan] = useState<S.TaskDetachPlan>();
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState(false);
  const [approved, setApproved] = useState(false);
  const generation = useRef(0);
  const identity = `${w.workspaceId}:${w.scopeGrantId}:${recordId}:${w.snapshotCursor}`;
  const current = useRef(identity);
  current.current = identity;
  useEffect(() => {
    generation.current++;
    setPlan(undefined);
    setBusy(false);
    setApplying(false);
    setApproved(false);
  }, [identity]);
  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );
  function cancel() {
    generation.current++;
    setPlan(undefined);
    setBusy(false);
    setApproved(false);
  }
  async function preview(choices: S.TaskDetachChoice[] = []) {
    const n = ++generation.current,
      key = identity;
    setBusy(true);
    setPlan(undefined);
    setApproved(false);
    try {
      const next = await dispatch<S.TaskDetachPlan>("tasks.detachPreview", {
        ...envelope(w),
        recordId,
        choices,
      });
      if (n === generation.current && key === current.current) setPlan(next);
    } catch (e) {
      if (n === generation.current && key === current.current) onError(e);
    } finally {
      if (n === generation.current && key === current.current) setBusy(false);
    }
  }
  async function apply() {
    if (!plan || !approved || busy || disabled) return;
    const key = identity,
      n = ++generation.current,
      reviewed = plan;
    setBusy(true);
    setApplying(true);
    setApproved(false);
    try {
      await dispatch<S.BatchResult>("batch", {
        ...envelope(w),
        mutation: {
          scopeGrantId: w.scopeGrantId,
          idempotencyKey: `detach-${reviewed.id}`,
          expectedRevisions: reviewed.requiredRevisions,
        },
        commands: [{ kind: "task.detach.apply", plan: reviewed }],
      });
      if (n === generation.current && key === current.current) {
        setPlan(undefined);
        await onCommitted();
      }
    } catch (e) {
      if (n === generation.current && key === current.current) {
        setPlan(undefined);
        onError(e);
      }
    } finally {
      if (n === generation.current && key === current.current) {
        setBusy(false);
        setApplying(false);
      }
    }
  }
  return (
    <div className="task-detach">
      <button
        type="button"
        disabled={disabled || busy}
        onClick={() => void preview()}
        aria-label={`Detach task facet for ${title}`}
      >
        Detach task
      </button>
      {busy && !applying && (
        <span role="status">
          Preparing complete occurrence review…{" "}
          <button type="button" onClick={cancel}>
            Cancel detachment review
          </button>
        </span>
      )}
      {plan && (
        <section role="dialog" aria-label={`Detach task facet: ${title}`}>
          <p>
            The record, page, title, status and due values remain. The record
            leaves Tasks and stops behaving as a task. Page reminders remain
            configured. Undo is available in Operations.
          </p>
          {plan.reminders.length > 0 && (
            <ul aria-label="Reminder effects">
              {plan.reminders.map((r) => (
                <li key={r.id}>
                  {r.description}:{" "}
                  {!r.eligibleBefore && r.eligibleAfter
                    ? "will become eligible again because completed-task suppression is removed"
                    : r.eligibleAfter
                      ? "remains eligible"
                      : "remains disabled"}
                  .
                </li>
              ))}
            </ul>
          )}
          {plan.occurrences.length === 0 ? (
            <p>No retained todo occurrences reference this task.</p>
          ) : (
            <>
              <p>
                Review every occurrence, including those on trashed pages.
                Conversion keeps nested and attached content and captures the
                current readable task state.
              </p>
              <ul>
                {plan.occurrences.map((o) => (
                  <li key={o.blockId}>
                    <strong>
                      {o.pageTitle}
                      {o.trashed ? " (in Trash)" : ""}
                    </strong>
                    <p>{o.text}</p>
                    <label>
                      Occurrence action{" "}
                      <select
                        aria-label={`Occurrence ${o.blockId} action`}
                        value={o.action}
                        disabled={busy}
                        onChange={(e) =>
                          void preview(
                            plan.occurrences.map((x) => ({
                              blockId: x.blockId,
                              action:
                                x.blockId === o.blockId
                                  ? (e.target.value as "convert" | "remove")
                                  : x.action,
                            })),
                          )
                        }
                      >
                        <option value="convert">
                          Convert to linked paragraph
                        </option>
                        <option value="remove" disabled={!o.canRemove}>
                          Remove occurrence
                        </option>
                      </select>
                    </label>
                    {!o.canRemove && <p>{o.removalReason}</p>}
                  </li>
                ))}
              </ul>
            </>
          )}
          <label>
            <input
              type="checkbox"
              checked={approved}
              disabled={busy}
              onChange={(e) => setApproved(e.target.checked)}
            />
            I reviewed all occurrence changes and want to detach this task
            facet.
          </label>
          <button
            type="button"
            disabled={busy || !approved || disabled}
            onClick={() => void apply()}
          >
            Apply reviewed detachment
          </button>
          <button type="button" disabled={applying} onClick={cancel}>
            Cancel detachment
          </button>
        </section>
      )}
    </div>
  );
}
