import { useEffect, useState } from "react";
import "./automation.css";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  type WorkspaceSnapshot,
  type S,
} from "../api";
import {
  RuleBuilder,
  actionSummary,
  type Rule,
  type AutomationOptions,
} from "./RuleBuilder";
type Run = {
  id: string;
  ruleId: string;
  ruleVersion: number;
  occurrenceKey: string;
  status: string;
  diagnostic: string;
  at: string;
};
type History = {
  rules: { rule: Rule; version: number; nextAt: string; failures: number }[];
  runs: Run[];
};
type Preview = {
  eligible: boolean;
  diagnostic: string;
  commands: S.Command[];
  expectedRevisions: S.ExpectedRevision[];
  occurrenceKey: string;
  at: string;
};
export default function AutomationPanel({
  workspace: w,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  onCommitted: () => Promise<void>;
  onError: (e: unknown) => void;
}) {
  const [pageId, setPageId] = useState("");
  const [history, setHistory] = useState<History>({ rules: [], runs: [] });
  const [editing, setEditing] = useState<Rule>();
  const [formVersion, setFormVersion] = useState(0);
  const [choices, setChoices] = useState<AutomationOptions>({
    templates: [],
    templatesTruncated: false,
    events: [],
  });
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<{
    rule: Rule;
    value: Preview;
    eventId?: string;
  }>();
  const [eventId, setEventId] = useState("");
  const page = w.pages.find(
    (p) => p.id === (pageId || w.pages.find((p) => !p.deletedAt)?.id),
  );
  const refresh = async () => {
    if (!page) return;
    const [next, options] = await Promise.all([
      dispatch<History>("automationHistory", {
        ...envelope(w),
        pageId: page.id,
      }),
      dispatch<AutomationOptions>("automationOptions", {
        ...envelope(w),
        pageId: page.id,
      }),
    ]);
    setHistory(next);
    setChoices(options);
  };
  useEffect(() => {
    let live = true;
    setPreview(undefined);
    const read = async () => {
      if (!page) return;
      const [next, options] = await Promise.all([
        dispatch<History>("automationHistory", {
          ...envelope(w),
          pageId: page.id,
        }),
        dispatch<AutomationOptions>("automationOptions", {
          ...envelope(w),
          pageId: page.id,
        }),
      ]);
      if (live) {
        setHistory(next);
        setChoices(options);
      }
    };
    void read().catch(onError);
    const timer = setInterval(() => void read().catch(onError), 30000);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [page?.id, w.workspaceId, w.scopeGrantId, w.snapshotCursor]);
  useEffect(() => {
    setEditing(undefined);
    setEventId("");
    setHistory({ rules: [], runs: [] });
  }, [page?.id, w.workspaceId, w.scopeGrantId]);
  const run = async (f: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await f();
      await onCommitted();
      await refresh();
    } catch (e) {
      onError(e);
    } finally {
      await refresh().catch(onError);
      setBusy(false);
    }
  };
  const inspect = async (rule: Rule, occurrence?: Run) => {
    setPreview(undefined);
    const event = occurrence?.occurrenceKey.startsWith("event:")
      ? occurrence.occurrenceKey.slice(6)
      : eventId;
    const at = occurrence?.occurrenceKey.startsWith("schedule:")
      ? occurrence.occurrenceKey.slice(9)
      : undefined;
    const value = await dispatch<Preview>("automationPreview", {
      ...envelope(w),
      pageId: page!.id,
      ruleId: rule.id,
      ...(event ? { eventId: event } : {}),
      ...(at ? { at } : {}),
    });
    setPreview({ rule, value, eventId: event || undefined });
  };
  return (
    <section className="automation-panel">
      <h2>Local automations</h2>
      <p>
        Rules run while Trove is open. Missed schedules skip and log by default.
        Every local effect is recorded with its occurrence so retries cannot
        duplicate it.
      </p>
      <label>
        Rules on page{" "}
        <select
          aria-label="Automation page"
          disabled={busy}
          value={page?.id ?? ""}
          onChange={(e) => setPageId(e.target.value)}
        >
          {w.pages
            .filter((p) => !p.deletedAt)
            .map((p) => (
              <option key={p.id} value={p.id}>
                {p.title}
              </option>
            ))}
        </select>
      </label>
      {!page ? (
        <p>Create a page to hold automation rules.</p>
      ) : (
        <>
          <RuleBuilder
            key={`${w.workspaceId}:${page.id}:${editing?.id ?? "new"}:${formVersion}`}
            workspace={w}
            pageId={page.id}
            initial={editing}
            choices={choices}
            busy={busy}
            onCancel={() => setEditing(undefined)}
            onSave={async (rule) => {
              await run(async () => {
                const id = rule.id || (await ids(w, 1))[0];
                await batch(
                  w,
                  [
                    {
                      kind: "automation.save",
                      pageId: page.id,
                      rule: { ...rule, id } as S.AutomationRule,
                    },
                  ],
                  pageGuards(w, page),
                );
                setEditing(undefined);
                setFormVersion((v) => v + 1);
                setPreview(undefined);
              });
            }}
          />
          <div className="db-panel">
            <h3>Rules</h3>
            {!history.rules.length && <p>No rules on this page.</p>}
            {history.rules.map(({ rule, nextAt, failures }) => (
              <article key={rule.id}>
                <h4>{rule.name}</h4>
                <p>
                  {rule.enabled
                    ? rule.paused
                      ? "Paused"
                      : "Enabled"
                    : "Disabled"}{" "}
                  ·{" "}
                  {rule.trigger.type === "schedule"
                    ? `Next ${nextAt} (${rule.trigger.timezone})`
                    : "On eligible collection changes"}
                  {failures ? ` · ${failures} failure(s)` : ""}
                </p>
                <button
                  disabled={busy}
                  onClick={() => {
                    setEditing(rule);
                    setPreview(undefined);
                  }}
                >
                  Edit rule
                </button>{" "}
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(() =>
                      batch(
                        w,
                        [
                          {
                            kind: "automation.state",
                            pageId: page.id,
                            ruleId: rule.id,
                            enabled: !rule.enabled,
                            paused: false,
                          },
                        ],
                        pageGuards(w, page),
                      ),
                    )
                  }
                >
                  {rule.enabled ? "Disable" : "Enable"}
                </button>{" "}
                <button
                  disabled={busy || !rule.enabled}
                  onClick={() =>
                    void run(() =>
                      batch(
                        w,
                        [
                          {
                            kind: "automation.state",
                            pageId: page.id,
                            ruleId: rule.id,
                            enabled: rule.enabled,
                            paused: !rule.paused,
                          },
                        ],
                        pageGuards(w, page),
                      ),
                    )
                  }
                >
                  {rule.paused ? "Resume" : "Pause"}
                </button>{" "}
                <button
                  disabled={busy}
                  onClick={() => void run(() => inspect(rule))}
                >
                  Preview
                </button>
              </article>
            ))}
            <label>
              Event for preview{" "}
              <select
                value={eventId}
                onChange={(e) => {
                  setEventId(e.target.value);
                  setPreview(undefined);
                }}
              >
                <option value="">Choose a recent committed change</option>
                {choices.events.map((event) => (
                  <option key={event.id} value={event.id}>
                    {new Date(event.at).toLocaleString()} · {event.origin} ·{" "}
                    {Array.from(
                      new Set(event.revisions.map((r) => r.target.entityType)),
                    ).join(", ")}
                  </option>
                ))}
              </select>
            </label>
            <p>
              Shows eligible origins among the 200 most recent events. Events
              must match the saved trigger and occur after its version was
              saved.
            </p>
          </div>
          {preview && (
            <section className="db-panel" aria-label="Automation preview">
              <h3>Preview: {preview.rule.name}</h3>
              <p>
                {preview.value.eligible
                  ? `${preview.value.commands.length} local action(s) at ${preview.value.at}`
                  : preview.value.diagnostic}
              </p>
              <ul>
                {preview.value.commands.slice(0, 100).map((c, i) => (
                  <li key={i}>
                    {actionSummary(c as unknown as Record<string, unknown>, w)}
                  </li>
                ))}
              </ul>
              {preview.value.commands.length > 100 && (
                <p>
                  Showing the first 100 of {preview.value.commands.length}{" "}
                  canonical effects.
                </p>
              )}
              {!preview.rule.enabled && (
                <p>
                  Enable the saved rule, then preview again to run an
                  occurrence.
                </p>
              )}
              <button
                disabled={
                  busy ||
                  !preview.value.eligible ||
                  !preview.rule.enabled ||
                  preview.rule.paused
                }
                onClick={() =>
                  void run(async () => {
                    await dispatch("automation.execute", {
                      ...envelope(w),
                      pageId: page.id,
                      ruleId: preview.rule.id,
                      occurrenceKey: preview.value.occurrenceKey,
                      at: preview.value.at,
                      expectedRevisions: preview.value.expectedRevisions,
                      ...(preview.eventId ? { eventId: preview.eventId } : {}),
                    });
                    setPreview(undefined);
                  })
                }
              >
                Run this occurrence
              </button>
              <button onClick={() => setPreview(undefined)}>
                Close preview
              </button>
            </section>
          )}
          <div className="db-panel">
            <h3>Run history</h3>
            {!history.runs.length ? (
              <p>No runs yet.</p>
            ) : (
              <table>
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Result</th>
                    <th>Details</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {history.runs.map((r) => (
                    <tr key={r.id}>
                      <td>{new Date(r.at).toLocaleString()}</td>
                      <td>{r.status}</td>
                      <td>{r.diagnostic || r.occurrenceKey}</td>
                      <td>
                        {r.status === "failed" &&
                          history.rules.some(
                            (x) =>
                              x.rule.id === r.ruleId &&
                              x.version === r.ruleVersion,
                          ) && (
                            <button
                              disabled={busy}
                              onClick={() =>
                                void run(async () => {
                                  const rule = history.rules.find(
                                    (x) => x.rule.id === r.ruleId,
                                  )?.rule;
                                  if (rule) await inspect(rule, r);
                                })
                              }
                            >
                              Review retry
                            </button>
                          )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
    </section>
  );
}
