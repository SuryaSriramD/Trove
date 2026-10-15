import { useCallback, useEffect, useRef, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  type D,
  type S,
  type WorkspaceSnapshot,
} from "../api";

type NativeStatus = S.ReminderDesktopStatus;
type Bridge = {
  ReminderStatus(): Promise<NativeStatus>;
  RefreshReminders(): Promise<NativeStatus>;
  SetReminderBackgroundEnabled(enabled: boolean): Promise<NativeStatus>;
};
const native = () => window.go?.main.App as unknown as Bridge | undefined;
const localDate = () => {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
};
type Edit = Omit<S.ReminderRule, "id" | "revision" | "title"> & {
  id?: string;
  revision: string;
};

export function ReminderPanel({
  workspace: w,
  onRefresh,
  onNavigate,
}: {
  workspace: WorkspaceSnapshot;
  onRefresh: () => Promise<unknown> | void;
  onNavigate: (location: D.Location) => void;
}) {
  const [state, setState] = useState<S.ReminderState>({
    rules: [],
    inbox: [],
    more: false,
  });
  const scope = useRef(w.workspaceId);
  scope.current = w.workspaceId;
  const requests = useRef(0);
  const [cursors, setCursors] = useState<string[]>([]);
  const cursor = cursors.at(-1);
  const [status, setStatus] = useState<NativeStatus | null>(null);
  const [edit, setEdit] = useState<Edit | null>(null);
  const [history, setHistory] = useState<S.ReminderRule[] | null>(null);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const refresh = useCallback(async () => {
    const request = ++requests.current,
      workspaceId = w.workspaceId;
    const [next, background] = await Promise.all([
      dispatch<S.ReminderState>("reminders.list", {
        ...envelope(w),
        ...(cursor ? { before: cursor } : {}),
      }),
      native()?.ReminderStatus?.(),
    ]);
    if (request !== requests.current || workspaceId !== scope.current) return;
    setState(next);
    setStatus(background ?? null);
  }, [w.workspaceId, cursor]);
  useEffect(() => {
    setCursors([]);
    setEdit(null);
    setHistory(null);
    setState({ rules: [], inbox: [], more: false });
    setStatus(null);
    setBusy(false);
    setError("");
  }, [w.workspaceId]);
  useEffect(() => {
    let live = true;
    const run = () =>
      void refresh().catch((e) => {
        if (live) setError(String(e.message ?? e));
      });
    run();
    const timer = setInterval(run, 5000);
    return () => {
      live = false;
      requests.current += 1;
      clearInterval(timer);
    };
  }, [refresh]);
  async function run(work: () => Promise<unknown>) {
    const workspaceId = w.workspaceId;
    setBusy(true);
    setError("");
    try {
      await work();
      if (workspaceId !== scope.current) return;
      await onRefresh();
      await refresh();
    } catch (e) {
      if (workspaceId === scope.current)
        setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (workspaceId === scope.current) setBusy(false);
    }
  }
  async function publish() {
    const workspaceId = w.workspaceId;
    if (workspaceId !== scope.current) return;
    if (native()?.RefreshReminders) {
      const next = await native()!.RefreshReminders();
      if (workspaceId === scope.current) setStatus(next);
    }
  }
  async function background(enabled: boolean) {
    const workspaceId = w.workspaceId;
    const next = await native()!.SetReminderBackgroundEnabled(enabled);
    if (workspaceId === scope.current) setStatus(next);
  }
  async function showHistory(reminderId: string) {
    const workspaceId = w.workspaceId;
    const next = await dispatch<S.ReminderRule[]>("reminders.history", {
      ...envelope(w),
      reminderId,
    });
    if (workspaceId === scope.current) setHistory(next);
  }
  function source(pageId: string) {
    const page = w.pages.find((p) => p.id === pageId && !p.deletedAt);
    if (!page)
      throw Error("The reminder source is unavailable. Refresh the workspace.");
    return page;
  }
  async function save(value: Edit) {
    const page = source(value.pageId),
      reminderId = value.id ?? (await ids(w, 1))[0];
    await batch(
      w,
      [
        {
          kind: "reminder.put",
          reminderId,
          pageId: value.pageId,
          expectedRevision: value.revision,
          startDate: value.startDate,
          localTime: value.localTime,
          timezone: value.timezone,
          frequency: value.frequency,
          enabled: value.enabled,
          private: value.private,
        },
      ],
      pageGuards(w, page),
    );
    if (w.workspaceId === scope.current) setEdit(null);
    await publish();
  }
  async function act(
    item: S.ReminderInboxItem,
    action: "snoozed" | "dismissed",
  ) {
    await batch(
      w,
      [
        {
          kind: "reminder.inboxAction",
          occurrenceId: item.id,
          expectedAction: item.action ?? "",
          expectedActionRevision: item.actionRevision ?? 0,
          action,
          ...(action === "snoozed"
            ? { snoozedUntil: new Date(Date.now() + 900000).toISOString() }
            : {}),
        },
      ],
      pageGuards(w, source(item.pageId)),
    );
    await publish();
  }
  async function complete(item: S.ReminderInboxItem) {
    const page = source(item.pageId),
      record = w.records.find((r) => r.pageId === page.id),
      facet = w.taskFacets.find((f) => f.recordId === record?.id),
      property = w.properties.find((p) => p.id === facet?.statusPropertyId);
    const option = (property?.config as { doneOptionId?: string })
      ?.doneOptionId;
    if (!record || !facet || !property || !option)
      throw Error("This source has no canonical task completion binding.");
    await batch(
      w,
      [{ kind: "task.setStatus", recordId: record.id, statusOptionId: option }],
      [
        ...pageGuards(w, page),
        {
          target: { entityType: "property", entityId: property.id },
          revision: property.revision,
        },
      ],
    );
    await publish();
  }
  return (
    <section className="reminder-panel" aria-label="Personal reminders">
      <h1>Reminders</h1>
      <p>
        Reminder times are separate from task due dates. Every missed occurrence
        stays in this inbox; recovery produces one catch-up summary.
      </p>
      {error && <p role="alert">{error}</p>}
      <section aria-label="Background reminder settings">
        <h2>Background delivery</h2>
        <p>
          Opt in to the macOS login item to receive reminders with Trove closed.
          You must be logged in. Sleep, shutdown, Focus and notification
          settings can delay or suppress delivery. Submission does not prove
          that a notification was seen.
        </p>
        <p>
          {status
            ? `Registration: ${status.registration} · Schedule: ${status.publication} · Notifications: ${status.permission}`
            : "Background delivery is available in the packaged macOS app."}
        </p>
        {status?.error && <p role="alert">{status.error}</p>}
        {status?.error && status.available && (
          <button
            disabled={busy}
            onClick={() => void run(() => background(false))}
          >
            Turn off background reminders
          </button>
        )}
        <button
          disabled={busy || !status?.available}
          onClick={() => void run(() => background(!status?.enabled))}
        >
          {status?.enabled
            ? "Disable background reminders"
            : "Enable background reminders"}
        </button>
        <button
          disabled={busy || !status?.available}
          onClick={() => void run(publish)}
        >
          Reconcile schedule
        </button>
        <p>
          Restored workspaces require fresh opt-in. The helper retains derived
          schedules and delivery evidence; it cannot edit workspace content.
        </p>
      </section>
      <button
        disabled={busy}
        onClick={() => {
          setHistory(null);
          setEdit({
            pageId: w.pages.find((p) => !p.deletedAt)?.id ?? "",
            revision: "0",
            startDate: localDate(),
            localTime: "09:00",
            timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
            frequency: "once",
            enabled: true,
            private: true,
          });
        }}
      >
        New reminder
      </button>
      {edit && (
        <form
          aria-label="Reminder editor"
          onSubmit={(event) => {
            event.preventDefault();
            void run(() => save(edit));
          }}
        >
          <label>
            Source page or task
            <select
              aria-label="Reminder source"
              disabled={busy || !!edit.id}
              value={edit.pageId}
              onChange={(event) =>
                setEdit({ ...edit, pageId: event.target.value })
              }
            >
              {w.pages
                .filter((p) => !p.deletedAt)
                .map((page) => (
                  <option key={page.id} value={page.id}>
                    {page.title || "Untitled"}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Date
            <input
              aria-label="Reminder date"
              type="date"
              min="2000-01-01"
              max="2200-12-31"
              required
              value={edit.startDate}
              onChange={(event) =>
                setEdit({ ...edit, startDate: event.target.value })
              }
            />
          </label>
          <label>
            Time
            <input
              aria-label="Reminder time"
              type="time"
              required
              value={edit.localTime}
              onChange={(event) =>
                setEdit({ ...edit, localTime: event.target.value })
              }
            />
          </label>
          <label>
            Timezone
            <input
              aria-label="Reminder timezone"
              required
              value={edit.timezone}
              onChange={(event) =>
                setEdit({ ...edit, timezone: event.target.value })
              }
            />
          </label>
          <label>
            Repeat
            <select
              aria-label="Reminder recurrence"
              value={edit.frequency}
              onChange={(event) =>
                setEdit({
                  ...edit,
                  frequency: event.target.value as Edit["frequency"],
                })
              }
            >
              <option value="once">Once</option>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly on the selected weekday</option>
            </select>
          </label>
          <p>
            Date-only reminders start at an editable 09:00. This timezone
            remains fixed when your system timezone changes. DST gaps move to
            the next valid minute; repeated times use the earlier occurrence.
            Entirely skipped local dates have no occurrence.
          </p>
          <label>
            <input
              type="checkbox"
              checked={edit.private}
              onChange={(event) =>
                setEdit({ ...edit, private: event.target.checked })
              }
            />
            Use private notification text
          </label>
          <label>
            <input
              type="checkbox"
              checked={edit.enabled}
              onChange={(event) =>
                setEdit({ ...edit, enabled: event.target.checked })
              }
            />
            Schedule enabled
          </label>
          <button disabled={busy || !edit.pageId} type="submit">
            Save reminder
          </button>
          <button disabled={busy} type="button" onClick={() => setEdit(null)}>
            Cancel editing
          </button>
        </form>
      )}
      <h2>Configured reminders</h2>
      {!state.rules.length && (
        <p>No reminders yet. Choose a page or task and a reminder time.</p>
      )}
      {state.rules.map((rule) => (
        <article key={rule.id}>
          <strong>{rule.title}</strong>
          <p>
            {rule.startDate} {rule.localTime} · {rule.timezone} ·{" "}
            {rule.frequency} ·{" "}
            {rule.enabled ? "Enabled" : "Inactive or source completed"}
          </p>
          <button disabled={busy} onClick={() => setEdit(rule)}>
            Edit reminder
          </button>
          <button
            disabled={busy || !rule.enabled}
            onClick={() => void run(() => save({ ...rule, enabled: false }))}
          >
            Cancel reminder
          </button>
          <button
            disabled={busy}
            onClick={() => void run(() => showHistory(rule.id))}
          >
            Reminder history
          </button>
          <button
            disabled={
              !w.pages.some((p) => p.id === rule.pageId && !p.deletedAt)
            }
            onClick={() =>
              onNavigate({
                pageId: rule.pageId,
                sourceRevision: source(rule.pageId).revision,
              })
            }
          >
            Open source
          </button>
        </article>
      ))}
      {history && (
        <section aria-label="Reminder history">
          <h3>Configuration history</h3>
          {history.map((rule) => (
            <p key={rule.revision}>
              Version {rule.revision}: {rule.startDate} {rule.localTime}{" "}
              {rule.timezone} · {rule.frequency} ·{" "}
              {rule.enabled ? "Enabled" : "Inactive"}
            </p>
          ))}
          <button onClick={() => setHistory(null)}>Close history</button>
        </section>
      )}
      <h2>Reminder inbox</h2>
      {state.more && (
        <p role="status">
          Recovering older occurrences in bounded batches. All occurrences
          remain retained.
        </p>
      )}
      {!state.inbox.length && <p>No due reminders yet.</p>}
      <nav aria-label="Reminder inbox pages">
        <button
          disabled={busy || !cursors.length}
          onClick={() => setCursors(cursors.slice(0, -1))}
        >
          Newer reminders
        </button>
        <button
          disabled={busy || !state.nextCursor}
          onClick={() =>
            state.nextCursor && setCursors([...cursors, state.nextCursor])
          }
        >
          Older reminders
        </button>
      </nav>
      {state.inbox.map((item) => (
        <article key={item.id}>
          <strong>{item.title}</strong>
          <p>
            {new Date(item.dueAt).toLocaleString()} · {item.outcome}
            {item.action ? ` · ${item.action}` : ""}
            {item.snoozedUntil
              ? ` until ${new Date(item.snoozedUntil).toLocaleString()}`
              : ""}
          </p>
          {item.error && <p role="alert">{item.error}</p>}
          <button
            disabled={
              !w.pages.some((p) => p.id === item.pageId && !p.deletedAt)
            }
            onClick={() =>
              onNavigate({
                pageId: item.pageId,
                sourceRevision: source(item.pageId).revision,
              })
            }
          >
            Open source
          </button>
          <button
            disabled={busy || item.action === "dismissed"}
            onClick={() => void run(() => act(item, "snoozed"))}
          >
            Snooze 15 minutes
          </button>
          <button
            disabled={busy || item.action === "dismissed"}
            onClick={() => void run(() => act(item, "dismissed"))}
          >
            Dismiss reminder
          </button>
          {w.taskFacets.some((f) =>
            w.records.some(
              (r) => r.id === f.recordId && r.pageId === item.pageId,
            ),
          ) && (
            <button
              disabled={busy}
              onClick={() => void run(() => complete(item))}
            >
              Complete task
            </button>
          )}
        </article>
      ))}
    </section>
  );
}
