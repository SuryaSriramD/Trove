import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  qualificationFrames,
  qualificationStart,
} from "../qualificationPerformance";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  type WorkspaceSnapshot,
  type D,
  type S,
} from "../api";
import {
  clearProperty,
  createDatabase,
  createRecord,
  defaultQuery,
  guard,
  loadRecords,
  loadRecordPage,
  saveView,
  setProperty,
  uniqueGuards,
  viewConfig,
} from "./service";
import {
  displayValue,
  groupRecords,
  options,
  reschedule,
  valueOf,
  type RecordGroup,
} from "./model";
import "./database.css";
import { AutofillPanel } from "./AutofillPanel";
import { VirtualRows } from "./VirtualRows";
import { Timeline } from "./Timeline";
import { FormulaEditor } from "./FormulaEditor";
import {
  RelationPicker,
  indexRelationTargets,
  type RelationTargets,
} from "./RelationPicker";
import { loadRecordWindow } from "./recordWindow";
import { calendarDay, resizeDateRange } from "./dateRange";
import { P2DatabasePanel, P2RowButtons } from "./P2Database";
import type { P2Button, P2Config } from "./P2Model";
import {
  AdvancedDatabasePanel,
  Gallery,
  DerivedCellView,
} from "./AdvancedDatabase";
type Props = {
  workspace: WorkspaceSnapshot;
  collectionId?: string;
  owningPageId?: string;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
  openPage: (id: string) => void;
};
const editableTypes = [
  "text",
  "number",
  "select",
  "multi_select",
  "status",
  "checkbox",
  "date",
  "url",
  "email",
  "phone",
  "files",
  "relation",
  "identity",
  "place",
] as const;
export default function DatabaseView({
  workspace: w,
  collectionId,
  owningPageId,
  onCommitted,
  onError,
  openPage,
}: Props) {
  const [selected, setSelected] = useState(collectionId ?? "");
  const [promotion, setPromotion] = useState<{ record: D.Record; statusId: string; dueId: string } | null>(null);
  const [active, setActive] = useState("");
  const [records, setRecords] = useState<D.Record[]>([]);
  const qualificationLoad = useRef<{
    started: number;
    returned: number;
    records: D.Record[];
  } | null>(null);
  const [pager, setPager] = useState<{
    key: string;
    index: number;
    history: string[];
    clock: number;
  }>({ key: "", index: 0, history: [""], clock: -1 });
  const [nextCursor, setNextCursor] = useState<string>();
  const [loadError, setLoadError] = useState("");
  const [clockTick, setClockTick] = useState(0);
  useEffect(() => {
    // Cursor clock identity changes at the UTC minute boundary, rather than
    // sixty seconds after this component happened to mount.
    let timer: ReturnType<typeof setTimeout>;
    const schedule = () => {
      timer = setTimeout(
        () => {
          setClockTick((x) => x + 1);
          schedule();
        },
        60000 - (Date.now() % 60000) + 1,
      );
    };
    schedule();
    return () => clearTimeout(timer);
  }, []);
  const settledWindow = useRef<{
    key: string;
    index: number;
    clock: number;
  } | null>(null);
  const [loading, setLoading] = useState(false);
  useLayoutEffect(() => {
    const observed = qualificationLoad.current;
    if (loading || !observed || records !== observed.records) return;
    qualificationLoad.current = null;
    return qualificationFrames([
      ["database_load_to_frames", observed.started],
      ["database_response_to_frames", observed.returned],
    ]);
  }, [records, loading]);
  const [busy, setBusy] = useState(false);
  const [settings, setSettings] = useState(false);
  const [p2Buttons, setP2Buttons] = useState<P2Button[]>([]);
  const [newProperty, setNewProperty] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const collection =
    w.collections.find((c) => c.id === (collectionId ?? selected)) ??
    (!collectionId
      ? w.collections.find((c) => c.systemRole !== "tasks")
      : undefined);
  const properties = w.properties.filter(
    (p) => p.collectionId === collection?.id,
  );
  // A saved view belongs to a page independently of its shared collection.
  // Page embeds must not pick a default/filter from another page's placement.
  const views = w.views.filter(
    (v) =>
      v.collectionId === collection?.id &&
      (!owningPageId || v.owningPageId === owningPageId),
  );
  const view = views.find((v) => v.id === active) ?? views[0];
  const windowed =
    (!view || view.type === "table" || view.type === "gallery") &&
    !view?.query.groupByPropertyId &&
    !view?.query.subgroupByPropertyId;
  const queryKey = `${w.workspaceId}:${w.snapshotCursor}:${collection?.id}:${collection?.revision}:${view?.id}:${view?.revision}`;
  const pageIndex = pager.key === queryKey ? pager.index : 0;
  const displayedPageIndex =
    settledWindow.current?.key === queryKey
      ? settledWindow.current.index
      : pageIndex;
  const pageHistory =
    pager.key === queryKey && pager.clock === clockTick ? pager.history : [""];
  const pageById = useMemo(
    () => new Map(w.pages.map((page) => [page.id, page])),
    [w.pages],
  );
  const relationTargets = useMemo(
    () => indexRelationTargets(w.records, w.pages),
    [w.records, w.pages],
  );
  useEffect(() => {
    if (collectionId) setSelected(collectionId);
  }, [collectionId]);
  useEffect(() => {
    let live = true;
    if (!collection) {
      qualificationLoad.current = null;
      setRecords([]);
      return;
    }
    if (
      settledWindow.current?.key === queryKey &&
      settledWindow.current.index === pageIndex &&
      settledWindow.current.clock === clockTick
    )
      return;
    qualificationLoad.current = null;
    setLoading(true);
    // A clock refresh retains the settled rows and viewport until its fresh
    // cursor chain completes. Canonical/query changes still restart at page 1.
    if (settledWindow.current?.key !== queryKey) setRecords([]);
    setNextCursor(undefined);
    setLoadError("");
    const qualificationStarted = qualificationStart();
    const load = windowed
      ? loadRecordWindow(
          (cursor) => loadRecordPage(w, collection.id, view?.id, cursor),
          pageIndex,
          pageHistory,
          () => !live,
        )
      : loadRecords(w, collection.id, view?.id, () => !live).then(
          (records) => ({
            records,
            nextCursor: undefined,
            index: 0,
            history: [""],
          }),
        );
    load
      .then((r) => {
        if (live) {
          settledWindow.current = {
            key: queryKey,
            index: r.index,
            clock: clockTick,
          };
          setPager({
            key: queryKey,
            index: r.index,
            history: r.history,
            clock: clockTick,
          });
          const returned = qualificationStart();
          if (qualificationStarted !== undefined && returned !== undefined) {
            qualificationLoad.current = {
              started: qualificationStarted,
              returned,
              records: r.records,
            };
          }
          setRecords(r.records);
          setNextCursor(r.nextCursor);
        }
      })
      .catch((error) => {
        if (live) {
          setLoadError(error instanceof Error ? error.message : String(error));
          onError(error);
        }
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [queryKey, pageIndex, windowed, clockTick]);
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
  const change = async (
    record: D.Record,
    property: D.PropertyDefinition,
    value: D.RawValue,
  ) => {
    await run(() => setProperty(w, record, property, value));
  };
  const addDatabase = () => {
    const title = prompt("Database name", "Untitled database");
    if (!title?.trim()) return;
    void run(async () => {
      const created = await createDatabase(w, title.trim());
      setSelected(created.collectionId);
      setActive("");
    });
  };
  const visible = view
    ? view.query.visiblePropertyIds.flatMap((id) => {
        const property = properties.find((p) => p.id === id);
        return property ? [property] : [];
      })
    : properties;
  const columnWidth = (id: string) =>
    view?.type === "table"
      ? ((view.config as D.view_table_v1).columnWidths.find(
          (c) => c.propertyId === id,
        )?.pixels ?? 220)
      : 220;
  useEffect(() => {
    let active = true;
    setP2Buttons([]);
    if (collection)
      dispatch<{ config: P2Config }>("databaseP2", {
        ...envelope(w),
        collectionId: collection.id,
      })
        .then((result) => {
          if (active) setP2Buttons(result.config.buttons);
        })
        .catch(onError);
    return () => {
      active = false;
    };
  }, [collection?.id, collection?.revision, w.snapshotCursor]);
  const titleFor = (r: D.Record) =>
    pageById.get(r.pageId)?.title ?? "Unavailable";
  const rename = async (r: D.Record, title: string) => {
    const p = w.pages.find((p) => p.id === r.pageId);
    if (p && p.title !== title)
      await run(() =>
        batch(
          w,
          [{ kind: "page.update", pageId: p.id, title }],
          pageGuards(w, p),
        ),
      );
  };
  const promote = (r: D.Record) => {
    const status = properties.find(p => p.type === "status" && (p.config as any).doneOptionId && (p.config as any).reopenOptionId);
    if (!status) { onError(Error("Add a status property with task mappings first.")); return; }
    setPromotion({ record: r, statusId: status.id, dueId: "" });
  };
  async function confirmPromotion() {
    if (!promotion) return;
    const page = w.pages.find(p => p.id === promotion.record.pageId), status = properties.find(p => p.id === promotion.statusId), due = properties.find(p => p.id === promotion.dueId);
    if (!page || !status) return;
    await run(async () => {
      await batch(w, [{ kind: "task.promote", recordId: promotion.record.id, statusPropertyId: status.id, ...(due ? { duePropertyId: due.id } : {}) }], uniqueGuards([...pageGuards(w, page), guard("property", status.id, status.revision), ...(due ? [guard("property", due.id, due.revision)] : [])]));
      setPromotion(null);
    });
  }
  function cell(r: D.Record, p: D.PropertyDefinition) {
    if (p.type === "title")
      return (
        <div className="db-title-cell">
          <input
            aria-label={`Title for ${titleFor(r)}`}
            defaultValue={titleFor(r)}
            key={`${r.id}:${r.revision}`}
            onBlur={(e) => void rename(r, e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
            disabled={busy}
          />
          <button
            aria-label={`Open ${titleFor(r)} page`}
            onClick={() => openPage(r.pageId)}
          >
            ↗
          </button>
        </div>
      );
    return (
      <div className="db-value-cell">
        <ValueEditor
          property={p}
          value={valueOf(r, p.id)}
          workspace={w}
          relationTargets={relationTargets.get(
            (p.config as { targetCollectionId?: string }).targetCollectionId ??
              "",
          )}
          label={`${p.name} for ${titleFor(r)}`}
          disabled={busy}
          onChange={(v) => void change(r, p, v)}
          key={`${r.id}:${p.id}:${r.revision}`}
        />
        {valueOf(r, p.id) !== null &&
          (editableTypes as readonly string[]).includes(p.type) &&
          !w.taskFacets.some(
            (f) => f.recordId === r.id && f.statusPropertyId === p.id,
          ) && (
            <button
              className="db-clear-value"
              aria-label={`Clear ${p.name} for ${titleFor(r)}`}
              disabled={busy}
              onClick={() => void run(() => clearProperty(w, r, p))}
            >
              Clear
            </button>
          )}
      </div>
    );
  }
  function rowActions(r: D.Record) {
    return (
      <div className="db-row-actions">
        {collection && (
          <P2RowButtons
            workspace={w}
            collection={collection}
            record={r}
            buttons={p2Buttons}
            onCommitted={onCommitted}
            onError={onError}
          />
        )}
        <button
          aria-label={`Open ${titleFor(r)} page`}
          onClick={() => openPage(r.pageId)}
        >
          Open page
        </button>
        {!w.taskFacets.some((f) => f.recordId === r.id) && (
          <button onClick={() => promote(r)}>Make task</button>
        )}
        {w.taskFacets.some((f) => f.recordId === r.id) && (
          <span title="This record is also a task">✓ Task</span>
        )}
      </div>
    );
  }
  function flatRows(items: D.Record[], type: string) {
    if (type === "table" && windowed)
      return (
        <VirtualRows
          count={items.length}
          identity={`${queryKey}:${displayedPageIndex}`}
          table
          colSpan={visible.length + 1}
          recordId={(index) => items[index].id}
          width={visible.reduce((width, p) => width + columnWidth(p.id), 160)}
          header={
            <>
              <colgroup>
                {visible.map((p) => (
                  <col key={p.id} style={{ width: columnWidth(p.id) }} />
                ))}
                <col style={{ width: 160 }} />
              </colgroup>
              <thead>
                <tr>
                  {visible.map((p) => (
                    <th key={p.id} scope="col">
                      {p.name}
                    </th>
                  ))}
                  <th scope="col">
                    <span className="sr-only">Record actions</span>
                  </th>
                </tr>
              </thead>
            </>
          }
          renderRow={(index) => {
            const r = items[index];
            return (
              <>
                {visible.map((p) => (
                  <td key={p.id}>{cell(r, p)}</td>
                ))}
                <td>{rowActions(r)}</td>
              </>
            );
          }}
        />
      );
    if (type === "table")
      return (
        <div className="db-table-scroll">
          <table
            className="db-table"
            style={{
              tableLayout: "fixed",
              width: visible.reduce(
                (width, p) => width + columnWidth(p.id),
                160,
              ),
            }}
          >
            <colgroup>
              {visible.map((p) => (
                <col key={p.id} style={{ width: columnWidth(p.id) }} />
              ))}
              <col style={{ width: 160 }} />
            </colgroup>
            <thead>
              <tr>
                {visible.map((p) => (
                  <th key={p.id} scope="col">
                    {p.name}
                  </th>
                ))}
                <th scope="col">
                  <span className="sr-only">Record actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <tr key={r.id} data-record-id={r.id}>
                  {visible.map((p) => (
                    <td key={p.id}>{cell(r, p)}</td>
                  ))}
                  <td>{rowActions(r)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    return (
      <div className="db-list">
        {items.map((r) => (
          <article key={r.id} data-record-id={r.id}>
            <div className="db-list-title">
              <button onClick={() => openPage(r.pageId)}>{titleFor(r)}</button>
              {rowActions(r)}
            </div>
            <dl>
              {visible.map((p) => (
                <div key={p.id}>
                  <dt>{p.name}</dt>
                  <dd>{cell(r, p)}</dd>
                </div>
              ))}
            </dl>
          </article>
        ))}
      </div>
    );
  }
  function grouped(
    groups: RecordGroup[],
    type: string,
    level = 0,
  ): React.ReactNode {
    return groups.map((group) => (
      <section key={group.key} className={`db-group db-group-${level}`}>
        {group.label && (
          <h3>
            {group.label} <span>{group.records.length}</span>
          </h3>
        )}
        {group.children.length
          ? grouped(group.children, type, level + 1)
          : flatRows(group.records, type)}
      </section>
    ));
  }
  const addView = async (type: D.View["type"]) => {
    if (!collection) return;
    const [id] = await ids(w, 1);
    await saveView(w, {
      id,
      collectionId: collection.id,
      containerId: null,
      owningPageId: owningPageId ?? collection.owningPageId,
      name: `${type[0].toUpperCase() + type.slice(1)} view`,
      type,
      schemaVersion: 1,
      query: defaultQuery(properties.map((p) => p.id)),
      config: viewConfig(type, properties),
    });
    setActive(id);
  };
  return (
    <section className="database" aria-label="Database">
      {collection && (
        <P2DatabasePanel
          key={collection.id}
          workspace={w}
          collection={collection}
          records={records}
          openPage={openPage}
          onCommitted={onCommitted}
          onError={onError}
        />
      )}
      <div className="db-header">
        <div>
          {!collectionId && (
            <label className="db-picker">
              Database
              <select
                aria-label="Choose database"
                value={collection?.id ?? ""}
                onChange={(e) => {
                  setSelected(e.target.value);
                  setActive("");
                }}
              >
                <option value="" disabled>
                  Choose a database
                </option>
                {w.collections.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.title}
                  </option>
                ))}
              </select>
            </label>
          )}
          {collection && <h2>{collection.title}</h2>}
        </div>
        {!collectionId && (
          <button onClick={addDatabase} disabled={busy}>
            ＋ New database
          </button>
        )}
      </div>
      {!collection ? (
        <div className="empty-state">
          <h3>Give your work some structure.</h3>
          <p>
            One collection, shared across every view. Every record has its own
            page.
          </p>
          <button className="primary" onClick={addDatabase}>
            Create database
          </button>
        </div>
      ) : (
        <>
          <div className="db-viewbar">
            <div role="tablist" aria-label="Saved database views">
              {views.map((v) => (
                <button
                  key={v.id}
                  role="tab"
                  aria-selected={v.id === view?.id}
                  className={v.id === view?.id ? "active" : ""}
                  onClick={() => setActive(v.id)}
                >
                  {
                    (
                      {
                        table: "▦",
                        list: "☷",
                        board: "▥",
                        calendar: "▦",
                      } as Record<string, string>
                    )[v.type]
                  }{" "}
                  {v.name}
                </button>
              ))}
            </div>
            <select
              aria-label="Add saved view"
              value=""
              disabled={busy}
              onChange={(e) => {
                const value = e.target.value as D.View["type"];
                if (value) void run(() => addView(value));
              }}
            >
              <option value="">＋ View</option>
              {[
                "table",
                "list",
                "board",
                "calendar",
                "gallery",
                "timeline",
              ].map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
            <button
              disabled={!view}
              aria-expanded={settings}
              onClick={() => setSettings(!settings)}
            >
              View settings
            </button>
            <button
              aria-expanded={newProperty}
              onClick={() => setNewProperty(!newProperty)}
            >
              ＋ Property
            </button>
          </div>
          {promotion && <div className="task-promotion" role="dialog" aria-label="Promote record to task"><strong>Make {titleFor(promotion.record)} a task</strong>
            <label>Status property<select aria-label="Task promotion status" value={promotion.statusId} onChange={e => setPromotion({ ...promotion, statusId: e.target.value })}>{properties.filter(p => p.type === "status" && (p.config as any).doneOptionId && (p.config as any).reopenOptionId).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
            <label>Due property<select aria-label="Task promotion due property" value={promotion.dueId} onChange={e => setPromotion({ ...promotion, dueId: e.target.value })}><option value="">No due property</option>{properties.filter(p => p.type === "date").map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
            <p>The record, page and existing property values stay in this database.</p><button disabled={busy} onClick={() => void confirmPromotion()}>Confirm task</button><button disabled={busy} onClick={() => setPromotion(null)}>Cancel</button>
          </div>}
          <AutofillPanel workspace={w} collection={collection} records={records} onCommitted={onCommitted} onError={onError} openPage={openPage} />
          <AdvancedDatabasePanel
            workspace={w}
            collection={collection}
            view={view}
            records={w.records.filter((r) => r.collectionId === collection.id)}
            onCommitted={onCommitted}
            onError={onError}
          />
          <FormulaEditor
            workspace={w}
            collection={collection}
            onCommitted={onCommitted}
            onError={onError}
          />
          {settings && view && (
            <ViewSettings
              key={`${view.id}:${view.revision}`}
              view={view}
              properties={properties}
              workspace={w}
              busy={busy}
              onSave={(v) =>
                void run(async () => {
                  await saveView(w, v);
                  setSettings(false);
                })
              }
            />
          )}{" "}
          {newProperty && (
            <PropertyForm
              workspace={w}
              collection={collection}
              busy={busy}
              onSave={async (command) => {
                await run(() =>
                  batch(
                    w,
                    [command],
                    [
                      guard("collection", collection.id, collection.revision),
                      guard("property", command.propertyId, "0"),
                    ],
                  ),
                );
                setNewProperty(false);
              }}
              onError={onError}
            />
          )}
          <form
            className="db-add-row"
            onSubmit={(e) => {
              e.preventDefault();
              if (!newTitle.trim()) return;
              void run(async () => {
                await createRecord(w, collection, newTitle.trim());
                setNewTitle("");
              });
            }}
          >
            <input
              aria-label="New record title"
              placeholder="Add a record…"
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              disabled={busy}
            />
            <button className="primary" disabled={busy || !newTitle.trim()}>
              Add record
            </button>
          </form>
          <div role="status" className="db-count">
            {loading
              ? "Loading records…"
              : windowed && (displayedPageIndex > 0 || nextCursor)
                ? `${records.length ? displayedPageIndex * 200 + 1 : 0}–${displayedPageIndex * 200 + records.length} records${nextCursor ? " · more available" : ""}`
                : `${records.length} record${records.length === 1 ? "" : "s"}`}
            {busy ? " · Saving…" : ""}
          </div>
          {loadError && (
            <div role="alert">
              {loadError}{" "}
              <button
                onClick={() => {
                  setPager({
                    key: queryKey,
                    index: 0,
                    history: [""],
                    clock: -1,
                  });
                  setClockTick((n) => n + 1);
                }}
              >
                Restart view
              </button>
            </div>
          )}
          {windowed && (
            <nav className="db-pager" aria-label="Record pages">
              <button
                disabled={loading || displayedPageIndex === 0}
                onClick={() =>
                  setPager({
                    key: queryKey,
                    index: displayedPageIndex - 1,
                    history: pageHistory,
                    clock: clockTick,
                  })
                }
              >
                Previous records
              </button>
              <span>Page {displayedPageIndex + 1}</span>
              <button
                disabled={loading || !nextCursor}
                onClick={() =>
                  setPager({
                    key: queryKey,
                    index: displayedPageIndex + 1,
                    history: [
                      ...pageHistory.slice(0, displayedPageIndex + 1),
                      nextCursor!,
                    ],
                    clock: clockTick,
                  })
                }
              >
                Next records
              </button>
            </nav>
          )}
          {view?.type === "board" ? (
            <Board
              records={records}
              view={view}
              properties={properties}
              pages={w.pages}
              busy={busy}
              openPage={openPage}
              onChange={change}
              renderCard={(r) => (
                <>
                  {visible.map((p) => (
                    <div className="db-card-property" key={p.id}>
                      <small>{p.name}</small>
                      {cell(r, p)}
                    </div>
                  ))}
                  {rowActions(r)}
                </>
              )}
            />
          ) : view?.type === "gallery" ? (
            <Gallery
              records={records}
              view={view}
              properties={properties}
              pages={w.pages}
              files={w.files}
              openPage={openPage}
              virtualIdentity={
                windowed ? `${queryKey}:${displayedPageIndex}` : undefined
              }
            />
          ) : view?.type === "timeline" ? (
            <Timeline
              records={records}
              view={view}
              workspace={w}
              properties={properties}
              busy={busy}
              openPage={openPage}
              onChange={change}
              onError={onError}
            />
          ) : view?.type === "calendar" ? (
            <Calendar
              records={records}
              view={view}
              properties={properties}
              pages={w.pages}
              busy={busy}
              openPage={openPage}
              onChange={change}
              onError={onError}
            />
          ) : (
            grouped(
              groupRecords(
                records,
                properties.find((p) => p.id === view?.query.groupByPropertyId),
                properties.find(
                  (p) => p.id === view?.query.subgroupByPropertyId,
                ),
                w.pages,
              ),
              view?.type === "list" ? "list" : "table",
            )
          )}
          {!loading && !loadError && !records.length && (
            <p className="db-empty">
              No records match this view. Add one above or adjust its filters.
            </p>
          )}
        </>
      )}
    </section>
  );
}
function ValueEditor({
  property: p,
  value,
  workspace: w,
  relationTargets,
  label,
  disabled,
  onChange,
}: {
  property: D.PropertyDefinition;
  value: D.RawValue;
  workspace: WorkspaceSnapshot;
  relationTargets?: RelationTargets;
  label: string;
  disabled: boolean;
  onChange: (value: D.RawValue) => void;
}) {
  const text = p.type === "relation" ? "" : displayValue(value, p, w.pages);
  const [draft, setDraft] = useState(text);
  const scalar = (type = "text") => (
    <input
      type={type}
      aria-label={label}
      value={draft}
      disabled={disabled}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => {
        if (draft !== text) onChange(draft);
      }}
      onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
    />
  );
  switch (p.type) {
    case "formula":
    case "rollup":
      return <DerivedCellView value={value} />;
    case "created_time":
    case "last_edited_time":
    case "unique_id":
      return <span title="Generated by Trove">{text || "—"}</span>;
    case "relation":
      return (
        <RelationPicker
          targets={relationTargets}
          value={value}
          label={label}
          disabled={disabled}
          onChange={onChange}
        />
      );
    case "identity":
      return (
        <input
          aria-label={label}
          disabled={disabled}
          defaultValue={
            Array.isArray(value)
              ? (value as { displayName: string }[])
                  .map((v) => v.displayName)
                  .join(", ")
              : ""
          }
          onBlur={(e) =>
            onChange(
              e.target.value
                .split(",")
                .map((s) => s.trim())
                .filter(Boolean)
                .map((displayName) => ({
                  sourceSystem: "trove",
                  externalKind: "person",
                  displayName,
                  raw: {},
                })),
            )
          }
        />
      );
    case "place":
      return (
        <input
          aria-label={label}
          disabled={disabled}
          defaultValue={(value as { label?: string } | null)?.label ?? ""}
          onBlur={(e) =>
            onChange({
              ...((value as object) ?? {}),
              label: e.target.value,
              raw: (value as { raw?: D.RawValue } | null)?.raw ?? {},
            })
          }
        />
      );
    case "checkbox":
      return (
        <input
          type="checkbox"
          aria-label={label}
          disabled={disabled}
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
        />
      );
    case "select":
    case "status":
      return (
        <select
          aria-label={label}
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value || null)}
        >
          <option value="" disabled={p.type === "status"}>
            {p.type === "status" ? "Set status" : "Empty"}
          </option>
          {options(p).map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </select>
      );
    case "multi_select":
      return (
        <select
          aria-label={label}
          multiple
          value={Array.isArray(value) ? (value as string[]) : []}
          disabled={disabled}
          onChange={(e) =>
            onChange(Array.from(e.target.selectedOptions, (o) => o.value))
          }
        >
          {options(p).map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </select>
      );
    case "files":
      return (
        <select
          multiple
          aria-label={label}
          disabled={disabled}
          value={
            Array.isArray(value)
              ? (value as { fileId: string }[]).map((f) => f.fileId)
              : []
          }
          onChange={(e) =>
            onChange(
              Array.from(e.target.selectedOptions, (o) => ({
                fileId: o.value,
                displayName:
                  w.files.find((f) => f.id === o.value)?.originalName ?? o.text,
              })),
            )
          }
        >
          {w.files.map((f) => (
            <option key={f.id} value={f.id}>
              {f.originalName}
            </option>
          ))}
        </select>
      );
    case "date":
      return (
        <DateEditor
          label={label}
          value={value}
          disabled={disabled}
          onChange={onChange}
        />
      );
    case "number":
      return (
        <input
          aria-label={label}
          inputMode="decimal"
          value={draft}
          disabled={disabled}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => {
            if (draft !== text)
              onChange({
                decimal: draft,
                format: (p.config as { format: string }).format,
              });
          }}
          onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
        />
      );
    case "text":
      return (
        <textarea
          aria-label={label}
          value={draft}
          disabled={disabled}
          rows={1}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => {
            if (draft !== text) onChange([{ text: draft }]);
          }}
        />
      );
    case "url":
      return scalar("url");
    case "email":
      return scalar("email");
    case "phone":
      return scalar("tel");
    default:
      return (
        <details className="db-preserved">
          <summary title="Preserved source value">
            {text || "Empty"} <span>Preserved</span>
          </summary>
          <pre>{JSON.stringify(value, null, 2)}</pre>
        </details>
      );
  }
}
export function DateEditor({
  label,
  value,
  disabled,
  onChange,
}: {
  label: string;
  value: D.RawValue;
  disabled: boolean;
  onChange: (v: D.RawValue) => void;
}) {
  const old = value as {
    start?: string;
    end?: string | null;
    timezone?: string;
    hasTime?: boolean;
  } | null;
  const [hasTime, setHasTime] = useState(old?.hasTime ?? false),
    [start, setStart] = useState(old?.start ?? ""),
    [end, setEnd] = useState(old?.end ?? ""),
    [timezone, setTimezone] = useState(
      old?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
    );
  return (
    <details className="db-date-editor">
      <summary>{old?.start ? displayValue(value) : "Set date"}</summary>
      <div>
        <label>
          <input
            type="checkbox"
            checked={hasTime}
            onChange={(e) => setHasTime(e.target.checked)}
            disabled={disabled}
          />{" "}
          Include time
        </label>
        <label>
          Start
          <input
            aria-label={`${label} start`}
            placeholder={hasTime ? "2026-09-30T09:00:00+05:30" : "YYYY-MM-DD"}
            type={hasTime ? "text" : "date"}
            value={start}
            onChange={(e) => setStart(e.target.value)}
            disabled={disabled}
          />
        </label>
        <label>
          End
          <input
            aria-label={`${label} end`}
            type={hasTime ? "text" : "date"}
            value={end}
            onChange={(e) => setEnd(e.target.value)}
            disabled={disabled}
          />
        </label>
        <label>
          Timezone
          <input
            aria-label={`${label} timezone`}
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            disabled={disabled}
          />
        </label>
        <button
          type="button"
          disabled={disabled || !start}
          onClick={() =>
            onChange({ start, end: end || null, timezone, hasTime })
          }
        >
          Save date
        </button>
      </div>
    </details>
  );
}
function Board({
  records,
  view,
  properties,
  pages,
  busy,
  onChange,
  renderCard,
}: {
  records: D.Record[];
  view: D.View;
  properties: D.PropertyDefinition[];
  pages: D.Page[];
  busy: boolean;
  openPage: (id: string) => void;
  onChange: (
    r: D.Record,
    p: D.PropertyDefinition,
    v: D.RawValue,
  ) => Promise<void>;
  renderCard: (r: D.Record) => React.ReactNode;
}) {
  const config = view.config as {
      groupPropertyId: string;
      hideEmptyGroups: boolean;
    },
    property = properties.find((p) => p.id === config.groupPropertyId);
  if (!property)
    return <p>Choose a board grouping property in view settings.</p>;
  const groups =
    property.type === "checkbox"
      ? [
          { id: "false", label: "Unchecked", value: false },
          { id: "true", label: "Checked", value: true },
        ]
      : [
          { id: "empty", label: `No ${property.name}`, value: null },
          ...options(property).map((o) => ({
            ...o,
            value: o.id as D.RawValue,
          })),
        ];
  const sub = properties.find((p) => p.id === view.query.subgroupByPropertyId);
  return (
    <div className="db-board" aria-label="Board view">
      {groups.map((group) => {
        const items = records.filter((r) => {
          const value = valueOf(r, property.id);
          return (
            value === group.value ||
            (property.type === "checkbox" &&
              group.value === false &&
              value === null)
          );
        });
        if (config.hideEmptyGroups && !items.length) return null;
        return (
          <section
            className="db-board-column"
            key={group.id}
            aria-label={group.label}
            onDragOver={(e) => {
              if (
                !busy &&
                !(property.type === "status" && group.value === null)
              )
                e.preventDefault();
            }}
            onDrop={(e) => {
              e.preventDefault();
              const r = records.find(
                (r) =>
                  r.id === e.dataTransfer.getData("application/x-trove-record"),
              );
              if (
                r &&
                !busy &&
                !(property.type === "status" && group.value === null)
              )
                void onChange(r, property, group.value);
            }}
          >
            <h3>
              {group.label} <span>{items.length}</span>
            </h3>
            {groupRecords(items, sub, undefined, pages).map((subgroup) => (
              <div key={subgroup.key}>
                {subgroup.label && <h4>{subgroup.label}</h4>}
                {subgroup.records.map((r) => (
                  <article
                    className="db-board-card"
                    key={r.id}
                    data-record-id={r.id}
                    draggable={!busy}
                    onDragStart={(e) =>
                      e.dataTransfer.setData("application/x-trove-record", r.id)
                    }
                  >
                    {renderCard(r)}
                    <label className="db-card-move">
                      Move to
                      <select
                        aria-label={`Move ${pages.find((p) => p.id === r.pageId)?.title ?? "record"} to column`}
                        disabled={busy}
                        value={group.id}
                        onChange={(e) => {
                          const target = groups.find(
                            (g) => g.id === e.target.value,
                          )!;
                          void onChange(r, property, target.value);
                        }}
                      >
                        {groups
                          .filter(
                            (g) =>
                              property.type !== "status" || g.value !== null,
                          )
                          .map((g) => (
                            <option key={g.id} value={g.id}>
                              {g.label}
                            </option>
                          ))}
                      </select>
                    </label>
                  </article>
                ))}
              </div>
            ))}
          </section>
        );
      })}
    </div>
  );
}
function Calendar({
  records,
  view,
  properties,
  pages,
  busy,
  openPage,
  onChange,
  onError,
}: {
  records: D.Record[];
  view: D.View;
  properties: D.PropertyDefinition[];
  pages: D.Page[];
  busy: boolean;
  openPage: (id: string) => void;
  onError: (error: unknown) => void;
  onChange: (
    r: D.Record,
    p: D.PropertyDefinition,
    v: D.RawValue,
  ) => Promise<void>;
}) {
  const [month, setMonth] = useState(() =>
    new Date().toISOString().slice(0, 7),
  );
  const config = view.config as {
      datePropertyId: string;
      weekStartsOn: number;
    },
    property = properties.find((p) => p.id === config.datePropertyId);
  if (!property) return <p>Choose a date property in view settings.</p>;
  const first = new Date(`${month}-01T12:00:00Z`),
    offset = (first.getUTCDay() - config.weekStartsOn + 7) % 7;
  const days = Array.from({ length: 42 }, (_, i) =>
    new Date(
      Date.UTC(first.getUTCFullYear(), first.getUTCMonth(), 1 - offset + i),
    )
      .toISOString()
      .slice(0, 10),
  );
  const dateOf = (r: D.Record) =>
    valueOf(r, property.id) as { start?: string; end?: string } | null;
  const resize = (r: D.Record, edge: "start" | "end", day: string) => {
    try {
      void onChange(
        r,
        property,
        resizeDateRange(valueOf(r, property.id), edge, day),
      );
    } catch (error) {
      onError(error);
    }
  };
  const event = (r: D.Record, day?: string) => (
    <article
      key={r.id}
      data-record-id={r.id}
      className="db-calendar-event"
      draggable={!busy}
      onDragStart={(e) =>
        e.dataTransfer.setData("application/x-trove-record", r.id)
      }
    >
      <button onClick={() => openPage(r.pageId)}>
        {pages.find((p) => p.id === r.pageId)?.title ?? "Untitled"}
      </button>
      <input
        type="date"
        aria-label={`Reschedule ${pages.find((p) => p.id === r.pageId)?.title ?? "record"}`}
        disabled={busy}
        value={calendarDay(valueOf(r, property.id))}
        onChange={(e) => {
          if (e.target.value)
            void onChange(
              r,
              property,
              reschedule(valueOf(r, property.id), e.target.value),
            );
        }}
      />
      {dateOf(r)?.start && (
        <>
          <input
            type="date"
            aria-label={`Range end for ${pages.find((p) => p.id === r.pageId)?.title ?? "record"}`}
            disabled={busy}
            min={calendarDay(valueOf(r, property.id))}
            value={calendarDay(valueOf(r, property.id), "end")}
            onChange={(e) => {
              if (e.target.value) resize(r, "end", e.target.value);
            }}
          />
          {(["start", "end"] as const)
            .filter(
              (edge) =>
                day ===
                (calendarDay(valueOf(r, property.id), edge) ||
                  calendarDay(valueOf(r, property.id))),
            )
            .map((edge) => (
              <button
                key={edge}
                type="button"
                className="db-range-handle"
                draggable={!busy}
                disabled={busy}
                aria-label={`Resize ${edge} of ${pages.find((p) => p.id === r.pageId)?.title ?? "record"}`}
                title={`Drag to change the range ${edge}`}
                onDragStart={(e) => {
                  e.stopPropagation();
                  e.dataTransfer.setData(
                    "application/x-trove-calendar-resize",
                    JSON.stringify({ recordId: r.id, edge }),
                  );
                }}
              >
                {edge === "start" ? "↤" : "↦"}
              </button>
            ))}
        </>
      )}
    </article>
  );
  return (
    <div className="db-calendar">
      <div className="db-calendar-toolbar">
        <label>
          Month
          <input
            type="month"
            aria-label="Calendar month"
            value={month}
            onChange={(e) => {
              if (e.target.value) setMonth(e.target.value);
            }}
          />
        </label>
        <span>
          Drag a record to move it. Drag an end handle or edit its end date to
          resize the range.
        </span>
      </div>
      <div
        className="db-calendar-grid"
        role="grid"
        aria-label={`Calendar ${month}`}
      >
        {Array.from({ length: 7 }, (_, i) => (
          <div className="db-weekday" role="columnheader" key={i}>
            {
              ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][
                (i + config.weekStartsOn) % 7
              ]
            }
          </div>
        ))}
        {days.map((day) => (
          <div
            role="gridcell"
            aria-label={day}
            key={day}
            className={`db-day ${day.startsWith(month) ? "" : "outside-month"}`}
            onDragOver={(e) => {
              if (!busy) e.preventDefault();
            }}
            onDrop={(e) => {
              e.preventDefault();
              const resizing = e.dataTransfer.getData(
                "application/x-trove-calendar-resize",
              );
              if (resizing) {
                try {
                  const { recordId, edge } = JSON.parse(resizing);
                  const r = records.find((r) => r.id === recordId);
                  if (r && !busy && (edge === "start" || edge === "end"))
                    resize(r, edge, day);
                } catch {
                  onError(
                    new Error("The dragged calendar range is unavailable."),
                  );
                }
                return;
              }
              const r = records.find(
                (r) =>
                  r.id === e.dataTransfer.getData("application/x-trove-record"),
              );
              if (r && !busy)
                void onChange(
                  r,
                  property,
                  reschedule(valueOf(r, property.id), day),
                );
            }}
          >
            <time dateTime={day}>{Number(day.slice(-2))}</time>
            {records
              .filter((r) => {
                const date = dateOf(r);
                return (
                  date?.start &&
                  calendarDay(valueOf(r, property.id)) <= day &&
                  (calendarDay(valueOf(r, property.id), "end") ||
                    calendarDay(valueOf(r, property.id))) >= day
                );
              })
              .map((r) => event(r, day))}
          </div>
        ))}
      </div>
      {records.some((r) => !dateOf(r)?.start) && (
        <section className="db-undated">
          <h3>No date</h3>
          {records.filter((r) => !dateOf(r)?.start).map((r) => event(r))}
        </section>
      )}
    </div>
  );
}
function PropertyForm({
  workspace: w,
  collection,
  busy,
  onSave,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  busy: boolean;
  onSave: (command: S.AddProperty) => Promise<void>;
  onError: (e: unknown) => void;
}) {
  const [name, setName] = useState(""),
    [type, setType] = useState<string>("text"),
    [labels, setLabels] = useState("To do, In progress, Done"),
    [target, setTarget] = useState(collection.id),
    [expression, setExpression] = useState(""),
    [relation, setRelation] = useState(""),
    [rollupTarget, setRollupTarget] = useState(""),
    [aggregation, setAggregation] = useState("sum");
  return (
    <form
      className="db-panel db-property-form"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          const optionLabels = labels
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean);
          const optionType = ["select", "multi_select", "status"].includes(
            type,
          );
          if (optionType && !optionLabels.length)
            throw Error("Enter at least one option.");
          if (type === "status" && optionLabels.length < 2)
            throw Error("A status needs an open option and a done option.");
          const [propertyId, ...optionIds] = await ids(
            w,
            1 + (optionType ? optionLabels.length : 0),
          );
          let config: D.RawValue = {};
          if (type === "number") config = { format: "decimal" };
          if (optionType) {
            config = {
              options: optionLabels.map((label, i) => ({
                id: optionIds[i],
                label,
                ...(type === "status"
                  ? {
                      group:
                        i === optionLabels.length - 1
                          ? "done"
                          : i === 0
                            ? "not_started"
                            : "in_progress",
                    }
                  : {}),
              })),
              ...(type === "status"
                ? {
                    defaultOptionId: optionIds[0],
                    reopenOptionId: optionIds[0],
                    doneOptionId: optionIds.at(-1)!,
                  }
                : {}),
            };
          }
          if (type === "relation")
            config = {
              targetCollectionId: target,
              bidirectionalPropertyId: null,
            };
          if (type === "formula")
            config = {
              sourceType: "trove.formula",
              sourceVersion: 1,
              readableText: expression,
              reason: "unsupported_behavior",
              raw: {
                originalExpression: expression,
                translatedExpression: expression,
              },
            };
          if (type === "rollup")
            config = {
              sourceType: "trove.rollup",
              sourceVersion: 1,
              readableText: `${aggregation} rollup`,
              reason: "unsupported_behavior",
              raw: {
                relationPropertyId: relation,
                targetPropertyId: rollupTarget,
                aggregation,
              },
            };
          await onSave({
            kind: "collection.addProperty",
            collectionId: collection.id,
            propertyId,
            name,
            type,
            schemaVersion: 1,
            config,
          });
        } catch (error) {
          onError(error);
        }
      }}
    >
      <label>
        Name
        <input
          aria-label="Property name"
          value={name}
          required
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <label>
        Type
        <select
          aria-label="Property type"
          value={type}
          onChange={(e) => setType(e.target.value)}
        >
          {[
            ...editableTypes,
            "formula",
            "rollup",
            "created_time",
            "last_edited_time",
            "unique_id",
          ].map((t) => (
            <option key={t} value={t}>
              {t.replace("_", " ")}
            </option>
          ))}
        </select>
      </label>
      {type === "relation" && (
        <label>
          Related collection
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            {w.collections.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </select>
        </label>
      )}
      {type === "formula" && (
        <label>
          Expression
          <input
            required
            value={expression}
            onChange={(e) => setExpression(e.target.value)}
            placeholder='prop("property-id") * 2'
          />
          <select
            value=""
            onChange={(e) =>
              setExpression((x) => x + `prop("${e.target.value}")`)
            }
          >
            <option value="">Insert a property…</option>
            {w.properties
              .filter((p) => p.collectionId === collection.id)
              .map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
          </select>
          <small>
            Arithmetic, comparisons, ifElse, min/max, text functions, now and
            today.
          </small>
        </label>
      )}
      {type === "rollup" && (
        <>
          <label>
            Relation
            <select
              value={relation}
              onChange={(e) => setRelation(e.target.value)}
            >
              <option value="">Choose relation</option>
              {w.properties
                .filter(
                  (p) =>
                    p.collectionId === collection.id && p.type === "relation",
                )
                .map((p) => (
                  <option value={p.id} key={p.id}>
                    {p.name}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Related property
            <select
              value={rollupTarget}
              onChange={(e) => setRollupTarget(e.target.value)}
            >
              <option value="">Choose property</option>
              {w.properties
                .filter(
                  (p) =>
                    p.collectionId ===
                    (
                      w.properties.find((p) => p.id === relation)?.config as {
                        targetCollectionId?: string;
                      }
                    )?.targetCollectionId,
                )
                .map((p) => (
                  <option value={p.id} key={p.id}>
                    {p.name}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Calculation
            <select
              value={aggregation}
              onChange={(e) => setAggregation(e.target.value)}
            >
              {["sum", "average", "min", "max", "count", "show_original"].map(
                (s) => (
                  <option key={s}>{s}</option>
                ),
              )}
            </select>
          </label>
        </>
      )}
      {["select", "multi_select", "status"].includes(type) && (
        <label>
          Options, separated by commas
          <input
            aria-label="Property options"
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
          />
          {type === "status" && <small>First is reopened; last is done.</small>}
        </label>
      )}
      <button disabled={busy || !name.trim()} className="primary">
        Create property
      </button>
    </form>
  );
}
function ViewSettings({
  view,
  properties,
  workspace: w,
  busy,
  onSave,
}: {
  view: D.View;
  properties: D.PropertyDefinition[];
  workspace: WorkspaceSnapshot;
  busy: boolean;
  onSave: (v: D.View) => void;
}) {
  const [draft, setDraft] = useState(view);
  const [widthDrafts, setWidthDrafts] = useState<Record<string, string>>({});
  const setQuery = (q: Partial<D.ViewQuery>) =>
    setDraft({ ...draft, query: { ...draft.query, ...q } });
  const cfg = draft.config as any;
  const setConfig = (c: object) =>
    setDraft({ ...draft, config: { ...cfg, ...c } });
  const moveProperty = (index: number, direction: number) => {
    const next = [...draft.query.visiblePropertyIds],
      destination = index + direction;
    if (destination < 0 || destination >= next.length) return;
    [next[index], next[destination]] = [next[destination], next[index]];
    setQuery({ visiblePropertyIds: next });
  };
  return (
    <form
      className="db-panel db-view-settings"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(draft);
      }}
    >
      <div className="db-settings-row">
        <label>
          View name
          <input
            aria-label="View name"
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
        </label>
        <label>
          Location
          <select
            aria-label="View location"
            value={draft.owningPageId}
            onChange={(e) =>
              setDraft({
                ...draft,
                owningPageId: e.target.value,
                containerId: null,
              })
            }
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
        {draft.type === "board" && (
          <label>
            Board columns
            <select
              aria-label="Board grouping property"
              value={cfg.groupPropertyId}
              onChange={(e) => setConfig({ groupPropertyId: e.target.value })}
            >
              {properties
                .filter((p) =>
                  ["select", "status", "checkbox"].includes(p.type),
                )
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
            </select>
            <span>
              <input
                type="checkbox"
                checked={cfg.hideEmptyGroups}
                onChange={(e) =>
                  setConfig({ hideEmptyGroups: e.target.checked })
                }
              />{" "}
              Hide empty groups
            </span>
          </label>
        )}
        {draft.type === "gallery" && (
          <>
            <label>
              Cover
              <select
                value={cfg.coverPropertyId ?? ""}
                onChange={(e) =>
                  setConfig({ coverPropertyId: e.target.value || null })
                }
              >
                <option value="">No cover</option>
                {properties
                  .filter((p) => p.type === "files")
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
              </select>
            </label>
            <label>
              Card size
              <select
                value={cfg.cardSize}
                onChange={(e) => setConfig({ cardSize: e.target.value })}
              >
                {["small", "medium", "large"].map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
            </label>
          </>
        )}
        {draft.type === "timeline" && (
          <>
            <label>
              <input
                type="checkbox"
                aria-label="Show timeline dependencies"
                checked={cfg.showDependencies !== false}
                onChange={(event) =>
                  setConfig({ showDependencies: event.target.checked })
                }
              />
              Show dependencies
            </label>
            <label>
              Timeline date
              <select
                value={cfg.datePropertyId}
                onChange={(e) => setConfig({ datePropertyId: e.target.value })}
              >
                {properties
                  .filter((p) => p.type === "date")
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
              </select>
            </label>
            <label>
              Scale
              <select
                value={cfg.scale}
                onChange={(e) => setConfig({ scale: e.target.value })}
              >
                {["day", "week", "month", "quarter", "year"].map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
            </label>
          </>
        )}
        {draft.type === "calendar" && (
          <>
            <label>
              Calendar date
              <select
                aria-label="Calendar date property"
                value={cfg.datePropertyId}
                onChange={(e) => setConfig({ datePropertyId: e.target.value })}
              >
                {properties
                  .filter((p) => p.type === "date")
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
              </select>
            </label>
            <label>
              Week starts
              <select
                value={cfg.weekStartsOn}
                onChange={(e) =>
                  setConfig({ weekStartsOn: Number(e.target.value) })
                }
              >
                {[
                  "Sunday",
                  "Monday",
                  "Tuesday",
                  "Wednesday",
                  "Thursday",
                  "Friday",
                  "Saturday",
                ].map((day, i) => (
                  <option value={i} key={day}>
                    {day}
                  </option>
                ))}
              </select>
            </label>
          </>
        )}
        {draft.type === "list" && (
          <label>
            <input
              type="checkbox"
              checked={cfg.showPageIcon}
              onChange={(e) => setConfig({ showPageIcon: e.target.checked })}
            />{" "}
            Show page icons
          </label>
        )}
      </div>
      <fieldset>
        <legend>Visible properties</legend>
        <div className="db-checks">
          {properties.map((p) => (
            <label key={p.id}>
              <input
                type="checkbox"
                checked={draft.query.visiblePropertyIds.includes(p.id)}
                onChange={(e) =>
                  setQuery({
                    visiblePropertyIds: e.target.checked
                      ? [...draft.query.visiblePropertyIds, p.id]
                      : draft.query.visiblePropertyIds.filter(
                          (id) => id !== p.id,
                        ),
                  })
                }
              />
              {p.name}
            </label>
          ))}
        </div>
        <ol className="db-property-order" aria-label="Property display order">
          {draft.query.visiblePropertyIds.map((id, index) => {
            const property = properties.find((p) => p.id === id);
            if (!property) return null;
            const width =
              (cfg.columnWidths ?? []).find(
                (c: { propertyId: string }) => c.propertyId === id,
              )?.pixels ?? 220;
            return (
              <li key={id}>
                <span>{property.name}</span>
                <button
                  type="button"
                  aria-label={`Move ${property.name} earlier`}
                  disabled={busy || index === 0}
                  onClick={() => moveProperty(index, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  aria-label={`Move ${property.name} later`}
                  disabled={
                    busy || index === draft.query.visiblePropertyIds.length - 1
                  }
                  onClick={() => moveProperty(index, 1)}
                >
                  ↓
                </button>
                {draft.type === "table" && (
                  <label>
                    Width
                    <input
                      type="number"
                      aria-label={`${property.name} column width`}
                      min={40}
                      max={2000}
                      step={1}
                      required
                      value={widthDrafts[id] ?? String(width)}
                      disabled={busy}
                      onChange={(e) => {
                        const raw = e.target.value,
                          pixels = Number(raw);
                        setWidthDrafts({ ...widthDrafts, [id]: raw });
                        if (
                          Number.isInteger(pixels) &&
                          pixels >= 40 &&
                          pixels <= 2000
                        )
                          setConfig({
                            columnWidths: [
                              ...(cfg.columnWidths ?? []).filter(
                                (c: { propertyId: string }) =>
                                  c.propertyId !== id,
                              ),
                              { propertyId: id, pixels },
                            ],
                          });
                      }}
                    />
                  </label>
                )}
              </li>
            );
          })}
        </ol>
      </fieldset>
      <div className="db-settings-row">
        {(["groupByPropertyId", "subgroupByPropertyId"] as const).map(
          (key, i) => (
            <label key={key}>
              {i ? "Subgroup" : "Group"}
              <select
                aria-label={i ? "Subgroup records" : "Group records"}
                value={draft.query[key] ?? ""}
                onChange={(e) => setQuery({ [key]: e.target.value || null })}
              >
                <option value="">None</option>
                {properties.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          ),
        )}
      </div>
      <fieldset>
        <legend>Sort</legend>
        {draft.query.sorts.map((sort, i) => (
          <div className="db-settings-row" key={i}>
            <select
              aria-label={`Sort property ${i + 1}`}
              value={sort.propertyId}
              onChange={(e) =>
                setQuery({
                  sorts: draft.query.sorts.map((s, n) =>
                    n === i ? { ...s, propertyId: e.target.value } : s,
                  ),
                })
              }
            >
              {properties.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <select
              aria-label={`Sort direction ${i + 1}`}
              value={sort.direction}
              onChange={(e) =>
                setQuery({
                  sorts: draft.query.sorts.map((s, n) =>
                    n === i
                      ? { ...s, direction: e.target.value as "asc" | "desc" }
                      : s,
                  ),
                })
              }
            >
              <option value="asc">Ascending</option>
              <option value="desc">Descending</option>
            </select>
            <button
              type="button"
              onClick={() =>
                setQuery({ sorts: draft.query.sorts.filter((_, n) => n !== i) })
              }
            >
              Remove
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={() =>
            setQuery({
              sorts: [
                ...draft.query.sorts,
                { propertyId: properties[0].id, direction: "asc" },
              ],
            })
          }
        >
          ＋ Sort
        </button>
      </fieldset>
      <fieldset>
        <legend>Filter</legend>
        <FilterBuilder
          filter={draft.query.filter}
          properties={properties}
          onChange={(filter) => setQuery({ filter })}
        />
      </fieldset>
      <button className="primary" disabled={busy || !draft.name.trim()}>
        Save view
      </button>
    </form>
  );
}
function filterDefault(p: D.PropertyDefinition): D.RawValue {
  switch (p.type) {
    case "number":
      return { decimal: "0", format: "decimal" };
    case "checkbox":
      return true;
    case "date":
      return {
        start: new Date().toISOString().slice(0, 10),
        end: null,
        timezone: "UTC",
        hasTime: false,
      };
    case "text":
      return [];
    case "select":
    case "status":
      return options(p)[0]?.id ?? null;
    case "multi_select":
      return [];
    default:
      return "";
  }
}
function FilterBuilder({
  filter,
  properties,
  onChange,
  depth = 0,
}: {
  filter: D.Filter | null;
  properties: D.PropertyDefinition[];
  onChange: (f: D.Filter | null) => void;
  depth?: number;
}) {
  const scalarProps = properties.filter(
    (p) => ["title", ...editableTypes].includes(p.type) && p.type !== "files",
  );
  const fresh = (): D.Filter => ({
    op: "eq",
    propertyId: scalarProps[0].id,
    value: filterDefault(scalarProps[0]),
  });
  if (!filter)
    return (
      <button
        type="button"
        onClick={() => onChange({ op: "and", terms: [fresh()] })}
      >
        ＋ Add filter
      </button>
    );
  if ("terms" in filter)
    return (
      <div className="db-filter-group">
        <div className="db-settings-row">
          <select
            aria-label="Combine filters"
            value={filter.op}
            onChange={(e) =>
              onChange({ ...filter, op: e.target.value as "and" | "or" })
            }
          >
            <option value="and">All conditions (AND)</option>
            <option value="or">Any condition (OR)</option>
          </select>
          <button type="button" onClick={() => onChange(null)}>
            Remove group
          </button>
        </div>
        {filter.terms.map((term, i) => (
          <FilterBuilder
            key={i}
            depth={depth + 1}
            filter={term}
            properties={properties}
            onChange={(f) =>
              onChange({
                ...filter,
                terms: f
                  ? filter.terms.map((v, n) => (n === i ? f : v))
                  : filter.terms.filter((_, n) => n !== i),
              })
            }
          />
        ))}
        <div className="db-settings-row">
          <button
            type="button"
            onClick={() =>
              onChange({ ...filter, terms: [...filter.terms, fresh()] })
            }
          >
            ＋ Condition
          </button>
          {depth < 4 && (
            <button
              type="button"
              onClick={() =>
                onChange({
                  ...filter,
                  terms: [...filter.terms, { op: "or", terms: [fresh()] }],
                })
              }
            >
              ＋ Group
            </button>
          )}
        </div>
      </div>
    );
  const property = properties.find((p) => p.id === filter.propertyId)!;
  let input: React.ReactNode;
  const op = filter.op;
  if (property.type === "status" || property.type === "select")
    input = (
      <select
        aria-label="Filter value"
        value={String(filter.value ?? "")}
        onChange={(e) => onChange({ ...filter, value: e.target.value })}
      >
        {options(property).map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </select>
    );
  else if (property.type === "checkbox")
    input = (
      <select
        aria-label="Filter value"
        value={String(filter.value)}
        onChange={(e) =>
          onChange({ ...filter, value: e.target.value === "true" })
        }
      >
        <option value="true">Checked</option>
        <option value="false">Unchecked</option>
      </select>
    );
  else
    input = (
      <input
        aria-label="Filter value"
        value={
          op === "contains"
            ? String(filter.value ?? "")
            : displayValue(filter.value ?? null, property)
        }
        onChange={(e) => {
          let value: D.RawValue = e.target.value;
          if (op !== "contains") {
            if (property.type === "number")
              value = { decimal: e.target.value, format: "decimal" };
            if (property.type === "text") value = [{ text: e.target.value }];
            if (property.type === "date")
              value = {
                start: e.target.value,
                end: null,
                timezone: "UTC",
                hasTime: false,
              };
            if (property.type === "multi_select")
              value = e.target.value.split(",").filter(Boolean);
          }
          onChange({ ...filter, value });
        }}
      />
    );
  return (
    <div className="db-filter-condition">
      <select
        aria-label="Filter property"
        value={property.id}
        onChange={(e) => {
          const p = properties.find((p) => p.id === e.target.value)!;
          onChange({ op: "eq", propertyId: p.id, value: filterDefault(p) });
        }}
      >
        {scalarProps.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
      <select
        aria-label="Filter comparison"
        value={op}
        onChange={(e) => {
          const op = e.target.value as typeof filter.op;
          onChange({
            ...filter,
            op,
            value: op === "contains" ? "" : filterDefault(property),
          });
        }}
      >
        {["eq", "ne", "lt", "lte", "gt", "gte", "contains", "is_empty"].map(
          (op) => (
            <option key={op} value={op}>
              {
                (
                  {
                    eq: "is",
                    ne: "is not",
                    lt: "less than",
                    lte: "at most",
                    gt: "greater than",
                    gte: "at least",
                    contains: "contains",
                    is_empty: "is empty",
                  } as Record<string, string>
                )[op]
              }
            </option>
          ),
        )}
      </select>
      {op !== "is_empty" && input}
      <button type="button" onClick={() => onChange(null)}>
        Remove
      </button>
    </div>
  );
}
