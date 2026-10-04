import { useEffect, useMemo, useRef, useState } from "react";
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
import { VirtualRows } from "./VirtualRows";
import { TemplateLibrary } from "../advanced/TemplateLibrary";

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
  virtualIdentity,
}: {
  records: D.Record[];
  view: D.View;
  properties: D.PropertyDefinition[];
  pages: D.Page[];
  files: D.File[];
  openPage: (id: string) => void;
  virtualIdentity?: string;
}) {
  const cfg = view.config as {
    cardSize: string;
    coverPropertyId: string | null;
  };
  const gallery = useRef<HTMLDivElement>(null);
  const [galleryWidth, setGalleryWidth] = useState(600);
  useEffect(() => {
    const element = gallery.current;
    if (!element) return;
    const observer = new ResizeObserver(() =>
      setGalleryWidth(element.clientWidth),
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [virtualIdentity]);
  const columns = Math.max(
    1,
    Math.floor(
      (galleryWidth + 16) /
        ((cfg.cardSize === "small"
          ? 180
          : cfg.cardSize === "large"
            ? 320
            : 240) +
          16),
    ),
  );
  const card = (r: D.Record) => (
    <article key={r.id} className="db-gallery-card">
      {cfg.coverPropertyId &&
        (() => {
          const cover = (
            valueOf(r, cfg.coverPropertyId) as { fileId: string }[] | null
          )?.find((value) =>
            files
              .find((file) => file.id === value.fileId)
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
      <button className="db-gallery-title" onClick={() => openPage(r.pageId)}>
        {pages.find((p) => p.id === r.pageId)?.title ?? "Untitled"}
      </button>
      {properties
        .filter(
          (p) =>
            p.type !== "title" && view.query.visiblePropertyIds.includes(p.id),
        )
        .map((p) => (
          <div key={p.id}>
            <small>{p.name}</small>
            <p>{displayValue(valueOf(r, p.id), p, pages) || "—"}</p>
          </div>
        ))}
    </article>
  );
  if (virtualIdentity)
    return (
      <div ref={gallery}>
        <VirtualRows
          count={Math.ceil(records.length / columns)}
          identity={`${virtualIdentity}:${columns}`}
          estimate={280}
          renderRow={(index) => (
            <div
              className="db-gallery-window-row"
              style={{
                gridTemplateColumns: `repeat(${columns},minmax(0,1fr))`,
              }}
            >
              {records
                .slice(index * columns, index * columns + columns)
                .map(card)}
            </div>
          )}
        />
      </div>
    );
  return (
    <div className={`db-gallery db-gallery-${cfg.cardSize}`}>
      {records.map(card)}
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
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [recordId, setRecordId] = useState("");
  const [parent, setParent] = useState("");
  const [deps, setDeps] = useState<string[]>([]);
  const [destination, setDestination] = useState("");
  useEffect(() => {
    let live = true;
    if (!expanded) return;
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
  }, [w.snapshotCursor, collection.id, expanded]);
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
  const pageTitles = useMemo(
    () => new Map(w.pages.map((page) => [page.id, page.title])),
    [w.pages],
  );
  const title = (r: D.Record) => pageTitles.get(r.pageId) ?? "Untitled";
  const choose = (id: string) => {
    setRecordId(id);
    setParent(state?.structure[id]?.parentRecordId ?? "");
    setDeps(state?.structure[id]?.dependencyRecordIds ?? []);
  };
  return (
    <details
      className="db-panel db-advanced"
      onToggle={(event) => setExpanded(event.currentTarget.open)}
    >
      <summary>
        Database structure, linked views and templates{" "}
        {state?.locked ? "· Locked" : ""}
      </summary>
      {expanded && (
        <>
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
              <select
                value={parent}
                onChange={(e) => setParent(e.target.value)}
              >
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
          <TemplateLibrary
            workspace={w}
            collectionId={collection.id}
            sourcePages={records.flatMap((r) => {
              const page = w.pages.find((p) => p.id === r.pageId);
              return page ? [page] : [];
            })}
            disabled={busy || state?.locked}
            onCommitted={onCommitted}
            onError={onError}
          />
        </>
      )}
    </details>
  );
}
