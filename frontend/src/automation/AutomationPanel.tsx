import { useEffect, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  type WorkspaceSnapshot,
  type S,
} from "../api";
type Rule = {
  id: string;
  name: string;
  enabled: boolean;
  paused: boolean;
  allowedOrigins: string[];
  trigger: {
    type: string;
    timezone?: string;
    frequency?: string;
    hour?: number;
    minute?: number;
    missedPolicy?: string;
    catchUpLimit?: number;
    collectionId?: string;
  };
  actions: Record<string, unknown>[];
};
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
  const [name, setName] = useState("Daily note");
  const [hour, setHour] = useState("09:00");
  const [zone, setZone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  );
  const [policy, setPolicy] = useState("skip");
  const [limit, setLimit] = useState(3);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<{
    rule: Rule;
    value: Preview;
    eventId?: string;
  }>();
  const [trigger, setTrigger] = useState("schedule");
  const [collection, setCollection] = useState("");
  const [allowAI, setAllowAI] = useState(false);
  const [eventId, setEventId] = useState("");
  const page = w.pages.find(
    (p) => p.id === (pageId || w.pages.find((p) => !p.deletedAt)?.id),
  );
  const refresh = async () => {
    if (page)
      setHistory(
        await dispatch<History>("automationHistory", {
          ...envelope(w),
          pageId: page.id,
        }),
      );
  };
  useEffect(() => {
    void refresh().catch(onError);
    const timer = setInterval(() => void refresh().catch(onError), 30000);
    return () => clearInterval(timer);
  }, [page?.id, w.snapshotCursor]);
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
          <form
            className="db-panel"
            onSubmit={(e) => {
              e.preventDefault();
              void run(async () => {
                const [id] = await ids(w, 1);
                const [h, m] = hour.split(":").map(Number);
                const rule: Rule = {
                  id,
                  name,
                  enabled: false,
                  paused: false,
                  allowedOrigins: allowAI ? ["user", "ai"] : ["user"],
                  trigger:
                    trigger === "schedule"
                      ? {
                          type: "schedule",
                          frequency: "daily",
                          timezone: zone,
                          hour: h,
                          minute: m,
                          missedPolicy: policy,
                          catchUpLimit: limit,
                        }
                      : { type: "event", collectionId: collection },
                  actions: [
                    {
                      kind: "page.create",
                      pageId: "$id:page",
                      parentPageId: page.id,
                      title: `${name} · {{occurrence}}`,
                    },
                  ],
                };
                await batch(
                  w,
                  [
                    {
                      kind: "automation.save",
                      pageId: page.id,
                      rule: rule as unknown as { [key: string]: unknown },
                    },
                  ],
                  pageGuards(w, page),
                );
              });
            }}
          >
            <h3>Create a recurring or event note</h3>
            <label>
              Name{" "}
              <input
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label>
              Trigger{" "}
              <select
                value={trigger}
                onChange={(e) => setTrigger(e.target.value)}
              >
                <option value="schedule">Every day</option>
                <option value="event">Collection changes</option>
              </select>
            </label>
            {trigger === "schedule" ? (
              <>
                <label>
                  Local time{" "}
                  <input
                    type="time"
                    required
                    value={hour}
                    onChange={(e) => setHour(e.target.value)}
                  />
                </label>
                <label>
                  Timezone{" "}
                  <input
                    required
                    value={zone}
                    onChange={(e) => setZone(e.target.value)}
                  />
                </label>
                <label>
                  Missed runs{" "}
                  <select
                    value={policy}
                    onChange={(e) => setPolicy(e.target.value)}
                  >
                    <option value="skip">Skip and log</option>
                    <option value="catch_up">Bounded catch-up</option>
                  </select>
                </label>
                {policy === "catch_up" && (
                  <label>
                    Maximum catch-up{" "}
                    <input
                      type="number"
                      min={1}
                      max={20}
                      value={limit}
                      onChange={(e) => setLimit(Number(e.target.value))}
                    />
                  </label>
                )}
              </>
            ) : (
              <>
                <label>
                  Collection{" "}
                  <select
                    required
                    value={collection}
                    onChange={(e) => setCollection(e.target.value)}
                  >
                    <option value="">Choose collection</option>
                    {w.collections.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.title}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={allowAI}
                    onChange={(e) => setAllowAI(e.target.checked)}
                  />{" "}
                  Explicitly allow reviewed AI changes
                </label>
              </>
            )}
            <p>
              Creates a child page using this name and the intended occurrence
              time. Save first, inspect the preview, then enable.
            </p>
            <button disabled={busy}>Save disabled rule</button>
          </form>
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
              Event ID for an event preview{" "}
              <input
                value={eventId}
                onChange={(e) => setEventId(e.target.value)}
                placeholder="Choose a committed event"
              />
            </label>
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
                {preview.value.commands.map((c, i) => (
                  <li key={i}>
                    {c.kind}
                    {"title" in c ? `: ${c.title}` : ""}
                  </li>
                ))}
              </ul>
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
                        {r.status === "failed" && (
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
