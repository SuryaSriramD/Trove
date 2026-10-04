import { useEffect, useState } from "react";
import {
  batch,
  fileURL,
  dispatch,
  envelope,
  ids,
  pageGuards,
  type D,
  type WorkspaceSnapshot,
} from "../api";
import { guard, saveView, uniqueGuards } from "./service";
import { displayValue, valueOf } from "./model";

export type DerivedCell = {
  propertyId: string;
  status: "computed" | "captured" | "error";
  value: D.RawValue;
  originalExpression?: string;
  translatedExpression?: string;
  importedCachedValue?: D.RawValue;
  diagnostic?: string;
  computedAt?: string;
  timezone: string;
};
type DatabaseState = {
  locked: boolean;
  values: Record<string, DerivedCell[]>;
  structure: Record<
    string,
    { parentRecordId: string | null; dependencyRecordIds: string[] }
  >;
};
export function DerivedCellView({ value }: { value: D.RawValue }) {
  const cell = (value as { raw?: DerivedCell } | null)?.raw;
  if (!cell) return <span>{displayValue(value)}</span>;
  return (
    <details className="db-derived">
      <summary>
        {cell.status === "computed"
          ? displayValue(cell.value)
          : cell.status === "captured"
            ? `Captured: ${displayValue(cell.importedCachedValue)} · not recalculated`
            : "Calculation unavailable"}
      </summary>
      {cell.diagnostic && <p role="status">{cell.diagnostic}</p>}
      {cell.originalExpression && (
        <p>
          Original expression: <code>{cell.originalExpression}</code>
        </p>
      )}
      {cell.translatedExpression && (
        <p>
          Translated expression: <code>{cell.translatedExpression}</code>
        </p>
      )}
      {cell.importedCachedValue !== undefined && (
        <p>
          Imported cached value: {displayValue(cell.importedCachedValue)}{" "}
          (captured evidence)
        </p>
      )}
      {cell.computedAt && (
        <small>
          Calculated {cell.computedAt} · {cell.timezone}
        </small>
      )}
    </details>
  );
}
export function Gallery({
  records,
  view,
  properties,
  pages,
  files,
  openPage,
}: {
  records: D.Record[];
  view: D.View;
  properties: D.PropertyDefinition[];
  pages: D.Page[];
  files: D.File[];
  openPage: (id: string) => void;
}) {
  const cfg = view.config as {
    cardSize: string;
    coverPropertyId: string | null;
  };
  return (
    <div className={`db-gallery db-gallery-${cfg.cardSize}`}>
      {records.map((r) => (
        <article key={r.id} className="db-gallery-card">
          {cfg.coverPropertyId &&
            (() => {
              const values = valueOf(r, cfg.coverPropertyId) as
                { fileId: string }[] | null;
              const cover = values?.find((v) =>
                files
                  .find((f) => f.id === v.fileId)
                  ?.mediaType.startsWith("image/"),
              );
              return cover ? (
                <img
                  className="db-gallery-cover"
                  src={fileURL(cover.fileId)}
                  alt=""
                  loading="lazy"
                />
              ) : null;
            })()}
          <button
            className="db-gallery-title"
            onClick={() => openPage(r.pageId)}
          >
            {pages.find((p) => p.id === r.pageId)?.title ?? "Untitled"}
          </button>
          {properties
            .filter(
              (p) =>
                p.type !== "title" &&
                view.query.visiblePropertyIds.includes(p.id),
            )
            .map((p) => (
              <div key={p.id}>
                <small>{p.name}</small>
                <p>{displayValue(valueOf(r, p.id), p, pages) || "—"}</p>
              </div>
            ))}
        </article>
      ))}
    </div>
  );
}
export function Timeline({
  records,
  view,
  pages,
  openPage,
}: {
  records: D.Record[];
  view: D.View;
  pages: D.Page[];
  openPage: (id: string) => void;
}) {
  const cfg = view.config as { datePropertyId: string; scale: string };
  const items = records
    .map((r) => ({
      r,
      date: valueOf(r, cfg.datePropertyId) as {
        start?: string;
        end?: string;
      } | null,
    }))
    .sort((a, b) =>
      (a.date?.start ?? "9999").localeCompare(b.date?.start ?? "9999"),
    );
  const dated = items.filter((i) => i.date?.start);
  const starts = dated.map((i) => Date.parse(i.date!.start!));
  const ends = dated.map((i) => Date.parse(i.date!.end ?? i.date!.start!));
  const min = Math.min(...starts),
    max = Math.max(...ends, min + 86400000),
    span = max - min;
  return (
    <div className="db-timeline" aria-label="Database timeline">
      <p>{cfg.scale} scale · dates follow each record's stored timezone</p>
      {items.map(({ r, date }) => (
        <div key={r.id} className="db-timeline-row">
          <button onClick={() => openPage(r.pageId)}>
            {pages.find((p) => p.id === r.pageId)?.title}
          </button>
          <div className="db-timeline-track">
            {date?.start ? (
              <span
                className="db-timeline-bar"
                style={{
                  marginLeft: `${((Date.parse(date.start) - min) / span) * 80}%`,
                  width: `${Math.max(5, ((Date.parse(date.end ?? date.start) - Date.parse(date.start)) / span) * 80)}%`,
                }}
              >
                {date.start}
                {date.end ? ` → ${date.end}` : ""}
              </span>
            ) : (
              <span>Unscheduled</span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
export function AdvancedDatabasePanel({
  workspace: w,
  collection,
  view,
  records,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  view?: D.View;
  records: D.Record[];
  onCommitted: () => Promise<void>;
  onError: (e: unknown) => void;
}) {
  const [state, setState] = useState<DatabaseState>();
  const [busy, setBusy] = useState(false);
  const [recordId, setRecordId] = useState("");
  const [parent, setParent] = useState("");
  const [deps, setDeps] = useState<string[]>([]);
  const [destination, setDestination] = useState("");
  const [template, setTemplate] = useState("");
  useEffect(() => {
    let live = true;
    dispatch<DatabaseState>("databaseDerived", {
      ...envelope(w),
      collectionId: collection.id,
    })
      .then((s) => {
        if (live) setState(s);
      })
      .catch(onError);
    return () => {
      live = false;
    };
  }, [w.snapshotCursor, collection.id]);
  const run = async (action: () => Promise<unknown>) => {
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
  const title = (r: D.Record) =>
    w.pages.find((p) => p.id === r.pageId)?.title ?? "Untitled";
  const choose = (id: string) => {
    setRecordId(id);
    setParent(state?.structure[id]?.parentRecordId ?? "");
    setDeps(state?.structure[id]?.dependencyRecordIds ?? []);
  };
  return (
    <details className="db-panel db-advanced">
      <summary>
        Database structure, linked views and templates{" "}
        {state?.locked ? "· Locked" : ""}
      </summary>
      <p>
        <button
          disabled={busy}
          onClick={() =>
            void run(() =>
              batch(
                w,
                [
                  {
                    kind: "collection.configure",
                    collectionId: collection.id,
                    locked: !state?.locked,
                  },
                ],
                [guard("collection", collection.id, collection.revision)],
              ),
            )
          }
        >
          {state?.locked ? "Unlock database" : "Lock database"}
        </button>{" "}
        <small>A locked database rejects record and schema changes.</small>
      </p>
      <fieldset disabled={busy || state?.locked}>
        <legend>Sub-items and dependencies</legend>
        <label>
          Record{" "}
          <select value={recordId} onChange={(e) => choose(e.target.value)}>
            <option value="">Choose a record</option>
            {records.map((r) => (
              <option value={r.id} key={r.id}>
                {title(r)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Parent{" "}
          <select value={parent} onChange={(e) => setParent(e.target.value)}>
            <option value="">No parent</option>
            {records
              .filter((r) => r.id !== recordId)
              .map((r) => (
                <option value={r.id} key={r.id}>
                  {title(r)}
                </option>
              ))}
          </select>
        </label>
        <label>
          Depends on{" "}
          <select
            multiple
            value={deps}
            onChange={(e) =>
              setDeps([...e.target.selectedOptions].map((o) => o.value))
            }
          >
            {records
              .filter((r) => r.id !== recordId)
              .map((r) => (
                <option value={r.id} key={r.id}>
                  {title(r)}
                </option>
              ))}
          </select>
        </label>
        <button
          disabled={!recordId}
          onClick={() =>
            void run(async () => {
              const r = records.find((r) => r.id === recordId)!;
              const p = w.pages.find((p) => p.id === r.pageId)!;
              return batch(
                w,
                [
                  {
                    kind: "record.configure",
                    recordId,
                    parentRecordId: parent || null,
                    dependencyRecordIds: deps,
                  },
                ],
                uniqueGuards([
                  ...pageGuards(w, p),
                  ...[parent, ...deps]
                    .filter(Boolean)
                    .map((id) =>
                      guard(
                        "record",
                        id,
                        w.records.find((r) => r.id === id)!.revision,
                      ),
                    ),
                ]),
              );
            })
          }
        >
          Save structure
        </button>
      </fieldset>
      {view && (
        <fieldset disabled={busy}>
          <legend>Linked view</legend>
          <label>
            Show this collection on page{" "}
            <select
              value={destination}
              onChange={(e) => setDestination(e.target.value)}
            >
              <option value="">Choose a page</option>
              {w.pages
                .filter((p) => !p.deletedAt)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title}
                  </option>
                ))}
            </select>
          </label>
          <button
            disabled={!destination}
            onClick={() =>
              void run(async () => {
                const [id] = await ids(w, 1);
                await saveView(w, {
                  ...view,
                  id,
                  owningPageId: destination,
                  name: `${view.name} · linked`,
                  revision: undefined,
                });
              })
            }
          >
            Create linked view
          </button>
          <small>All linked views read and edit the same records.</small>
        </fieldset>
      )}
      <fieldset disabled={busy || state?.locked}>
        <legend>Record templates</legend>
        <label>
          Use record as a template{" "}
          <select
            value={template}
            onChange={(e) => setTemplate(e.target.value)}
          >
            <option value="">Choose a record</option>
            {records.map((r) => (
              <option key={r.id} value={r.id}>
                {title(r)}
              </option>
            ))}
          </select>
        </label>
        <button
          disabled={!template}
          onClick={() =>
            void run(async () => {
              const source = records.find((r) => r.id === template)!;
              const name = prompt("New record title", `${title(source)} copy`);
              if (!name) return;
              const [recordId, pageId] = await ids(w, 2);
              const editable = new Set(
                w.properties
                  .filter(
                    (p) =>
                      p.collectionId === collection.id &&
                      ![
                        "title",
                        "formula",
                        "rollup",
                        "created_time",
                        "last_edited_time",
                        "unique_id",
                      ].includes(p.type),
                  )
                  .map((p) => p.id),
              );
              const vals = source.values.filter((v) =>
                editable.has(v.propertyId),
              );
              const guards = uniqueGuards([
                guard("collection", collection.id, collection.revision),
                guard("record", recordId, "0"),
                guard("page", pageId, "0"),
                ...vals.map((v) =>
                  guard(
                    "property",
                    v.propertyId,
                    w.properties.find((p) => p.id === v.propertyId)!.revision,
                  ),
                ),
                ...vals.flatMap((v) =>
                  w.properties.find((p) => p.id === v.propertyId)?.type ===
                  "relation"
                    ? (v.value as string[]).map((id) =>
                        guard(
                          "record",
                          id,
                          w.records.find((r) => r.id === id)!.revision,
                        ),
                      )
                    : [],
                ),
              ]);
              await batch(
                w,
                [
                  {
                    kind: "record.create",
                    recordId,
                    pageId,
                    collectionId: collection.id,
                    title: name,
                  },
                  ...vals.map((v) => ({
                    kind: "record.setValue" as const,
                    recordId,
                    propertyId: v.propertyId,
                    value: {
                      type: w.properties.find((p) => p.id === v.propertyId)!
                        .type,
                      schemaVersion: v.schemaVersion,
                      value: v.value,
                    },
                  })),
                ],
                guards,
              );
            })
          }
        >
          Create from template
        </button>
        <small>
          Copies editable property values; the new record has its own identity
          and page.
        </small>
      </fieldset>
    </details>
  );
}
