import { useRef, useState } from "react";
import {
  dispatch,
  envelope,
  type D,
  type S,
  type WorkspaceSnapshot,
} from "../api";
import { valueOf } from "./model";
export function SchedulingPanel({
  workspace: w,
  collection,
  records,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  records: D.Record[];
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const dates = w.properties.filter(
    (p) => p.collectionId === collection.id && p.type === "date",
  );
  const [recordId, setRecordId] = useState("");
  const [propertyId, setPropertyId] = useState("");
  const [newStart, setNewStart] = useState("");
  const [timezone, setTimezone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  );
  const [calendar, setCalendar] = useState<"calendar_days" | "working_week">(
    "calendar_days",
  );
  const [weekdays, setWeekdays] = useState([1, 2, 3, 4, 5]);
  const [applying, setApplying] = useState(false);
  const [plan, setPlan] = useState<S.SchedulingPlan>();
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const dateId = propertyId || dates[0]?.id || "";
  function cancel() {
    generation.current++;
    setPlan(undefined);
    setBusy(false);
  }
  function selectRecord(id: string) {
    cancel();
    setRecordId(id);
    const r = records.find((r) => r.id === id);
    setNewStart(
      ((r ? valueOf(r, dateId) : null) as { start?: string } | null)?.start ??
        "",
    );
  }
  async function preview() {
    const run = ++generation.current;
    setBusy(true);
    setPlan(undefined);
    try {
      const value = await dispatch<S.SchedulingPlan>("scheduling.preview", {
        ...envelope(w),
        input: {
          collectionId: collection.id,
          datePropertyId: dateId,
          recordId,
          newStart,
          policy: { version: 1, calendar, timezone, weekdays },
        },
      });
      if (generation.current === run) setPlan(value);
    } catch (e) {
      if (generation.current === run) onError(e);
    } finally {
      if (generation.current === run) setBusy(false);
    }
  }
  async function apply() {
    if (!plan) return;
    setBusy(true);
    setApplying(true);
    try {
      await dispatch<S.BatchResult>("batch", {
        ...envelope(w),
        mutation: {
          scopeGrantId: w.scopeGrantId,
          idempotencyKey: `schedule-${plan.id}`,
          expectedRevisions: plan.requiredRevisions,
        },
        commands: [{ kind: "scheduling.apply", plan }],
      });
      setPlan(undefined);
      await onCommitted();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
      setApplying(false);
    }
  }
  return (
    <details className="scheduling-panel">
      <summary>Reschedule dependencies</summary>
      <p>
        Review finish-to-start changes for this collection. Date-only ranges
        count inclusive eligible dates; timed ranges preserve elapsed eligible
        time. Weekdays are whole local days. Existing slack is preserved.
      </p>
      <fieldset disabled={busy}>
        <label>
          Starting record
          <select
            aria-label="Reschedule starting record"
            value={recordId}
            onChange={(e) => selectRecord(e.target.value)}
          >
            <option value="">Choose a record</option>
            {records.map((r) => (
              <option key={r.id} value={r.id}>
                {w.pages.find((p) => p.id === r.pageId)?.title ?? r.id}
              </option>
            ))}
          </select>
        </label>
        <label>
          Date property
          <select
            aria-label="Reschedule date property"
            value={dateId}
            onChange={(e) => {
              cancel();
              setPropertyId(e.target.value);
            }}
          >
            {dates.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          New start
          <input
            aria-label="Reschedule new start"
            value={newStart}
            placeholder="2026-10-15 or 2026-10-15T09:00:00+05:30"
            onChange={(e) => {
              cancel();
              setNewStart(e.target.value);
            }}
          />
        </label>
        <label>
          Timezone
          <input
            aria-label="Reschedule timezone"
            value={timezone}
            onChange={(e) => {
              cancel();
              setTimezone(e.target.value);
            }}
          />
        </label>
        <label>
          Calendar
          <select
            aria-label="Reschedule calendar"
            value={calendar}
            onChange={(e) => {
              cancel();
              setCalendar(e.target.value as typeof calendar);
            }}
          >
            <option value="calendar_days">Calendar days</option>
            <option value="working_week">Working week</option>
          </select>
        </label>
        {calendar === "working_week" && (
          <div>
            {[
              "Sunday",
              "Monday",
              "Tuesday",
              "Wednesday",
              "Thursday",
              "Friday",
              "Saturday",
            ].map((day, index) => (
              <label key={day}>
                <input
                  type="checkbox"
                  checked={weekdays.includes(index)}
                  onChange={(e) => {
                    cancel();
                    setWeekdays((old) =>
                      e.target.checked
                        ? [...old, index].sort()
                        : old.filter((v) => v !== index),
                    );
                  }}
                />
                {day}
              </label>
            ))}
          </div>
        )}
      </fieldset>
      <button
        disabled={busy || !recordId || !dateId || !newStart}
        onClick={() => void preview()}
      >
        Preview complete schedule
      </button>
      <button disabled={applying || (!busy && !plan)} onClick={cancel}>
        Cancel schedule
      </button>
      {busy && <p role="status">Checking schedule…</p>}
      {plan && (
        <section aria-label="Complete scheduling preview">
          <p>
            {plan.input.policy.timezone} ·{" "}
            {plan.input.policy.calendar.replaceAll("_", " ")} ·{" "}
            {plan.records.filter((r) => r.changed).length} changes. Other rows
            show preserved slack or prerequisite constraints. Apply and undo are
            atomic.
          </p>
          <table>
            <thead>
              <tr>
                <th>Record</th>
                <th>Before</th>
                <th>After</th>
                <th>Reason and duration</th>
              </tr>
            </thead>
            <tbody>
              {plan.records.map((r) => (
                <tr key={r.recordId}>
                  <th>{r.title}</th>
                  <td>
                    {r.before.start}
                    {r.before.end && ` → ${r.before.end}`}
                  </td>
                  <td>
                    {r.after.start}
                    {r.after.end && ` → ${r.after.end}`}
                  </td>
                  <td>
                    {r.reason.replaceAll("_", " ")} · {r.duration}
                    {r.dstEffects.map((note) => (
                      <p key={note}>{note}</p>
                    ))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <button
            disabled={busy || !plan.records.some((r) => r.changed)}
            onClick={() => void apply()}
          >
            Apply reviewed schedule
          </button>
        </section>
      )}
    </details>
  );
}
