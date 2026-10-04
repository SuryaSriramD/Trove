import { useState } from "react";
import type { D, WorkspaceSnapshot } from "../api";
import { displayValue, options } from "../database/model";
export type Rule = {
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
    weekday?: number;
    missedPolicy?: string;
    catchUpLimit?: number;
    collectionId?: string;
    entityType?: string;
  };
  condition?: {
    recordId: string;
    propertyId: string;
    op: string;
    value?: unknown;
  };
  actions: Record<string, unknown>[];
};
export type AutomationOptions = {
  templates: {
    id: string;
    name: string;
    hash: string;
    collectionId: string;
    sourcePageId: string;
  }[];
  templatesTruncated: boolean;
  events: {
    id: string;
    origin: string;
    at: string;
    revisions: { target: { entityType: string; entityId: string } }[];
  }[];
};
const obj = (v: unknown): Record<string, any> =>
  v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, any>)
    : {};
const text = (v: unknown) => (typeof v === "string" ? v : "");
const supportedTypes = new Set([
  "text",
  "number",
  "checkbox",
  "select",
  "multi_select",
  "status",
  "date",
  "url",
  "email",
  "phone",
  "relation",
]);
const zone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
export function defaultValue(p: D.PropertyDefinition): unknown {
  switch (p.type) {
    case "text":
      return [{ text: "", marks: [] }];
    case "number":
      return { decimal: "0", format: obj(p.config).format ?? "number" };
    case "checkbox":
      return false;
    case "select":
    case "status":
      return options(p)[0]?.id ?? null;
    case "multi_select":
    case "relation":
      return [];
    case "date":
      return {
        start: new Date().toISOString().slice(0, 10),
        end: null,
        timezone: zone(),
        hasTime: false,
      };
    default:
      return "";
  }
}
export function TypedRuleValue({
  property: p,
  value,
  onChange,
  workspace: w,
  label,
}: {
  property: D.PropertyDefinition;
  value: unknown;
  onChange: (v: unknown) => void;
  workspace: WorkspaceSnapshot;
  label: string;
}) {
  const v = obj(value);
  if (p.type === "checkbox")
    return (
      <label>
        {label}{" "}
        <select
          value={value === true ? "true" : "false"}
          onChange={(e) => onChange(e.target.value === "true")}
        >
          <option value="false">No</option>
          <option value="true">Yes</option>
        </select>
      </label>
    );
  if (
    p.type === "select" ||
    p.type === "status" ||
    p.type === "multi_select" ||
    p.type === "relation"
  ) {
    const multiple = p.type === "multi_select" || p.type === "relation";
    const choices =
      p.type === "relation"
        ? w.records
            .filter((r) => r.collectionId === obj(p.config).targetCollectionId)
            .map((r) => ({
              id: r.id,
              label: w.pages.find((p) => p.id === r.pageId)?.title ?? r.id,
            }))
        : options(p);
    return (
      <label>
        {label}{" "}
        <select
          multiple={multiple}
          value={
            multiple
              ? Array.isArray(value)
                ? (value as string[])
                : []
              : text(value)
          }
          onChange={(e) =>
            onChange(
              multiple
                ? Array.from(e.target.selectedOptions, (o) => o.value)
                : e.target.value || null,
            )
          }
        >
          {!multiple && <option value="">Empty</option>}
          {choices.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </select>
      </label>
    );
  }
  if (p.type === "date")
    return (
      <fieldset>
        <legend>{label}</legend>
        <label>
          Include time{" "}
          <input
            type="checkbox"
            checked={v.hasTime === true}
            onChange={(e) =>
              onChange({
                start: e.target.checked
                  ? `${text(v.start).slice(0, 10)}T09:00:00Z`
                  : text(v.start).slice(0, 10),
                end: null,
                timezone: text(v.timezone) || zone(),
                hasTime: e.target.checked,
              })
            }
          />
        </label>
        <label>
          Start{" "}
          <input
            required
            type={v.hasTime ? "text" : "date"}
            placeholder={v.hasTime ? "2026-10-05T09:00:00+05:30" : undefined}
            value={text(v.start)}
            onChange={(e) => onChange({ ...v, start: e.target.value })}
          />
        </label>
        <label>
          End (optional){" "}
          <input
            type={v.hasTime ? "text" : "date"}
            value={text(v.end)}
            onChange={(e) => onChange({ ...v, end: e.target.value || null })}
          />
        </label>
        <label>
          Date timezone{" "}
          <input
            required
            value={text(v.timezone)}
            onChange={(e) => onChange({ ...v, timezone: e.target.value })}
          />
        </label>
        {v.hasTime && (
          <p>
            Use a timestamp with its UTC offset. Preview checks it against the
            timezone.
          </p>
        )}
      </fieldset>
    );
  if (!supportedTypes.has(p.type))
    return (
      <p>
        Existing {p.type} value retained:{" "}
        {displayValue(value as D.RawValue, p, w.pages)}. Choose a supported
        property to change its value.
      </p>
    );
  return (
    <label>
      {label}{" "}
      <input
        required={p.type === "number"}
        inputMode={p.type === "number" ? "decimal" : undefined}
        type={p.type === "email" ? "email" : "text"}
        value={
          p.type === "number"
            ? text(v.decimal)
            : p.type === "text"
              ? displayValue(value as D.RawValue, p)
              : text(value)
        }
        onChange={(e) =>
          onChange(
            p.type === "number"
              ? { ...v, decimal: e.target.value, format: v.format ?? "number" }
              : p.type === "text"
                ? [{ text: e.target.value, marks: [] }]
                : e.target.value,
          )
        }
      />
    </label>
  );
}
function RecordChoice({
  w,
  value,
  onChange,
  label,
  extra = [],
}: {
  w: WorkspaceSnapshot;
  value: string;
  onChange: (id: string) => void;
  label: string;
  extra?: { id: string; pageId: string; collectionId: string; title: string }[];
}) {
  const [search, setSearch] = useState("");
  const records = [
    ...w.records.map((r) => ({
      ...r,
      title: w.pages.find((p) => p.id === r.pageId)?.title ?? r.id,
    })),
    ...extra,
  ];
  const filtered = records.filter((r) =>
    r.title.toLowerCase().includes(search.toLowerCase()),
  );
  const shown = filtered.slice(0, 200);
  const chosen = records.find((r) => r.id === value);
  if (chosen && !shown.some((r) => r.id === value)) shown.unshift(chosen);
  return (
    <>
      <label>
        Find {label.toLowerCase()}{" "}
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </label>
      <label>
        {label}{" "}
        <select
          required
          value={value}
          onChange={(e) => onChange(e.target.value)}
        >
          <option value="">Choose record</option>
          {shown.map((r) => (
            <option key={r.id} value={r.id}>
              {r.title}
            </option>
          ))}
        </select>
      </label>
      {filtered.length > 200 && (
        <small>Showing 200 matches; narrow the search.</small>
      )}
    </>
  );
}
export function RuleBuilder({
  workspace: w,
  pageId,
  initial,
  choices,
  busy,
  onSave,
  onCancel,
}: {
  workspace: WorkspaceSnapshot;
  pageId: string;
  initial?: Rule;
  choices: AutomationOptions;
  busy: boolean;
  onSave: (r: Rule) => Promise<void>;
  onCancel: () => void;
}) {
  const [rule, setRule] = useState<Rule>(() =>
    structuredClone(
      initial ?? {
        id: "",
        name: "Daily note",
        enabled: false,
        paused: false,
        allowedOrigins: ["user"],
        trigger: {
          type: "schedule",
          frequency: "daily",
          timezone: zone(),
          hour: 9,
          minute: 0,
          weekday: 1,
          missedPolicy: "skip",
          catchUpLimit: 3,
        },
        actions: [
          {
            kind: "page.create",
            pageId: "$id:page-1",
            parentPageId: pageId,
            title: "Daily note · {{occurrence}}",
          },
        ],
      },
    ),
  );
  const update = (patch: Partial<Rule>) => setRule((r) => ({ ...r, ...patch }));
  const trigger = (patch: Partial<Rule["trigger"]>) =>
    setRule((r) => ({ ...r, trigger: { ...r.trigger, ...patch } }));
  const freshName = () => {
    let n = 1;
    const ids = new Set(rule.actions.flatMap((a) => [a.pageId, a.recordId]));
    while (
      ids.has(`$id:page-${n}`) ||
      ids.has(`$id:record-${n}`) ||
      ids.has(`$id:record-page-${n}`)
    )
      n++;
    return n;
  };
  const changeAction = (index: number, a: Record<string, unknown>) =>
    update({ actions: rule.actions.map((old, i) => (i === index ? a : old)) });
  const condition = rule.condition,
    record = w.records.find((r) => r.id === condition?.recordId),
    conditionProperty = w.properties.find(
      (p) => p.id === condition?.propertyId,
    );
  const conditionProps = w.properties.filter(
    (p) =>
      p.collectionId === record?.collectionId &&
      (supportedTypes.has(p.type) || p.id === condition?.propertyId),
  );
  return (
    <form
      className="db-panel"
      onSubmit={(e) => {
        e.preventDefault();
        void onSave({ ...rule, enabled: false, paused: false });
      }}
    >
      <fieldset disabled={busy}>
        <legend>{initial ? "Edit saved rule" : "Create a local rule"}</legend>
        <label>
          Name{" "}
          <input
            required
            maxLength={200}
            value={rule.name}
            onChange={(e) => update({ name: e.target.value })}
          />
        </label>
        <label>
          Trigger{" "}
          <select
            value={rule.trigger.type}
            onChange={(e) =>
              update({
                trigger:
                  e.target.value === "schedule"
                    ? {
                        type: "schedule",
                        frequency: "daily",
                        timezone: zone(),
                        hour: 9,
                        minute: 0,
                        weekday: 1,
                        missedPolicy: "skip",
                        catchUpLimit: 3,
                      }
                    : { type: "event", collectionId: w.collections[0]?.id },
              })
            }
          >
            <option value="schedule">Schedule</option>
            <option value="event">Workspace changes</option>
          </select>
        </label>
        {rule.trigger.type === "schedule" ? (
          <>
            <label>
              Frequency{" "}
              <select
                value={rule.trigger.frequency}
                onChange={(e) => trigger({ frequency: e.target.value })}
              >
                <option value="daily">Daily</option>
                <option value="weekly">Weekly</option>
              </select>
            </label>
            {rule.trigger.frequency === "weekly" && (
              <label>
                Weekday{" "}
                <select
                  value={rule.trigger.weekday ?? 0}
                  onChange={(e) => trigger({ weekday: Number(e.target.value) })}
                >
                  {[
                    "Sunday",
                    "Monday",
                    "Tuesday",
                    "Wednesday",
                    "Thursday",
                    "Friday",
                    "Saturday",
                  ].map((d, i) => (
                    <option key={d} value={i}>
                      {d}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <label>
              Local time{" "}
              <input
                type="time"
                required
                value={`${String(rule.trigger.hour ?? 0).padStart(2, "0")}:${String(rule.trigger.minute ?? 0).padStart(2, "0")}`}
                onChange={(e) => {
                  const [hour, minute] = e.target.value.split(":").map(Number);
                  trigger({ hour, minute });
                }}
              />
            </label>
            <label>
              Timezone{" "}
              <input
                required
                value={rule.trigger.timezone ?? ""}
                onChange={(e) => trigger({ timezone: e.target.value })}
              />
            </label>
            <label>
              Missed runs{" "}
              <select
                value={rule.trigger.missedPolicy ?? "skip"}
                onChange={(e) => trigger({ missedPolicy: e.target.value })}
              >
                <option value="skip">Skip and log</option>
                <option value="catch_up">Bounded catch-up</option>
              </select>
            </label>
            {rule.trigger.missedPolicy === "catch_up" && (
              <label>
                Maximum catch-up{" "}
                <input
                  type="number"
                  required
                  min={1}
                  max={20}
                  value={rule.trigger.catchUpLimit ?? 1}
                  onChange={(e) =>
                    trigger({ catchUpLimit: Number(e.target.value) })
                  }
                />
              </label>
            )}
            <p>
              Schedules run while Trove is open. Missing daylight-saving times
              are skipped; repeated times run once.
            </p>
          </>
        ) : (
          <>
            <label>
              Collection{" "}
              <select
                value={rule.trigger.collectionId ?? ""}
                onChange={(e) =>
                  update({
                    trigger: {
                      type: "event",
                      ...(e.target.value
                        ? { collectionId: e.target.value }
                        : { entityType: "record" }),
                    },
                  })
                }
              >
                <option value="">Any collection</option>
                {w.collections.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.title}
                  </option>
                ))}
              </select>
            </label>
            {!rule.trigger.collectionId && (
              <label>
                Entity changes{" "}
                <select
                  value={rule.trigger.entityType ?? ""}
                  onChange={(e) => {
                    const { entityType: _, ...rest } = rule.trigger;
                    update({
                      trigger: e.target.value
                        ? { ...rest, entityType: e.target.value }
                        : rest,
                    });
                  }}
                >
                  <option value="">Any entity</option>
                  {[
                    "page",
                    "block",
                    "collection",
                    "property",
                    "record",
                    "view",
                    "container",
                    "file",
                    "meeting",
                    "transcript_segment",
                    "memory",
                  ].map((k) => (
                    <option key={k} value={k}>
                      {k.replaceAll("_", " ")}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <label>
              <input
                type="checkbox"
                checked={rule.allowedOrigins.includes("ai")}
                onChange={(e) =>
                  update({
                    allowedOrigins: e.target.checked
                      ? ["user", "ai"]
                      : ["user"],
                  })
                }
              />{" "}
              Explicitly include reviewed AI changes
            </label>
            <p>Import, restore and automation events never trigger rules.</p>
          </>
        )}
        <label>
          <input
            type="checkbox"
            checked={!!condition}
            onChange={(e) =>
              update({
                condition: e.target.checked
                  ? { recordId: "", propertyId: "", op: "eq", value: null }
                  : undefined,
              })
            }
          />{" "}
          Only run when a record matches
        </label>
        {condition && (
          <fieldset>
            <legend>Condition</legend>
            <RecordChoice
              w={w}
              label="Condition record"
              value={condition.recordId}
              onChange={(recordId) =>
                update({
                  condition: {
                    recordId,
                    propertyId: "",
                    op: condition.op,
                    value: null,
                  },
                })
              }
            />
            <label>
              Condition property{" "}
              <select
                required
                value={condition.propertyId}
                onChange={(e) => {
                  const p = w.properties.find((p) => p.id === e.target.value);
                  update({
                    condition: {
                      ...condition,
                      propertyId: e.target.value,
                      value: p ? defaultValue(p) : null,
                    },
                  });
                }}
              >
                <option value="">Choose property</option>
                {conditionProps.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Comparison{" "}
              <select
                value={condition.op}
                onChange={(e) =>
                  update({ condition: { ...condition, op: e.target.value } })
                }
              >
                <option value="eq">Equals</option>
                <option value="ne">Does not equal</option>
                <option value="is_empty">Is empty</option>
              </select>
            </label>
            {conditionProperty && condition.op !== "is_empty" && (
              <TypedRuleValue
                workspace={w}
                property={conditionProperty}
                value={condition.value}
                label="Condition value"
                onChange={(value) =>
                  update({ condition: { ...condition, value } })
                }
              />
            )}
            <p>
              The condition reads this selected record; it does not substitute
              the triggering record.
            </p>
          </fieldset>
        )}
        <h4>Actions</h4>
        <p>
          Use {"{{occurrence}}"} in text for the intended UTC occurrence time.
          Preview checks all actions together.
        </p>
        {rule.actions.map((a, i) => {
          const patch = (p: Record<string, unknown>) =>
            changeAction(i, { ...a, ...p });
          const extras = rule.actions
            .slice(0, i)
            .filter((x) => x.kind === "record.create")
            .map((x) => ({
              id: text(x.recordId),
              pageId: text(x.pageId),
              collectionId: text(x.collectionId),
              title: `New record: ${text(x.title)}`,
            }));
          const record =
            w.records.find((r) => r.id === a.recordId) ??
            extras.find((r) => r.id === a.recordId);
          const property = w.properties.find((p) => p.id === a.propertyId);
          const facet = w.taskFacets.find((t) => t.recordId === a.recordId),
            status = w.properties.find((p) => p.id === facet?.statusPropertyId);
          return (
            <fieldset key={i}>
              <legend>Action {i + 1}</legend>
              <label>
                Action type{" "}
                <select
                  value={text(a.kind)}
                  onChange={(e) => {
                    const kind = e.target.value,
                      fresh = freshName();
                    changeAction(
                      i,
                      kind === "page.create"
                        ? {
                            kind,
                            pageId: `$id:page-${fresh}`,
                            parentPageId: pageId,
                            title: rule.name,
                          }
                        : kind === "record.create"
                          ? {
                              kind,
                              recordId: `$id:record-${fresh}`,
                              pageId: `$id:record-page-${fresh}`,
                              collectionId: "",
                              title: rule.name,
                            }
                          : kind === "template.instantiate"
                            ? {
                                kind,
                                templateId: "",
                                templateHash: "",
                                title: `${rule.name} · {{occurrence}}`,
                              }
                            : kind === "page.update"
                              ? { kind, pageId, title: rule.name }
                              : { kind, recordId: "" },
                    );
                  }}
                >
                  <option value="page.create">Create a page</option>
                  <option value="page.update">Update a page</option>
                  <option value="record.create">
                    Create a database record
                  </option>
                  <option value="record.setValue">Set a property</option>
                  <option value="record.clearValue">Clear a property</option>
                  <option value="task.setStatus">Set task status</option>
                  <option value="template.instantiate">
                    Create from a saved template
                  </option>
                </select>
              </label>
              {(a.kind === "page.create" || a.kind === "page.update") && (
                <label>
                  {a.kind === "page.create" ? "Parent page" : "Target page"}{" "}
                  <select
                    required={a.kind === "page.update"}
                    value={text(
                      a[a.kind === "page.create" ? "parentPageId" : "pageId"],
                    )}
                    onChange={(e) => {
                      if (a.kind === "page.create" && !e.target.value) {
                        const { parentPageId: _, ...rest } = a;
                        changeAction(i, rest);
                      } else
                        patch({
                          [a.kind === "page.create"
                            ? "parentPageId"
                            : "pageId"]: e.target.value,
                        });
                    }}
                  >
                    <option value="">
                      {a.kind === "page.create" ? "Top level" : "Choose page"}
                    </option>
                    {w.pages
                      .filter((p) => !p.deletedAt)
                      .map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.title}
                        </option>
                      ))}
                  </select>
                </label>
              )}
              {a.kind === "template.instantiate" && (
                <>
                  <label>
                    Saved template{" "}
                    <select
                      required
                      value={text(a.templateId)}
                      onChange={(e) => {
                        const t = choices.templates.find(
                          (t) => t.id === e.target.value,
                        );
                        const next = {
                          kind: a.kind,
                          templateId: t?.id ?? "",
                          templateHash: t?.hash ?? "",
                          title: a.title,
                          ...(t?.collectionId
                            ? { collectionId: t.collectionId }
                            : {}),
                        };
                        changeAction(i, next);
                      }}
                    >
                      <option value="">Choose capture</option>
                      {choices.templates.map((t) => (
                        <option key={t.id} value={t.id}>
                          {t.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  {!!a.templateId &&
                    !choices.templates.some(
                      (t) => t.id === a.templateId && t.hash === a.templateHash,
                    ) && (
                      <p role="status">
                        The saved capture is unavailable or changed. Select a
                        current template before previewing.
                      </p>
                    )}
                  <p>
                    Copies the captured body, compatible property defaults and
                    independent tasks. Changed or cross-database property
                    schemas require a compatible capture; no defaults are
                    silently omitted.
                  </p>
                  {choices.templatesTruncated && (
                    <p>Only the first 200 captures are available here.</p>
                  )}
                </>
              )}
              {(a.kind === "record.create" ||
                a.kind === "template.instantiate") && (
                <label>
                  Destination database{" "}
                  <select
                    required={a.kind === "record.create"}
                    value={text(a.collectionId)}
                    onChange={(e) => {
                      if (e.target.value)
                        patch({ collectionId: e.target.value });
                      else {
                        const { collectionId: _, ...rest } = a;
                        changeAction(i, rest);
                      }
                    }}
                  >
                    <option value="">
                      {a.kind === "record.create"
                        ? "Choose database"
                        : "Standalone page (body templates)"}
                    </option>
                    {w.collections.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.title}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {a.kind === "page.update" && (
                <label>
                  <input
                    type="checkbox"
                    checked={a.title !== undefined}
                    onChange={(e) => {
                      if (e.target.checked)
                        patch({
                          title:
                            w.pages.find((p) => p.id === a.pageId)?.title ?? "",
                        });
                      else {
                        const { title: _, ...rest } = a;
                        changeAction(i, rest);
                      }
                    }}
                  />{" "}
                  Change title
                </label>
              )}
              {([
                "page.create",
                "record.create",
                "template.instantiate",
              ].includes(text(a.kind)) ||
                (a.kind === "page.update" && a.title !== undefined)) && (
                <label>
                  Title{" "}
                  <input
                    required
                    maxLength={2000}
                    value={text(a.title)}
                    onChange={(e) => patch({ title: e.target.value })}
                  />
                </label>
              )}
              {a.kind === "page.update" && (
                <label>
                  Archive state{" "}
                  <select
                    value={
                      typeof a.archived === "boolean"
                        ? String(a.archived)
                        : "keep"
                    }
                    onChange={(e) => {
                      if (e.target.value === "keep") {
                        const { archived: _, ...rest } = a;
                        changeAction(i, rest);
                      } else patch({ archived: e.target.value === "true" });
                    }}
                  >
                    <option value="keep">Keep current</option>
                    <option value="false">Unarchive</option>
                    <option value="true">Archive</option>
                  </select>
                </label>
              )}
              {[
                "record.setValue",
                "record.clearValue",
                "task.setStatus",
              ].includes(text(a.kind)) && (
                <>
                  <RecordChoice
                    w={
                      a.kind === "task.setStatus"
                        ? {
                            ...w,
                            records: w.records.filter((r) =>
                              w.taskFacets.some((t) => t.recordId === r.id),
                            ),
                          }
                        : w
                    }
                    extra={a.kind === "task.setStatus" ? [] : extras}
                    label="Target record"
                    value={text(a.recordId)}
                    onChange={(recordId) =>
                      changeAction(i, { kind: a.kind, recordId })
                    }
                  />
                  {a.kind === "task.setStatus" ? (
                    <label>
                      Task status{" "}
                      <select
                        required
                        value={text(a.statusOptionId)}
                        onChange={(e) =>
                          patch({ statusOptionId: e.target.value })
                        }
                      >
                        <option value="">Choose status</option>
                        {status &&
                          options(status).map((o) => (
                            <option key={o.id} value={o.id}>
                              {o.label}
                            </option>
                          ))}
                      </select>
                    </label>
                  ) : (
                    <>
                      <label>
                        Property{" "}
                        <select
                          required
                          value={text(a.propertyId)}
                          onChange={(e) => {
                            const p = w.properties.find(
                              (p) => p.id === e.target.value,
                            );
                            patch({
                              propertyId: e.target.value,
                              ...(a.kind === "record.setValue" && p
                                ? {
                                    value: {
                                      type: p.type,
                                      schemaVersion: p.schemaVersion,
                                      value: defaultValue(p),
                                    },
                                  }
                                : {}),
                            });
                          }}
                        >
                          <option value="">Choose property</option>
                          {w.properties
                            .filter(
                              (p) =>
                                p.collectionId === record?.collectionId &&
                                (supportedTypes.has(p.type) ||
                                  p.id === a.propertyId),
                            )
                            .map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.name}
                              </option>
                            ))}
                        </select>
                      </label>
                      {property && a.kind === "record.setValue" && (
                        <TypedRuleValue
                          workspace={w}
                          property={property}
                          value={obj(a.value).value}
                          label="Property value"
                          onChange={(value) =>
                            patch({
                              value: {
                                type: property.type,
                                schemaVersion: property.schemaVersion,
                                value,
                              },
                            })
                          }
                        />
                      )}
                    </>
                  )}
                </>
              )}
              <button
                type="button"
                disabled={rule.actions.length === 1}
                onClick={() =>
                  update({ actions: rule.actions.filter((_, n) => i !== n) })
                }
              >
                Remove action {i + 1}
              </button>
            </fieldset>
          );
        })}
        <button
          type="button"
          disabled={rule.actions.length >= 32}
          onClick={() => {
            const name = freshName();
            update({
              actions: [
                ...rule.actions,
                {
                  kind: "page.create",
                  pageId: `$id:page-${name}`,
                  parentPageId: pageId,
                  title: rule.name,
                },
              ],
            });
          }}
        >
          Add action
        </button>
        <p>
          Saving creates a disabled rule version. Review its preview, then
          explicitly enable it. Existing run history stays available.
        </p>
        <button>Save disabled rule</button>
        {initial && (
          <button type="button" onClick={onCancel}>
            Cancel editing
          </button>
        )}
      </fieldset>
    </form>
  );
}
export function actionSummary(
  a: Record<string, unknown>,
  w: WorkspaceSnapshot,
): string {
  const page = w.pages.find((p) => p.id === a.pageId),
    record = w.records.find((r) => r.id === a.recordId),
    recordTitle =
      w.pages.find((p) => p.id === record?.pageId)?.title ?? "New record",
    property = w.properties.find((p) => p.id === a.propertyId);
  switch (a.kind) {
    case "page.create":
      return `Create page “${a.title}”`;
    case "page.update":
      return `Update “${page?.title ?? "page"}”${a.title ? ` to “${a.title}”` : ""}${a.archived !== undefined ? ` (${a.archived ? "archive" : "unarchive"})` : ""}`;
    case "record.create":
      return `Create “${a.title}” in ${w.collections.find((c) => c.id === a.collectionId)?.title ?? "database"}`;
    case "record.setValue":
      return `Set ${recordTitle} · ${property?.name ?? "property"} to ${displayValue(obj(a.value).value as D.RawValue, property, w.pages)}`;
    case "record.clearValue":
      return `Clear ${recordTitle} · ${property?.name ?? "property"}`;
    case "task.setStatus":
      return `Set ${recordTitle} task status to ${w.properties.flatMap((p) => options(p)).find((o) => o.id === a.statusOptionId)?.label ?? "selected status"}`;
    case "block.insert":
      return `Copy ${obj(a.block).type ?? "body"} block`;
    case "task.promote":
      return "Create an independent task";
    case "template.instantiate":
      return `Copy saved template as “${a.title}”`;
    default:
      return String(a.kind).replaceAll(".", " ");
  }
}
