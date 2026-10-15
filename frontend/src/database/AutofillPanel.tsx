import { useEffect, useRef, useState } from "react";
import {
  dispatch,
  envelope,
  type D,
  type S,
  type WorkspaceSnapshot,
} from "../api";
import { p2FieldTypes } from "./P2Model";
import { displayValue } from "./model";

type Cell = S.AutofillCell;
type Autofill = S.AutofillBatch;
export function AutofillPanel({
  workspace: w,
  collection,
  records,
  onCommitted,
  onError,
  openPage,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  records: D.Record[];
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
  openPage: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(false),
    [busy, setBusy] = useState(false);
  const [rowIds, setRows] = useState<string[]>([]),
    [sourceIds, setSources] = useState<string[]>([]),
    [targetIds, setTargets] = useState<string[]>([]);
  const [instructions, setInstructions] = useState(
      "Summarize or extract only facts supported by the selected fields. Return no value when evidence is missing.",
    ),
    [overwrites, setOverwrites] = useState(false);
  const [models, setModels] = useState<{ id: string; name: string }[]>([]),
    [model, setModel] = useState("");
  const [history, setHistory] = useState<Autofill[]>([]),
    [current, setCurrent] = useState<Autofill | null>(null),
    [selected, setSelected] = useState<string[]>([]),
    [overwriteIds, setOverwriteIds] = useState<string[]>([]);
  const epoch = useRef(0);
  const properties = w.properties.filter(
      (p) => p.collectionId === collection.id,
    ),
    fields = properties.filter((p) => p2FieldTypes.includes(p.type));
  const title = (record: D.Record) =>
    w.pages.find((p) => p.id === record.pageId)?.title ?? "Untitled record";
  const label = (cell: Cell) =>
    records.find((r) => r.id === cell.recordId)
      ? title(records.find((r) => r.id === cell.recordId)!)
      : cell.recordId;
  function choose(next: Autofill) {
    setCurrent(next);
    setSelected(
      next.cells
        .filter((c) => c.state === "value" && !c.overwrite)
        .map((c) => c.id),
    );
    setOverwriteIds([]);
  }
  const toggle = (items: string[], id: string) =>
    items.includes(id) ? items.filter((x) => x !== id) : [...items, id];
  useEffect(() => {
    epoch.current++;
    setCurrent(null);
    setHistory([]);
    setRows([]);
    setSources([]);
    setTargets([]);
    setSelected([]);
    setOverwriteIds([]);
  }, [w.workspaceId, collection.id]);
  useEffect(() => {
    if (!expanded) return;
    let live = true;
    dispatch<Autofill[]>("autofill.list", {
      ...envelope(w),
      collectionId: collection.id,
    })
      .then((r) => {
        if (live) setHistory(r);
      })
      .catch(onError);
    dispatch<{
      packs: {
        pack: {
          id: string;
          name: string;
          kind: string;
          qualificationOnly?: boolean;
        };
        state: string;
      }[];
    }>("models.list")
      .then((r) => {
        if (!live) return;
        const ready = r.packs
          .filter(
            (p) =>
              p.state === "ready" &&
              p.pack.kind === "chat" &&
              !p.pack.qualificationOnly,
          )
          .map((p) => p.pack);
        setModels(ready);
        setModel((old) =>
          ready.some((p) => p.id === old) ? old : (ready[0]?.id ?? ""),
        );
      })
      .catch(onError);
    return () => {
      live = false;
    };
  }, [expanded, w.workspaceId, collection.id]);
  useEffect(() => {
    if (current?.state !== "running") return;
    let live = true,
      timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await dispatch<Autofill>("autofill.get", {
          ...envelope(w),
          batchId: current.id,
        });
        if (!live) return;
        setCurrent(next);
        if (next.state === "running") timer = setTimeout(poll, 750);
        else {
          choose(next);
          setHistory((old) => [next, ...old.filter((b) => b.id !== next.id)]);
        }
      } catch (e) {
        if (live) {
          onError(e);
          timer = setTimeout(poll, 2000);
        }
      }
    };
    timer = setTimeout(poll, 500);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [current?.id, current?.state, w.workspaceId]);
  async function run(action: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    try {
      await action();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  }
  async function create() {
    const expected = epoch.current;
    const next = await dispatch<Autofill>("autofill.create", {
      ...envelope(w),
      input: {
        collectionId: collection.id,
        recordIds: rowIds,
        sourcePropertyIds: sourceIds,
        targetPropertyIds: targetIds,
        instructions,
        modelPackId: model,
        includeNonempty: overwrites,
      } satisfies S.AutofillInput,
    });
    if (epoch.current !== expected) return;
    choose(next);
    setHistory((old) => [next, ...old]);
  }
  async function generate() {
    if (!current) return;
    const expected = epoch.current;
    const next = await dispatch<Autofill>("autofill.generate", {
      ...envelope(w),
      batchId: current.id,
    });
    if (epoch.current === expected) setCurrent(next);
  }
  async function apply() {
    if (!current) return;
    const id = current.id,
      expected = epoch.current;
    await dispatch("autofill.apply", {
      ...envelope(w),
      batchId: id,
      cellIds: selected,
      overwriteCellIds: overwriteIds,
    });
    await onCommitted();
    const next = await dispatch<Autofill>("autofill.get", {
      ...envelope(w),
      batchId: id,
    });
    if (epoch.current === expected) choose(next);
  }
  async function undo() {
    if (!current) return;
    const preview = await dispatch<S.OperationPreviewUndoResult>(
      "operations.previewUndo",
      { ...envelope(w), operationId: current.id },
    );
    await dispatch("batch", {
      ...envelope(w),
      mutation: {
        idempotencyKey: `undo-autofill-${current.id}`,
        scopeGrantId: w.scopeGrantId,
        expectedRevisions: preview.expectedRevisions,
      },
      commands: [preview.command],
    });
    await onCommitted();
  }
  const running = current?.state === "running";
  const pending =
    current?.cells.filter((c) => c.state === "pending").length ?? 0;
  const value = (v: D.RawValue, p: D.PropertyDefinition) =>
    v === null ? "No value" : displayValue(v, p) || JSON.stringify(v);
  return (
    <section className="p2-database" aria-label="AI database autofill">
      <details
        open={expanded}
        onToggle={(e) => setExpanded(e.currentTarget.open)}
      >
        <summary>AI autofill</summary>
        {expanded && (
          <>
            <p>
              Choose rows and fields, generate local suggestions, then review
              the values to apply. Each batch keeps its source and schema
              revisions.
            </p>
            <fieldset disabled={busy || running}>
              <legend>New autofill batch</legend>
              <fieldset>
                <legend>Rows in this window</legend>
                {records.length ? (
                  records.map((r) => (
                    <label key={r.id}>
                      <input
                        type="checkbox"
                        checked={rowIds.includes(r.id)}
                        onChange={() => setRows(toggle(rowIds, r.id))}
                      />
                      {title(r)}
                    </label>
                  ))
                ) : (
                  <p>No records in this window.</p>
                )}
              </fieldset>
              <fieldset>
                <legend>Source fields</legend>
                {properties
                  .filter(
                    (p) => p.type === "title" || p2FieldTypes.includes(p.type),
                  )
                  .map((p) => (
                    <label key={p.id}>
                      <input
                        type="checkbox"
                        checked={sourceIds.includes(p.id)}
                        onChange={() => setSources(toggle(sourceIds, p.id))}
                      />
                      {p.name}
                    </label>
                  ))}
              </fieldset>
              <fieldset>
                <legend>Target fields</legend>
                {fields.map((p) => (
                  <label key={p.id}>
                    <input
                      type="checkbox"
                      checked={targetIds.includes(p.id)}
                      onChange={() => setTargets(toggle(targetIds, p.id))}
                    />
                    {p.name}
                  </label>
                ))}
              </fieldset>
              <label>
                Instructions
                <textarea
                  aria-label="Autofill instructions"
                  maxLength={4000}
                  value={instructions}
                  onChange={(e) => setInstructions(e.target.value)}
                />
              </label>
              <label>
                Local model
                <select
                  aria-label="Autofill model"
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                >
                  <option value="">Choose an installed model</option>
                  {models.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </label>
              {!models.length && (
                <p>
                  Install an instruction model in Models to generate
                  suggestions.
                </p>
              )}
              <label>
                <input
                  type="checkbox"
                  checked={overwrites}
                  onChange={(e) => setOverwrites(e.target.checked)}
                />
                Also generate suggestions for existing values; review these
                separately
              </label>
              <p>
                {rowIds.length * targetIds.length} cells selected (maximum 128).
              </p>
              <button
                disabled={
                  !rowIds.length ||
                  !sourceIds.length ||
                  !targetIds.length ||
                  !model ||
                  !instructions.trim() ||
                  rowIds.length * targetIds.length > 128
                }
                onClick={() => void run(create)}
              >
                Prepare autofill
              </button>
            </fieldset>
            <label>
              Batch history
              <select
                aria-label="Autofill history"
                disabled={busy || running}
                value={current?.id ?? ""}
                onChange={(e) => {
                  const b = history.find((x) => x.id === e.target.value);
                  if (b)
                    void run(async () =>
                      choose(
                        await dispatch<Autofill>("autofill.get", {
                          ...envelope(w),
                          batchId: b.id,
                        }),
                      ),
                    );
                }}
              >
                <option value="">Choose a saved batch</option>
                {history.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.createdAt} · {b.state}
                  </option>
                ))}
              </select>
            </label>
            {current && (
              <section aria-label="Autofill review">
                <h3>Review autofill</h3>
                <p role="status">
                  {current.state} · {current.cells.length - pending}/
                  {current.cells.length} cells finished
                </p>
                <p>
                  Model:{" "}
                  {models.find((m) => m.id === current.input.modelPackId)
                    ?.name ?? current.input.modelPackId}{" "}
                  · prompt v{current.promptVersion} · source snapshots retained
                </p>
                {current.error && <p role="alert">{current.error}</p>}
                {pending > 0 && !running && current.state !== "applied" && (
                  <button disabled={busy} onClick={() => void run(generate)}>
                    {current.state === "draft"
                      ? "Generate suggestions"
                      : "Resume remaining cells"}
                  </button>
                )}
                {running && (
                  <button
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        await dispatch("autofill.cancel", {
                          ...envelope(w),
                          batchId: current.id,
                        });
                      })
                    }
                  >
                    Cancel generation
                  </button>
                )}
                {[false, true].map((overwrite) => (
                  <section
                    key={String(overwrite)}
                    aria-label={
                      overwrite ? "Review overwrites" : "Fill blank fields"
                    }
                  >
                    <h4>
                      {overwrite ? "Review overwrites" : "Fill blank fields"}
                    </h4>
                    {current.cells
                      .filter((c) => c.overwrite === overwrite)
                      .map((c) => (
                        <article key={c.id}>
                          <strong>
                            {label(c)} · {c.property.name}
                          </strong>
                          <button onClick={() => openPage(c.pageId)}>
                            Open source row
                          </button>
                          <p>
                            Before:{" "}
                            {c.beforePresent
                              ? value(c.before, c.property)
                              : "Missing"}
                          </p>
                          <p>
                            Suggestion:{" "}
                            {c.state === "value"
                              ? value(c.value, c.property)
                              : c.state}{" "}
                            {c.reason && `— ${c.reason}`}
                          </p>
                          <details>
                            <summary>Sources and provenance</summary>
                            <pre>
                              {JSON.stringify(
                                {
                                  model: current.model,
                                  sources: c.sources,
                                  property: c.property,
                                  generatedAt: c.generatedAt,
                                  sourceRevisions: current.expectedRevisions,
                                },
                                null,
                                2,
                              )}
                            </pre>
                          </details>
                          {c.state === "value" &&
                            current.state !== "applied" && (
                              <label>
                                <input
                                  type="checkbox"
                                  disabled={busy || running}
                                  checked={selected.includes(c.id)}
                                  onChange={() => {
                                    setSelected(toggle(selected, c.id));
                                    if (overwrite)
                                      setOverwriteIds(
                                        toggle(overwriteIds, c.id),
                                      );
                                  }}
                                />
                                {overwrite
                                  ? `Approve overwrite of ${label(c)} · ${c.property.name}`
                                  : `Apply ${label(c)} · ${c.property.name}`}
                              </label>
                            )}
                        </article>
                      ))}
                  </section>
                ))}
                {!running && current.state !== "applied" && (
                  <button
                    disabled={busy || !selected.length}
                    onClick={() => void run(apply)}
                  >
                    Apply {selected.length} reviewed values atomically
                  </button>
                )}
                {current.state === "applied" && (
                  <>
                    <p>
                      {current.appliedIds.length} values applied. Generation
                      history and provenance are retained.
                    </p>
                    <button disabled={busy} onClick={() => void run(undo)}>
                      Undo autofill values
                    </button>
                  </>
                )}
              </section>
            )}
          </>
        )}
      </details>
    </section>
  );
}
