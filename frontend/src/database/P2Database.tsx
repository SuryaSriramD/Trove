import { useEffect, useRef, useState } from "react";
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
import { guard, uniqueGuards } from "./service";
import { displayValue, options } from "./model";
import {
  chartRatios,
  formFieldEmpty,
  p2FieldTypes,
  type P2Button,
  type P2Chart,
  type P2Config,
} from "./P2Model";
import "./P2Database.css";

type Props = {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  records: D.Record[];
  openPage: (id: string) => void;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
};
export function P2DatabasePanel({
  workspace: w,
  collection,
  openPage,
  onCommitted,
  onError,
}: Props) {
  const [config, setConfig] = useState<P2Config>();
  const [chart, setChart] = useState<P2Chart>();
  const [busy, setBusy] = useState(false);
  const [chartError, setChartError] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [title, setTitle] = useState("");
  const [values, setValues] = useState<Record<string, D.RawValue>>({});
  const [buttonProperty, setButtonProperty] = useState("");
  const [buttonLabel, setButtonLabel] = useState("");
  const [buttonValue, setButtonValue] = useState<D.RawValue>(null);
  const [confirmation, setConfirmation] = useState(true);
  const [configDirty, setConfigDirty] = useState(false);
  const dirty = useRef(false);
  const baseline = useRef<P2Config | undefined>(undefined);
  const latest = useRef<{ config: P2Config; revision: string } | undefined>(
    undefined,
  );
  const [configRevision, setConfigRevision] = useState(collection.revision);
  const [configConflict, setConfigConflict] = useState(false);
  const [configError, setConfigError] = useState("");
  const [reload, setReload] = useState(0);
  const formRevisions = useRef<Record<string, string>>({});
  const properties = w.properties.filter(
    (p) => p.collectionId === collection.id,
  );
  const fields = properties.filter((p) => p2FieldTypes.includes(p.type));
  function resetForm() {
    setValues(
      Object.fromEntries(
        fields.filter((p) => p.type === "checkbox").map((p) => [p.id, false]),
      ),
    );
    formRevisions.current = Object.fromEntries(
      fields.map((p) => [p.id, p.revision]),
    );
    setTitle("");
  }
  function editConfig(next: P2Config) {
    dirty.current = true;
    setConfigDirty(true);
    setConfig(next);
  }
  useEffect(() => {
    resetForm();
    setConfig(undefined);
    dirty.current = false;
    setConfigDirty(false);
    setConfigConflict(false);
    baseline.current = undefined;
    latest.current = undefined;
    setButtonProperty("");
    setButtonLabel("");
    setButtonValue(null);
  }, [w.workspaceId, collection.id]);
  useEffect(() => {
    let active = true;
    setConfigError("");
    dispatch<{ config: P2Config }>("databaseP2", {
      ...envelope(w),
      collectionId: collection.id,
    })
      .then((settings) => {
        if (!active) return;
        latest.current = {
          config: settings.config,
          revision: collection.revision,
        };
        if (!dirty.current) {
          setConfig(settings.config);
          baseline.current = settings.config;
          setConfigRevision(collection.revision);
          setConfigConflict(false);
        } else if (
          JSON.stringify(settings.config) === JSON.stringify(baseline.current)
        ) {
          setConfigRevision(collection.revision);
          setConfigConflict(false);
        } else setConfigConflict(true);
      })
      .catch((e) => {
        if (active) setConfigError(String(e));
      });
    return () => {
      active = false;
    };
  }, [
    w.workspaceId,
    w.scopeGrantId,
    collection.id,
    collection.revision,
    reload,
  ]);
  useEffect(() => {
    let active = true;
    setChart(undefined);
    setChartError("");
    if (expanded)
      dispatch<P2Chart>("databaseChart", {
        ...envelope(w),
        collectionId: collection.id,
      })
        .then((aggregate) => {
          if (active) setChart(aggregate);
        })
        .catch((e) => {
          if (active) setChartError(String(e.message ?? e));
        });
    return () => {
      active = false;
    };
  }, [
    w.workspaceId,
    w.scopeGrantId,
    collection.id,
    collection.revision,
    w.snapshotCursor,
    expanded,
  ]);
  const run = async (action: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    try {
      await action();
      await onCommitted();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };
  async function save(next: P2Config) {
    if (configConflict)
      throw Error(
        "Saved settings changed while you were editing. Discard settings changes and review the current configuration.",
      );
    const result = await batch(
      w,
      [
        {
          kind: "database.p2Configure",
          collectionId: collection.id,
          config: next,
        } as S.Command,
      ],
      [guard("collection", collection.id, configRevision)],
    );
    dirty.current = false;
    setConfigDirty(false);
    baseline.current = next;
    setConfig(next);
    const revision = result.revisions.find(
      (r) =>
        r.target.entityType === "collection" &&
        r.target.entityId === collection.id,
    )?.after;
    if (revision) setConfigRevision(revision);
    latest.current = { config: next, revision: revision ?? configRevision };
  }
  if (!config)
    return configError ? (
      <p role="alert">
        {configError}{" "}
        <button onClick={() => setReload((n) => n + 1)}>
          Retry database settings
        </button>
      </p>
    ) : (
      <p role="status">Loading local database enhancements…</p>
    );
  const buttonField = fields.find((p) => p.id === buttonProperty);
  return (
    <section className="p2-database" aria-label="Local database enhancements">
      <details onToggle={(event) => setExpanded(event.currentTarget.open)}>
        <summary>Charts, local forms and row buttons</summary>
        {configError && (
          <p role="alert">
            {configError}{" "}
            <button onClick={() => setReload((n) => n + 1)}>
              Retry database settings
            </button>
          </p>
        )}
        {configConflict && (
          <p role="alert">
            Saved settings changed while you were editing. Your draft is
            retained. Discard settings changes to load the current
            configuration.
          </p>
        )}
        {configDirty && (
          <p role="status">
            Settings have unsaved changes. Save or discard them before
            submitting the form.{" "}
            <button
              disabled={busy}
              onClick={() => {
                if (!latest.current) return;
                setConfig(latest.current.config);
                baseline.current = latest.current.config;
                setConfigRevision(latest.current.revision);
                dirty.current = false;
                setConfigDirty(false);
                setConfigConflict(false);
              }}
            >
              Discard settings changes
            </button>
          </p>
        )}
        <fieldset disabled={busy}>
          <div className="p2-grid">
            <section aria-label="Chart configuration">
              <h3>Local chart</h3>
              <label>
                Group by
                <select
                  aria-label="Chart group"
                  value={config.chart.groupPropertyId ?? ""}
                  onChange={(e) =>
                    editConfig({
                      ...config,
                      chart: {
                        ...config.chart,
                        groupPropertyId: e.target.value || null,
                      },
                    })
                  }
                >
                  <option value="">All records</option>
                  {properties
                    .filter((p) =>
                      [
                        "title",
                        "text",
                        "select",
                        "status",
                        "checkbox",
                        "date",
                      ].includes(p.type),
                    )
                    .map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                </select>
              </label>
              <label>
                Calculate
                <select
                  aria-label="Chart aggregation"
                  value={config.chart.aggregation}
                  onChange={(e) =>
                    editConfig({
                      ...config,
                      chart: {
                        ...config.chart,
                        aggregation: e.target
                          .value as P2Config["chart"]["aggregation"],
                      },
                    })
                  }
                >
                  {["count", "sum", "average", "min", "max"].map((a) => (
                    <option key={a}>{a}</option>
                  ))}
                </select>
              </label>
              {config.chart.aggregation !== "count" && (
                <label>
                  Value
                  <select
                    aria-label="Chart numeric property"
                    value={config.chart.valuePropertyId ?? ""}
                    onChange={(e) =>
                      editConfig({
                        ...config,
                        chart: {
                          ...config.chart,
                          valuePropertyId: e.target.value || null,
                        },
                      })
                    }
                  >
                    <option value="">Choose numeric property</option>
                    {properties
                      .filter((p) =>
                        ["number", "formula", "rollup"].includes(p.type),
                      )
                      .map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                  </select>
                </label>
              )}
              <label>
                Display
                <select
                  aria-label="Chart display"
                  value={config.chart.visualization}
                  onChange={(e) =>
                    editConfig({
                      ...config,
                      chart: {
                        ...config.chart,
                        visualization: e.target.value as "bar" | "donut",
                      },
                    })
                  }
                >
                  <option value="bar">Bars</option>
                  <option value="donut">Donut</option>
                </select>
              </label>
              <button
                disabled={busy}
                onClick={() => void run(() => save(config))}
              >
                Save chart and settings
              </button>
              {chartError && <p role="alert">{chartError}</p>}
              {chart && (
                <P2ChartView chart={chart} workspace={w} openPage={openPage} />
              )}
            </section>
            <section aria-label="Local form configuration">
              <h3>Local form</h3>
              <label>
                <input
                  type="checkbox"
                  checked={config.form.enabled}
                  onChange={(e) =>
                    editConfig({
                      ...config,
                      form: { ...config.form, enabled: e.target.checked },
                    })
                  }
                />
                Enable a local submission form
              </label>
              <fieldset>
                <legend>Required fields (title is always required)</legend>
                {fields.map((p) => (
                  <label key={p.id}>
                    <input
                      type="checkbox"
                      checked={config.form.requiredPropertyIds.includes(p.id)}
                      onChange={(e) =>
                        editConfig({
                          ...config,
                          form: {
                            ...config.form,
                            requiredPropertyIds: e.target.checked
                              ? [...config.form.requiredPropertyIds, p.id]
                              : config.form.requiredPropertyIds.filter(
                                  (id) => id !== p.id,
                                ),
                          },
                        })
                      }
                    />
                    {p.name}
                  </label>
                ))}
              </fieldset>
              <button
                disabled={busy}
                onClick={() => void run(() => save(config))}
              >
                Save form settings
              </button>
              {config.form.enabled && (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void run(async () => {
                      if (configDirty)
                        throw Error(
                          "Save or discard settings changes before submitting the form.",
                        );
                      for (const [id, value] of Object.entries(values)) {
                        if (value == null) continue;
                        const property = fields.find((p) => p.id === id);
                        if (
                          !property ||
                          formRevisions.current[id] !== property.revision
                        )
                          throw Error(
                            `${property?.name ?? "A form field"} changed while this draft was being edited. Review that field again or discard the form draft.`,
                          );
                      }
                      for (const id of config.form.requiredPropertyIds)
                        if (formFieldEmpty(values[id]))
                          throw Error(
                            `${fields.find((p) => p.id === id)?.name ?? "Required field"} is required.`,
                          );
                      const [recordId, pageId] = await ids(w, 2);
                      const entries = Object.entries(values).filter(
                        ([, v]) => v != null,
                      );
                      await batch(
                        w,
                        [
                          {
                            kind: "database.formSubmit",
                            collectionId: collection.id,
                            recordId,
                            pageId,
                            title: title.trim(),
                            values: entries.map(([propertyId, value]) => {
                              const p = fields.find(
                                (p) => p.id === propertyId,
                              )!;
                              return {
                                propertyId,
                                value: {
                                  type: p.type,
                                  schemaVersion: p.schemaVersion,
                                  value,
                                },
                              };
                            }),
                          } as S.Command,
                        ],
                        [
                          guard(
                            "collection",
                            collection.id,
                            collection.revision,
                          ),
                          guard("record", recordId, "0"),
                          guard("page", pageId, "0"),
                          ...entries.map(([id]) =>
                            guard(
                              "property",
                              id,
                              fields.find((p) => p.id === id)!.revision,
                            ),
                          ),
                        ],
                      );
                      resetForm();
                    });
                  }}
                >
                  <p>
                    Creates a record locally. Save changed form settings before
                    submitting.
                  </p>
                  <label>
                    Title
                    <input
                      aria-label="Form title"
                      required
                      maxLength={512}
                      value={title}
                      onChange={(e) => setTitle(e.target.value)}
                    />
                  </label>
                  {fields.map((p) => (
                    <label key={p.id}>
                      {p.name}
                      {config.form.requiredPropertyIds.includes(p.id)
                        ? " *"
                        : ""}
                      <P2Field
                        property={p}
                        value={values[p.id] ?? null}
                        onChange={(v) => {
                          formRevisions.current[p.id] = p.revision;
                          setValues((old) => ({ ...old, [p.id]: v }));
                        }}
                        label={`Form ${p.name}`}
                      />
                    </label>
                  ))}
                  <button
                    type="submit"
                    disabled={busy || configDirty || !title.trim()}
                  >
                    Submit local form
                  </button>
                  <button type="button" disabled={busy} onClick={resetForm}>
                    Discard form draft
                  </button>
                </form>
              )}
            </section>
            <section aria-label="Row button configuration">
              <h3>Row buttons</h3>
              <p>Set one field on the selected row.</p>
              {config.buttons.map((b) => (
                <div key={b.id}>
                  {b.label} →{" "}
                  {properties.find((p) => p.id === b.propertyId)?.name}:{" "}
                  {displayValue(
                    b.value.value,
                    properties.find((p) => p.id === b.propertyId),
                  )}{" "}
                  {b.confirmationRequired ? "(confirmation)" : ""}
                  <button
                    disabled={busy}
                    aria-label={`Remove ${b.label} button`}
                    onClick={() =>
                      void run(() =>
                        save({
                          ...config,
                          buttons: config.buttons.filter((v) => v.id !== b.id),
                        }),
                      )
                    }
                  >
                    Remove
                  </button>
                </div>
              ))}
              <label>
                Label
                <input
                  aria-label="Row button label"
                  value={buttonLabel}
                  maxLength={120}
                  onChange={(e) => setButtonLabel(e.target.value)}
                />
              </label>
              <label>
                Property
                <select
                  aria-label="Row button property"
                  value={buttonProperty}
                  onChange={(e) => {
                    setButtonProperty(e.target.value);
                    setButtonValue(
                      fields.find((p) => p.id === e.target.value)?.type ===
                        "checkbox"
                        ? false
                        : null,
                    );
                  }}
                >
                  <option value="">Choose field</option>
                  {fields.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </label>
              {buttonField && (
                <P2Field
                  key={buttonField.id}
                  property={buttonField}
                  value={buttonValue}
                  onChange={setButtonValue}
                  label="Row button value"
                />
              )}
              <label>
                <input
                  type="checkbox"
                  checked={confirmation}
                  onChange={(e) => setConfirmation(e.target.checked)}
                />
                Ask for confirmation each time
              </label>
              <button
                disabled={
                  busy ||
                  !buttonField ||
                  !buttonLabel.trim() ||
                  config.buttons.length >= 16
                }
                onClick={() =>
                  void run(async () => {
                    if (!buttonField) return;
                    const [id] = await ids(w, 1);
                    await save({
                      ...config,
                      buttons: [
                        ...config.buttons,
                        {
                          id,
                          label: buttonLabel.trim(),
                          propertyId: buttonField.id,
                          value: {
                            type: buttonField.type,
                            schemaVersion: buttonField.schemaVersion,
                            value: buttonValue,
                          },
                          confirmationRequired: confirmation,
                        },
                      ],
                    });
                    setButtonLabel("");
                  })
                }
              >
                Add row button
              </button>
            </section>
          </div>
        </fieldset>
      </details>
    </section>
  );
}
export function P2RowButtons({
  workspace: w,
  collection,
  record,
  buttons,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  record: D.Record;
  buttons: P2Button[];
  onCommitted: () => Promise<void>;
  onError: (e: unknown) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [review, setReview] = useState<
    P2Button & { collectionRevision: string; recordRevision: string }
  >();
  useEffect(() => setReview(undefined), [collection.revision, record.revision]);
  async function run(
    button: P2Button,
    confirmed: boolean,
    reviewed?: { collectionRevision: string; recordRevision: string },
  ) {
    if (busy) return;
    setBusy(true);
    try {
      const page = w.pages.find((p) => p.id === record.pageId),
        property = w.properties.find((p) => p.id === button.propertyId);
      if (!page || !property) throw Error("The row action is unavailable.");
      await batch(
        w,
        [
          {
            kind: "database.rowButtonRun",
            collectionId: collection.id,
            recordId: record.id,
            buttonId: button.id,
            confirmed,
          } as S.Command,
        ],
        uniqueGuards([
          ...pageGuards(w, page),
          guard("property", property.id, property.revision),
          guard(
            "collection",
            collection.id,
            reviewed?.collectionRevision ?? collection.revision,
          ),
          guard(
            "record",
            record.id,
            reviewed?.recordRevision ?? record.revision,
          ),
        ]),
      );
      setReview(undefined);
      await onCommitted();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      {buttons.map((b) => (
        <button
          key={b.id}
          disabled={busy}
          onClick={() =>
            b.confirmationRequired
              ? setReview({
                  ...b,
                  collectionRevision: collection.revision,
                  recordRevision: record.revision,
                })
              : void run(b, false)
          }
        >
          {b.label}
        </button>
      ))}
      {review && (
        <span
          className="p2-row-review"
          role="alertdialog"
          aria-label="Confirm row action"
        >
          <span>
            Set {w.properties.find((p) => p.id === review.propertyId)?.name} to{" "}
            {displayValue(
              review.value.value,
              w.properties.find((p) => p.id === review.propertyId),
            )}{" "}
            on “{w.pages.find((p) => p.id === record.pageId)?.title}”?
          </span>
          <button
            disabled={busy}
            onClick={() => void run(review, true, review)}
          >
            Confirm {review.label}
          </button>
          <button disabled={busy} onClick={() => setReview(undefined)}>
            Cancel
          </button>
        </span>
      )}
    </>
  );
}
export function P2ChartView({
  chart,
  workspace: w,
  openPage,
}: {
  chart: P2Chart;
  workspace: WorkspaceSnapshot;
  openPage: (id: string) => void;
}) {
  let ratios: number[] = [],
    error = "";
  try {
    ratios = chartRatios(
      chart.groups.map((g) => g.decimal),
      chart.config.visualization,
    );
  } catch (e) {
    error = (e as Error).message;
  }
  let start = 0;
  return (
    <figure className="p2-chart">
      <figcaption>
        {chart.config.aggregation} · {chart.recordCount} records
      </figcaption>
      {error ? (
        <p role="status">{error}</p>
      ) : chart.config.visualization === "donut" ? (
        <svg
          viewBox="0 0 160 160"
          role="img"
          aria-label="Donut chart; exact values in table below"
        >
          {ratios.map((ratio, i) => {
            const offset = start;
            start += ratio * 100;
            return (
              <circle
                key={i}
                cx="80"
                cy="80"
                r="55"
                pathLength="100"
                fill="none"
                stroke={`hsl(${(i * 67) % 360} 58% 48%)`}
                strokeWidth="28"
                strokeDasharray={`${ratio * 100} ${100 - ratio * 100}`}
                strokeDashoffset={-offset}
                transform="rotate(-90 80 80)"
              />
            );
          })}
        </svg>
      ) : (
        <svg
          viewBox={`0 0 500 ${Math.max(40, chart.groups.length * 28)}`}
          role="img"
          aria-label="Bar chart; exact values in table below"
        >
          {ratios.map((ratio, i) => (
            <g key={i}>
              <text x="0" y={i * 28 + 18} fontSize="12" fill="currentColor">
                {chart.groups[i].label.slice(0, 19)}
              </text>
              <line
                x1="330"
                x2="330"
                y1={i * 28}
                y2={i * 28 + 25}
                stroke="currentColor"
              />
              <rect
                x={ratio < 0 ? 330 + ratio * 155 : 330}
                y={i * 28 + 3}
                width={Math.abs(ratio) * 155}
                height="19"
                fill="var(--purple)"
              />
            </g>
          ))}
        </svg>
      )}
      <table>
        <thead>
          <tr>
            <th>Group</th>
            <th>Exact displayed value</th>
            <th>Source rows</th>
          </tr>
        </thead>
        <tbody>
          {chart.groups.map((g) => (
            <tr key={g.key}>
              <td>{g.label}</td>
              <td title={g.exactFraction ?? "No numeric values"}>
                {g.decimal ?? "No value"}
              </td>
              <td>
                <details>
                  <summary>{g.recordIds.length} records</summary>
                  {g.recordIds.map((id) => {
                    const r = w.records.find((r) => r.id === id),
                      p = w.pages.find((p) => p.id === r?.pageId);
                    return p ? (
                      <button key={id} onClick={() => openPage(p.id)}>
                        {p.title}
                      </button>
                    ) : (
                      <span key={id}>Unavailable record</span>
                    );
                  })}
                </details>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p>
        Current collection, including rows outside the active view. Empty
        numeric values are excluded. Averages display 12 decimal places; hover
        for the exact fraction.
      </p>
    </figure>
  );
}
function P2Field({
  property: p,
  value,
  onChange,
  label,
}: {
  property: D.PropertyDefinition;
  value: D.RawValue;
  onChange: (v: D.RawValue) => void;
  label: string;
}) {
  if (p.type === "checkbox")
    return (
      <input
        type="checkbox"
        aria-label={label}
        checked={value === true}
        onChange={(e) => onChange(e.target.checked)}
      />
    );
  if (["select", "status", "multi_select"].includes(p.type))
    return (
      <select
        aria-label={label}
        multiple={p.type === "multi_select"}
        value={
          p.type === "multi_select"
            ? ((value as string[]) ?? [])
            : typeof value === "string"
              ? value
              : ""
        }
        onChange={(e) =>
          onChange(
            p.type === "multi_select"
              ? Array.from(e.target.selectedOptions, (o) => o.value)
              : e.target.value || null,
          )
        }
      >
        {p.type !== "multi_select" && <option value="">Choose</option>}
        {options(p).map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </select>
    );
  if (p.type === "date")
    return (
      <input
        type="date"
        aria-label={label}
        value={(value as { start?: string })?.start ?? ""}
        onChange={(e) =>
          onChange(
            e.target.value
              ? {
                  start: e.target.value,
                  end: null,
                  hasTime: false,
                  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
                }
              : null,
          )
        }
      />
    );
  const text =
    p.type === "number"
      ? ((value as { decimal?: string })?.decimal ?? "")
      : p.type === "text"
        ? displayValue(value)
        : typeof value === "string"
          ? value
          : "";
  return (
    <input
      aria-label={label}
      type={p.type === "email" ? "email" : p.type === "url" ? "url" : "text"}
      inputMode={p.type === "number" ? "decimal" : undefined}
      value={text}
      onChange={(e) =>
        onChange(
          p.type === "number"
            ? e.target.value
              ? {
                  decimal: e.target.value,
                  format: (p.config as { format?: string }).format ?? "decimal",
                }
              : null
            : p.type === "text"
              ? [{ text: e.target.value }]
              : e.target.value,
        )
      }
    />
  );
}
