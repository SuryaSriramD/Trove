import { ArrowDown, ArrowUp } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  readRecord,
  type D,
  type S,
  type WorkspaceSnapshot,
} from "../api";
import { guard, setProperty, uniqueGuards } from "./service";
import { ValueEditor } from "./DatabaseView";
import { valueOf } from "./model";
import { indexRelationTargets } from "./RelationPicker";

type Props = {
  workspace: WorkspaceSnapshot;
  collectionId: string;
  recordId?: string;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
};
const empty = (): S.RecordLayout => ({
  version: 1,
  sections: [],
  hiddenPropertyIds: [],
});
export function RecordLayout({
  workspace: w,
  collectionId,
  recordId,
  onCommitted,
  onError,
}: Props) {
  const [result, setResult] = useState<S.RecordLayoutResult>();
  const [record, setRecord] = useState<D.Record>();
  const [draft, setDraft] = useState<S.RecordLayout>(empty);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [retry, setRetry] = useState(0);
  const relationTargets = useMemo(
    () => indexRelationTargets(w.records, w.pages),
    [w.records, w.pages],
  );
  const collection = w.collections.find((c) => c.id === collectionId);
  const properties = w.properties.filter(
    (p) => p.collectionId === collectionId && p.type !== "title",
  );
  useEffect(() => {
    let active = true;
    setFailure("");
    setResult(undefined);
    setRecord(undefined);
    void Promise.all([
      dispatch<S.RecordLayoutResult>("recordLayouts.get", {
        ...envelope(w),
        collectionId,
        ...(recordId ? { recordId } : {}),
      }),
      recordId ? readRecord(w, recordId) : Promise.resolve(undefined),
    ])
      .then(([layout, r]) => {
        if (active) {
          setResult(layout);
          setDraft(
            structuredClone(
              recordId
                ? layout.effectiveLayout
                : (layout.defaultLayout ?? empty()),
            ),
          );
          setRecord(r);
        }
      })
      .catch((e) => {
        if (active) setFailure(String(e));
      });
    return () => {
      active = false;
    };
  }, [w.workspaceId, collectionId, recordId, w.snapshotCursor, retry]);
  if (!collection) return null;
  if (failure)
    return (
      <p role="alert">
        {failure}{" "}
        <button onClick={() => setRetry((n) => n + 1)}>
          Reload record layout
        </button>
      </p>
    );
  if (!result) return <p role="status">Loading record layout…</p>;
  async function save(layout: S.RecordLayout | null) {
    const page = w.pages.find(
      (p) => p.id === (record?.pageId ?? collection!.owningPageId),
    );
    if (!page) return;
    setBusy(true);
    try {
      const [operationId] = await ids(w, 1);
      const action: S.JournalAction = recordId
        ? { kind: "record.layout.set", recordId, layout }
        : { kind: "collection.layout.set", collectionId, layout };
      await batch(
        w,
        [
          {
            kind: "operation.apply",
            operationId,
            label: layout ? "Change record layout" : "Reset record layout",
            actions: [action],
          },
        ],
        uniqueGuards([
          ...pageGuards(w, page),
          guard("collection", collectionId, collection!.revision),
        ]),
      );
      await onCommitted();
      setRetry((n) => n + 1);
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  }
  function moveSection(index: number, step: number) {
    setDraft((d) => {
      const sections = [...d.sections];
      const [s] = sections.splice(index, 1);
      sections.splice(index + step, 0, s);
      return { ...d, sections };
    });
  }
  function assign(propertyId: string, target: string) {
    setDraft((d) => ({
      ...d,
      hiddenPropertyIds: [
        ...d.hiddenPropertyIds.filter((id) => id !== propertyId),
        ...(target === "hidden" ? [propertyId] : []),
      ],
      sections: d.sections.map((s) => ({
        ...s,
        propertyIds: [
          ...s.propertyIds.filter((id) => id !== propertyId),
          ...(target === s.id ? [propertyId] : []),
        ],
      })),
    }));
  }
  function moveProperty(sectionId: string, index: number, step: number) {
    setDraft((d) => ({
      ...d,
      sections: d.sections.map((s) => {
        if (s.id !== sectionId) return s;
        const propertyIds = [...s.propertyIds];
        const [id] = propertyIds.splice(index, 1);
        propertyIds.splice(index + step, 0, id);
        return { ...s, propertyIds };
      }),
    }));
  }
  const effective = result.effectiveLayout;
  const assigned = new Set([
    ...effective.hiddenPropertyIds,
    ...effective.sections.flatMap((s) => s.propertyIds),
  ]);
  const sections = [
    ...effective.sections,
    {
      id: "unplaced",
      title: "Other properties",
      placement: "header" as const,
      propertyIds: properties
        .filter((p) => !assigned.has(p.id))
        .map((p) => p.id),
    },
  ];
  const field = (property: D.PropertyDefinition) => (
    <label
      key={`${property.id}:${record?.revision}`}
      className="record-layout-field"
    >
      <span>{property.name}</span>
      <ValueEditor
        property={property}
        value={valueOf(record!, property.id)}
        workspace={w}
        relationTargets={relationTargets.get(
          (property.config as { targetCollectionId?: string })
            .targetCollectionId ?? "",
        )}
        label={`${property.name} on record page`}
        disabled={busy}
        onChange={(value) => {
          setBusy(true);
          void setProperty(w, record!, property, value)
            .then(onCommitted)
            .then(() => setRetry((n) => n + 1))
            .catch(onError)
            .finally(() => setBusy(false));
        }}
      />
    </label>
  );
  return (
    <section
      className="record-layout"
      aria-label={recordId ? "Record properties" : "Collection record layout"}
    >
      {result.missingPropertyIds.length > 0 && (
        <p role="status">
          {result.missingPropertyIds.length} removed properties are omitted from
          this layout. Their stable references remain available in layout
          history.
        </p>
      )}
      {record && (
        <div className="record-layout-grid">
          {(["header", "sidebar"] as const).map((placement) => (
            <div
              key={placement}
              className={`record-layout-${placement}`}
              aria-label={`${placement} properties`}
            >
              {sections
                .filter((s) => s.placement === placement)
                .map((s) => {
                  const shown = s.propertyIds
                    .map((id) => properties.find((p) => p.id === id))
                    .filter((p): p is D.PropertyDefinition => !!p);
                  return (
                    shown.length > 0 && (
                      <section key={s.id}>
                        <h3>{s.title}</h3>
                        {shown.map(field)}
                      </section>
                    )
                  );
                })}
            </div>
          ))}
        </div>
      )}
      <details>
        <summary>
          {recordId
            ? "Customize this record layout"
            : "Edit collection record layout"}
        </summary>
        <p>
          {recordId
            ? result.overrideLayout
              ? "This record uses an override."
              : "This record uses the collection default."
            : "This default is shared by records without an override."}{" "}
          Layout changes preserve all values. Unplaced properties appear under
          Other properties.
        </p>
        {draft.sections.map((section, index) => (
          <fieldset key={section.id}>
            <legend>Section {index + 1}</legend>
            <label>
              Section title
              <input
                aria-label={`Section ${index + 1} title`}
                value={section.title}
                onChange={(e) =>
                  setDraft((d) => ({
                    ...d,
                    sections: d.sections.map((s) =>
                      s.id === section.id ? { ...s, title: e.target.value } : s,
                    ),
                  }))
                }
              />
            </label>
            <label>
              Placement
              <select
                aria-label={`Section ${index + 1} placement`}
                value={section.placement}
                onChange={(e) =>
                  setDraft((d) => ({
                    ...d,
                    sections: d.sections.map((s) =>
                      s.id === section.id
                        ? {
                            ...s,
                            placement: e.target.value as "header" | "sidebar",
                          }
                        : s,
                    ),
                  }))
                }
              >
                <option value="header">Header</option>
                <option value="sidebar">Sidebar</option>
              </select>
            </label>
            <button
              disabled={index === 0 || busy}
              onClick={() => moveSection(index, -1)}
            >
              Move section up
            </button>
            <button
              disabled={index === draft.sections.length - 1 || busy}
              onClick={() => moveSection(index, 1)}
            >
              Move section down
            </button>
            <button
              disabled={busy}
              onClick={() =>
                setDraft((d) => ({
                  ...d,
                  sections: d.sections.filter((s) => s.id !== section.id),
                }))
              }
            >
              Remove section
            </button>
            <ol>
              {section.propertyIds.map((id, i) => (
                <li key={id}>
                  {properties.find((p) => p.id === id)?.name ??
                    "Removed property"}
                  <button
                    aria-label={`Move ${properties.find((p) => p.id === id)?.name ?? id} up`}
                    disabled={i === 0 || busy}
                    onClick={() => moveProperty(section.id, i, -1)}
                  >
                    <ArrowUp size={16} strokeWidth={1.5} aria-hidden="true" />
                  </button>
                  <button
                    aria-label={`Move ${properties.find((p) => p.id === id)?.name ?? id} down`}
                    disabled={i === section.propertyIds.length - 1 || busy}
                    onClick={() => moveProperty(section.id, i, 1)}
                  >
                    <ArrowDown size={16} strokeWidth={1.5} aria-hidden="true" />
                  </button>
                </li>
              ))}
            </ol>
          </fieldset>
        ))}
        <button
          disabled={busy || draft.sections.length >= 32}
          onClick={() => {
            setBusy(true);
            void ids(w, 1)
              .then(([id]) =>
                setDraft((d) => ({
                  ...d,
                  sections: [
                    ...d.sections,
                    {
                      id,
                      title: "Properties",
                      placement: "header",
                      propertyIds: [],
                    },
                  ],
                })),
              )
              .catch(onError)
              .finally(() => setBusy(false));
          }}
        >
          Add layout section
        </button>
        <div className="record-layout-assignments">
          {properties.map((p) => (
            <label key={p.id}>
              {p.name}
              <select
                aria-label={`Layout placement for ${p.name}`}
                value={
                  draft.hiddenPropertyIds.includes(p.id)
                    ? "hidden"
                    : (draft.sections.find((s) => s.propertyIds.includes(p.id))
                        ?.id ?? "unplaced")
                }
                onChange={(e) => assign(p.id, e.target.value)}
              >
                <option value="unplaced">Other properties</option>
                <option value="hidden">Hidden</option>
                {draft.sections.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.title || "Untitled section"} · {s.placement}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
        <button disabled={busy} onClick={() => void save(draft)}>
          Save {recordId ? "record override" : "collection layout"}
        </button>
        <button disabled={busy} onClick={() => void save(null)}>
          Reset to {recordId ? "collection default" : "standard layout"}
        </button>
        <button
          disabled={busy}
          onClick={() =>
            setDraft(
              structuredClone(
                recordId
                  ? result.effectiveLayout
                  : (result.defaultLayout ?? empty()),
              ),
            )
          }
        >
          Discard layout edits
        </button>
      </details>
    </section>
  );
}
