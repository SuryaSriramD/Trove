import { useEffect, useMemo, useState } from "react";
import { dispatch, envelope, type D, type WorkspaceSnapshot } from "../api";
import { calendarDay, resizeDateRange } from "./dateRange";
import { groupRecords, valueOf, type RecordGroup } from "./model";
import {
  dayNumber,
  dayString,
  timelineAxis,
  timelineRange,
  type TimelineScale,
} from "./timelineModel";

type Structure = Record<
  string,
  { parentRecordId: string | null; dependencyRecordIds: string[] }
>;
type Row = { record?: D.Record; label?: string; depth: number };
function moveRange(value: D.RawValue, day: string): D.RawValue {
  const range = value as D.property_date_v1,
    delta = dayNumber(day) - dayNumber(calendarDay(value));
  const start = resizeDateRange(
    { ...range, end: null },
    "start",
    day,
  ) as D.property_date_v1;
  return range.end
    ? resizeDateRange(
        { ...range, start: start.start },
        "end",
        dayString(dayNumber(calendarDay(value, "end")) + delta),
      )
    : start;
}
export function Timeline({
  records,
  view,
  workspace: w,
  properties,
  busy,
  openPage,
  onChange,
  onError,
}: {
  records: D.Record[];
  view: D.View;
  workspace: WorkspaceSnapshot;
  properties: D.PropertyDefinition[];
  busy: boolean;
  openPage: (id: string) => void;
  onChange: (
    record: D.Record,
    property: D.PropertyDefinition,
    value: D.RawValue,
  ) => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const cfg = view.config as {
    datePropertyId: string;
    scale: TimelineScale;
    showDependencies?: boolean;
  };
  const property = properties.find((p) => p.id === cfg.datePropertyId);
  const dates = useMemo(
    () =>
      records
        .map((r) => calendarDay(valueOf(r, cfg.datePropertyId)))
        .filter(Boolean),
    [records, cfg.datePropertyId],
  );
  const first = dates.length
    ? dates.reduce((a, b) => (a < b ? a : b))
    : new Date().toISOString().slice(0, 10);
  const [showControls, setShowControls] = useState(false);
  const [selectedAnchor, setAnchor] = useState<{ view: string; day: string }>();
  const anchor = selectedAnchor?.view === view.id ? selectedAnchor.day : first;
  const axis = timelineAxis(anchor, cfg.scale);
  const [structure, setStructure] = useState<Structure>({}),
    [structureError, setStructureError] = useState("");
  useEffect(() => {
    let live = true;
    setStructure({});
    setStructureError("");
    void dispatch<{ structure: Structure }>("databaseStructure", {
      ...envelope(w),
      collectionId: view.collectionId,
    })
      .then((result) => {
        if (live) setStructure(result.structure);
      })
      .catch((error) => {
        if (live)
          setStructureError(
            error instanceof Error ? error.message : String(error),
          );
      });
    return () => {
      live = false;
    };
  }, [w.workspaceId, w.snapshotCursor, view.collectionId]);
  const pageTitles = useMemo(
    () => new Map(w.pages.map((page) => [page.id, page.title])),
    [w.pages],
  );
  const title = (record: D.Record) =>
    pageTitles.get(record.pageId) ?? "Untitled";
  const rows = useMemo(() => {
    const out: Row[] = [];
    const append = (groups: RecordGroup[], depth = 0) => {
      for (const group of groups) {
        if (group.label) out.push({ label: group.label, depth });
        if (group.children.length) append(group.children, depth + 1);
        else for (const record of group.records) out.push({ record, depth });
      }
    };
    append(
      groupRecords(
        records,
        properties.find((p) => p.id === view.query.groupByPropertyId),
        properties.find((p) => p.id === view.query.subgroupByPropertyId),
        w.pages,
      ),
    );
    return out;
  }, [records, properties, view.query, w.pages]);
  const positions = new Map<
    string,
    { y: number; range: ReturnType<typeof timelineRange> }
  >();
  let height = 0;
  for (const row of rows) {
    if (row.record) {
      const value = valueOf(row.record, cfg.datePropertyId),
        start = calendarDay(value),
        end = calendarDay(value, "end");
      positions.set(row.record.id, {
        y: height + 27,
        range: start ? timelineRange(start, end, axis) : null,
      });
      height += 54;
    } else height += 30;
  }
  const edges: { key: string; path: string; label: string }[] = [];
  let edgeCount = 0;
  if (cfg.showDependencies !== false)
    for (const record of records) {
      const to = positions.get(record.id);
      if (!to?.range) continue;
      for (const id of structure[record.id]?.dependencyRecordIds ?? []) {
        const from = positions.get(id);
        if (!from?.range) continue;
        edgeCount++;
        if (edges.length >= 5001) continue;
        const x1 = from.range.left + from.range.width,
          x2 = to.range.left;
        edges.push({
          key: record.id + id,
          path: `M${x1},${from.y} H${x1 + 12} V${to.y} H${x2}`,
          label: `Dependency leads to ${title(record)}`,
        });
      }
    }
  const save = async (
    record: D.Record,
    boundary: "start" | "end" | "move",
    day: string,
  ) => {
    if (!property) return;
    try {
      const value = valueOf(record, cfg.datePropertyId);
      await onChange(
        record,
        property,
        boundary === "move"
          ? moveRange(value, day)
          : resizeDateRange(value, boundary, day),
      );
    } catch (error) {
      onError(error);
    }
  };
  return (
    <section className="db-timeline" aria-label="Database timeline">
      <div className="db-timeline-toolbar">
        <button
          onClick={() => setAnchor({ view: view.id, day: axis.step(-1) })}
        >
          Earlier range
        </button>
        <label>
          Timeline window starts
          <input
            type="date"
            value={anchor}
            onChange={(event) => {
              if (event.target.value)
                setAnchor({ view: view.id, day: event.target.value });
            }}
          />
        </label>
        <button onClick={() => setAnchor({ view: view.id, day: axis.step(1) })}>
          Later range
        </button>
        <button onClick={() => setAnchor({ view: view.id, day: first })}>
          First dated record
        </button>
        <span>
          {cfg.scale} scale · {dayString(axis.start)} to{" "}
          {dayString(axis.end - 1)}
        </span>
      </div>
      <p>
        Positions use each record’s stored calendar date and time zone. Drag a
        range or its edge; exact start and end controls remain available below.
      </p>
      {structureError && (
        <p role="alert">Dependencies unavailable: {structureError}</p>
      )}
      {edgeCount > 5000 && (
        <p role="status">
          More than 5,000 dependency arrows match. Narrow this view to display
          the overlay; dependency lists remain complete.
        </p>
      )}
      <div className="db-timeline-scroll">
        <div style={{ width: 260 + axis.width }}>
          <div
            className="db-timeline-axis"
            style={{ gridTemplateColumns: `260px ${axis.width}px` }}
          >
            <strong>Record · saved order</strong>
            <div>
              {axis.ticks.slice(0, -1).map((tick) => (
                <span
                  key={tick.day}
                  style={{
                    left:
                      ((tick.day - axis.start) / (axis.end - axis.start)) *
                      axis.width,
                  }}
                >
                  {tick.label}
                </span>
              ))}
            </div>
          </div>
          <div className="db-timeline-content" style={{ height }}>
            {cfg.showDependencies !== false && edgeCount <= 5000 && (
              <svg
                className="db-timeline-edges"
                style={{ left: 260 }}
                width={axis.width}
                height={height}
                aria-label="Dependency overlay"
              >
                <defs>
                  <marker
                    id={`arrow-${view.id}`}
                    viewBox="0 0 10 10"
                    refX="8"
                    refY="5"
                    markerWidth="5"
                    markerHeight="5"
                    orient="auto"
                  >
                    <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
                  </marker>
                </defs>
                {edges.map((edge) => (
                  <path
                    key={edge.key}
                    d={edge.path}
                    markerEnd={`url(#arrow-${view.id})`}
                  >
                    <title>{edge.label}</title>
                  </path>
                ))}
              </svg>
            )}
            {rows.map((row, index) =>
              row.record ? (
                (() => {
                  const record = row.record,
                    value = valueOf(record, cfg.datePropertyId),
                    start = calendarDay(value),
                    end = calendarDay(value, "end"),
                    range = positions.get(record.id)?.range;
                  return (
                    <div
                      className="db-timeline-data-row"
                      key={record.id}
                      data-timeline-record={record.id}
                      style={{ gridTemplateColumns: `260px ${axis.width}px` }}
                    >
                      <button
                        className="db-timeline-name"
                        style={{ paddingLeft: 12 + row.depth * 14 }}
                        onClick={() => openPage(record.pageId)}
                      >
                        {title(record)}
                      </button>
                      <div
                        className="db-timeline-lane"
                        onDragOver={(event) => event.preventDefault()}
                        onDrop={(event) => {
                          event.preventDefault();
                          try {
                            const drag = JSON.parse(
                              event.dataTransfer.getData(
                                "application/x-trove-timeline",
                              ),
                            );
                            const source = records.find(
                              (r) => r.id === drag.recordId,
                            );
                            if (!source || busy) return;
                            const box =
                                event.currentTarget.getBoundingClientRect(),
                              day = dayString(
                                Math.floor(
                                  axis.start +
                                    (Math.max(
                                      0,
                                      Math.min(
                                        axis.width,
                                        event.clientX - box.left,
                                      ),
                                    ) /
                                      axis.width) *
                                      (axis.end - axis.start),
                                ),
                              );
                            void save(source, drag.boundary, day);
                          } catch (error) {
                            onError(error);
                          }
                        }}
                      >
                        {range ? (
                          <div
                            className="db-timeline-range"
                            draggable={!busy}
                            onDragStart={(event) =>
                              event.dataTransfer.setData(
                                "application/x-trove-timeline",
                                JSON.stringify({
                                  recordId: record.id,
                                  boundary: "move",
                                }),
                              )
                            }
                            style={{ left: range.left, width: range.width }}
                            title={`${title(record)} · ${start}${end ? " to " + end : ""}`}
                          >
                            <button
                              className="db-timeline-edge"
                              aria-label={`Resize start ${title(record)}`}
                              draggable={!busy}
                              onDragStart={(event) => {
                                event.stopPropagation();
                                event.dataTransfer.setData(
                                  "application/x-trove-timeline",
                                  JSON.stringify({
                                    recordId: record.id,
                                    boundary: "start",
                                  }),
                                );
                              }}
                            >
                              ↔
                            </button>
                            <span>
                              {range.before ? "‹ " : ""}
                              {title(record)}
                              {range.after ? " ›" : ""}
                            </span>
                            <button
                              className="db-timeline-edge"
                              aria-label={`Resize end ${title(record)}`}
                              draggable={!busy}
                              onDragStart={(event) => {
                                event.stopPropagation();
                                event.dataTransfer.setData(
                                  "application/x-trove-timeline",
                                  JSON.stringify({
                                    recordId: record.id,
                                    boundary: "end",
                                  }),
                                );
                              }}
                            >
                              ↔
                            </button>
                          </div>
                        ) : (
                          <span className="db-timeline-outside">
                            {start ? "Outside displayed window" : "Unscheduled"}
                          </span>
                        )}
                      </div>
                    </div>
                  );
                })()
              ) : (
                <div
                  className="db-timeline-group"
                  key={`group-${index}`}
                  style={{ paddingLeft: 12 + row.depth * 14 }}
                >
                  {row.label}
                </div>
              ),
            )}
          </div>
        </div>
      </div>
      <details onToggle={(event) => setShowControls(event.currentTarget.open)}>
        <summary>Edit date ranges and inspect dependencies</summary>
        {showControls &&
          records.map((record) => {
            const value = valueOf(record, cfg.datePropertyId),
              deps = structure[record.id]?.dependencyRecordIds ?? [];
            return (
              <div
                className="db-timeline-edit"
                key={`${record.id}:${record.revision}`}
              >
                <strong>{title(record)}</strong>
                <label>
                  Start
                  <input
                    aria-label={`Timeline start ${title(record)}`}
                    type="date"
                    defaultValue={calendarDay(value)}
                    disabled={busy || !value}
                    onBlur={(event) => {
                      if (
                        event.target.value &&
                        event.target.value !== calendarDay(value)
                      )
                        void save(record, "start", event.target.value);
                    }}
                  />
                </label>
                <label>
                  End
                  <input
                    aria-label={`Timeline end ${title(record)}`}
                    type="date"
                    defaultValue={calendarDay(value, "end")}
                    disabled={busy || !value}
                    onBlur={(event) => {
                      if (
                        event.target.value &&
                        event.target.value !== calendarDay(value, "end")
                      )
                        void save(record, "end", event.target.value);
                    }}
                  />
                </label>
                <span>
                  {(value as { timezone?: string } | null)?.timezone ??
                    "No date"}
                </span>
                <span>
                  Depends on:{" "}
                  {structureError
                    ? "unavailable"
                    : deps.length
                      ? deps
                          .map((id) => {
                            const target = w.records.find((r) => r.id === id);
                            return target
                              ? title(target)
                              : "Record outside this view";
                          })
                          .join(", ")
                      : "none"}
                </span>
              </div>
            );
          })}
      </details>
    </section>
  );
}
