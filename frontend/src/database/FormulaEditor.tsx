import { useEffect, useMemo, useRef, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  pageGuards,
  type D,
  type WorkspaceSnapshot,
} from "../api";
import { guard, uniqueGuards } from "./service";
type Registry = {
  version: number;
  functions: { name: string; sinceVersion: number; signature: string }[];
  operators: string[];
  limits: Record<string, number>;
  differences: string[];
};
type Preview = {
  status: "computed" | "error";
  value: unknown;
  resultType: string;
  diagnostic?: string;
  dependencies: string[];
  computedAt: string;
  timezone: string;
};
export function FormulaEditor({
  workspace: w,
  collection,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const [open, setOpen] = useState(false);
  const formulas = w.properties.filter(
    (p) => p.collectionId === collection.id && p.type === "formula",
  );
  return formulas.length ? (
    <details
      className="db-formula-editor"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>Formula editor and compatibility</summary>
      {open && (
        <FormulaControls
          key={collection.id}
          workspace={w}
          collection={collection}
          formulas={formulas}
          onCommitted={onCommitted}
          onError={onError}
        />
      )}
    </details>
  ) : null;
}
function FormulaControls({
  workspace: w,
  collection,
  formulas,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  collection: D.Collection;
  formulas: D.PropertyDefinition[];
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const [selected, setSelected] = useState(formulas[0].id),
    [registry, setRegistry] = useState<Registry>();
  useEffect(() => {
    let live = true;
    void dispatch<Registry>("databaseFormulaRegistry", envelope(w))
      .then((result) => {
        if (live) setRegistry(result);
      })
      .catch(onError);
    return () => {
      live = false;
    };
  }, [w.workspaceId, onError]);
  const property = formulas.find((p) => p.id === selected) ?? formulas[0];
  return (
    <>
      <label>
        Formula property
        <select
          aria-label="Formula property"
          value={property.id}
          onChange={(event) => setSelected(event.target.value)}
        >
          {formulas.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <FormulaDraft
        key={`${property.id}:${property.revision}`}
        workspace={w}
        property={property}
        collection={collection}
        onCommitted={onCommitted}
        onError={onError}
      />
      {registry && (
        <details>
          <summary>
            Supported evaluator v{registry.version} functions and limits
          </summary>
          <p>Operators: {registry.operators.join(" ")}</p>
          <ul>
            {registry.functions.map((fn) => (
              <li key={fn.name}>
                <code>{fn.signature}</code> (since v{fn.sinceVersion})
              </li>
            ))}
          </ul>
          <p>
            {Object.entries(registry.limits)
              .map(([key, value]) => `${key}: ${value}`)
              .join(" · ")}
          </p>
          <ul>
            {registry.differences.map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </details>
      )}
    </>
  );
}
function FormulaDraft({
  workspace: w,
  property,
  collection,
  onCommitted,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  property: D.PropertyDefinition;
  collection: D.Collection;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const raw = (property.config as { raw?: Record<string, unknown> }).raw ?? {};
  const [expression, setExpression] = useState(
      String(raw.translatedExpression ?? raw.originalExpression ?? ""),
    ),
    [at, setAt] = useState(new Date().toISOString()),
    [zone, setZone] = useState(
      Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    ),
    [sampleQuery, setSampleQuery] = useState("");
  const titles = useMemo(
    () => new Map(w.pages.map((p) => [p.id, p.title])),
    [w.pages],
  );
  const records = w.records.filter((r) => r.collectionId === collection.id);
  const [sample, setSample] = useState(records[0]?.id ?? ""),
    [preview, setPreview] = useState<{ key: string; result: Preview }>(),
    [busy, setBusy] = useState(false);
  const request = useRef(0),
    key = JSON.stringify([expression, sample, at, zone, w.snapshotCursor]);
  const shown = records
    .filter((r) =>
      (titles.get(r.pageId) ?? "")
        .toLocaleLowerCase()
        .includes(sampleQuery.toLocaleLowerCase()),
    )
    .slice(0, 100);
  const valid = preview?.key === key && preview.result.status === "computed";
  const properties = w.properties.filter(
    (p) => p.collectionId === collection.id,
  );
  async function runPreview() {
    const sequence = ++request.current;
    setBusy(true);
    try {
      const result = await dispatch<Preview>("databaseFormulaPreview", {
        ...envelope(w),
        propertyId: property.id,
        recordId: sample,
        expression,
        evaluatorVersion: 2,
        at,
        timezone: zone,
      });
      if (sequence === request.current) setPreview({ key, result });
    } catch (error) {
      onError(error);
    } finally {
      if (sequence === request.current) setBusy(false);
    }
  }
  async function save() {
    if (!valid || !preview) return;
    setBusy(true);
    try {
      const page = w.pages.find((p) => p.id === collection.owningPageId);
      if (!page) throw Error("Database page unavailable.");
      const dependencies = preview.result.dependencies.map((id) => {
        const p = properties.find((p) => p.id === id);
        if (!p)
          throw Error(
            "Formula dependency unavailable. Refresh and preview again.",
          );
        return guard("property", id, p.revision);
      });
      await batch(
        w,
        [
          {
            kind: "property.setFormula",
            propertyId: property.id,
            expression,
            evaluatorVersion: 2,
          },
        ],
        uniqueGuards([
          ...pageGuards(w, page),
          guard("collection", collection.id, collection.revision),
          guard("property", property.id, property.revision),
          ...dependencies,
        ]),
      );
      await onCommitted();
    } catch (error) {
      onError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div>
      <p>
        Stored evaluator: v{String(raw.evaluatorVersion ?? 1)}. Saving
        explicitly selects v2; imported source and captured values remain
        unchanged.
      </p>
      <label>
        Expression
        <textarea
          aria-label="Formula expression"
          value={expression}
          maxLength={8192}
          onChange={(event) => setExpression(event.target.value)}
        />
      </label>
      <label>
        Insert property
        <select
          aria-label="Insert formula property"
          value=""
          onChange={(event) =>
            setExpression(expression + `prop("${event.target.value}")`)
          }
        >
          <option value="">Choose a property…</option>
          {properties
            .filter((p) => p.id !== property.id)
            .map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.type})
              </option>
            ))}
        </select>
      </label>
      <div className="db-settings-row">
        <label>
          Find sample record
          <input
            aria-label="Find formula sample"
            value={sampleQuery}
            onChange={(event) => setSampleQuery(event.target.value)}
          />
        </label>
        <label>
          Sample record
          <select
            aria-label="Formula sample record"
            value={sample}
            onChange={(event) => setSample(event.target.value)}
          >
            <option value="">Choose a record</option>
            {!shown.some((r) => r.id === sample) && sample && (
              <option value={sample}>
                {titles.get(
                  records.find((r) => r.id === sample)?.pageId ?? "",
                ) ?? "Selected record"}
              </option>
            )}
            {shown.map((r) => (
              <option key={r.id} value={r.id}>
                {titles.get(r.pageId) ?? "Untitled"}
              </option>
            ))}
          </select>
        </label>
        <small>First 100 matching records; narrow the sample search.</small>
        <label>
          Preview instant
          <input
            aria-label="Formula preview instant"
            value={at}
            onChange={(event) => setAt(event.target.value)}
          />
        </label>
        <label>
          Time zone
          <input
            aria-label="Formula time zone"
            value={zone}
            onChange={(event) => setZone(event.target.value)}
          />
        </label>
      </div>
      <button
        disabled={busy || !sample || !expression}
        onClick={() => void runPreview()}
      >
        Preview formula v2
      </button>
      <button disabled={busy || !valid} onClick={() => void save()}>
        Save formula v2
      </button>
      {!records.length && (
        <p>Add a sample record to preview and save the formula.</p>
      )}
      {preview?.key === key && (
        <div
          className="db-formula-preview"
          role={preview.result.status === "error" ? "alert" : "status"}
        >
          {preview.result.status === "error" ? (
            <p>{preview.result.diagnostic}</p>
          ) : (
            <>
              <strong>Computed {preview.result.resultType}</strong>
              <pre>{JSON.stringify(preview.result.value, null, 2)}</pre>
              <small>
                {preview.result.computedAt} · {preview.result.timezone}
              </small>
            </>
          )}
        </div>
      )}
      <details>
        <summary>Retained source and captured evidence</summary>
        <p>Original expression</p>
        <pre>{String(raw.originalExpression ?? "None")}</pre>
        <p>Imported cached value (not a live result)</p>
        <pre>{JSON.stringify(raw.importedCachedValue ?? null, null, 2)}</pre>
      </details>
    </div>
  );
}
