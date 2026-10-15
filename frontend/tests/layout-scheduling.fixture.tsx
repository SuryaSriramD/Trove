import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { RecordLayout } from "../src/database/RecordLayout";
import { SchedulingPanel } from "../src/database/SchedulingPanel";
import { OperationHistory } from "../src/OperationHistory";
import "../src/database/database.css";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const date = (start: string, end: string) => ({
  start,
  end,
  hasTime: false,
  timezone: "UTC",
});
const value = (start: string, end: string) => ({
  propertyId: id(8),
  type: "date",
  schemaVersion: 1,
  value: date(start, end),
});
const initial = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [
    { id: id(3), title: "Project", revision: "1" },
    { id: id(4), title: "Plan", revision: "1" },
    { id: id(5), title: "Build", revision: "1" },
  ],
  collections: [
    { id: id(6), owningPageId: id(3), title: "Project", revision: "1" },
  ],
  properties: [
    {
      id: id(7),
      collectionId: id(6),
      name: "Title",
      type: "title",
      schemaVersion: 1,
      config: {},
      revision: "1",
    },
    {
      id: id(8),
      collectionId: id(6),
      name: "Date",
      type: "date",
      schemaVersion: 1,
      config: {},
      revision: "1",
    },
    {
      id: id(9),
      collectionId: id(6),
      name: "Notes",
      type: "text",
      schemaVersion: 1,
      config: {},
      revision: "1",
    },
  ],
  records: [
    {
      id: id(10),
      pageId: id(4),
      collectionId: id(6),
      revision: "1",
      values: [
        value("2026-10-12", "2026-10-13"),
        {
          propertyId: id(9),
          type: "text",
          schemaVersion: 1,
          value: [{ text: "Retain these words" }],
        },
      ],
    },
    {
      id: id(11),
      pageId: id(5),
      collectionId: id(6),
      revision: "1",
      values: [value("2026-10-14", "2026-10-15")],
    },
  ],
  blocks: [],
  files: [],
  taskFacets: [],
  memories: [],
  containers: [],
  containerCollections: [],
  meetings: [],
  transcriptSegments: [],
  views: [],
  layouts: { collection: null, record: null },
  operations: [],
  mutations: 0,
  next: 100,
};
let state =
  JSON.parse(localStorage.getItem("g10-fixture") ?? "null") ?? initial;
const persist = () =>
  localStorage.setItem("g10-fixture", JSON.stringify(state));
const guards = () => [
  {
    target: { entityType: "collection", entityId: id(6) },
    revision: state.collections[0].revision,
  },
  ...state.records.flatMap((r: any) => [
    { target: { entityType: "record", entityId: r.id }, revision: r.revision },
    {
      target: { entityType: "page", entityId: r.pageId },
      revision: state.pages.find((p: any) => p.id === r.pageId).revision,
    },
  ]),
  { target: { entityType: "property", entityId: id(8) }, revision: "1" },
];
(window as any).go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const req = JSON.parse(raw);
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "allocateIds")
          return ok({
            ids: Array.from({ length: req.count }, () => id(++state.next)),
          });
        if (method === "readRecord")
          return ok({
            record: state.records.find((r: any) => r.id === req.recordId),
          });
        if (method === "recordLayouts.get")
          return ok({
            collectionId: id(6),
            ...(req.recordId ? { recordId: req.recordId } : {}),
            defaultLayout: state.layouts.collection,
            overrideLayout: req.recordId ? state.layouts.record : null,
            effectiveLayout: (req.recordId ? state.layouts.record : null) ??
              state.layouts.collection ?? {
                version: 1,
                sections: [],
                hiddenPropertyIds: [],
              },
            missingPropertyIds: [],
          });
        if (method === "scheduling.preview") {
          const records = state.records.map((r: any, index: number) => ({
            recordId: r.id,
            pageId: r.pageId,
            title: state.pages.find((p: any) => p.id === r.pageId).title,
            before: r.values[0].value,
            after:
              index === 0
                ? date("2026-10-15", "2026-10-16")
                : date("2026-10-17", "2026-10-18"),
            dependencyRecordIds: index ? [id(10)] : [],
            reason: index ? "dependency" : "requested",
            changed: true,
            duration: "2 inclusive eligible dates",
            dstEffects: [],
          }));
          return ok({
            version: 1,
            id: id(++state.next),
            hash: "a".repeat(64),
            input: req.input,
            requiredRevisions: guards(),
            records,
          });
        }
        if (method === "operations.list")
          return ok({
            operations: state.operations
              .filter((o: any) => o.pages.includes(req.pageId))
              .map(({ id, label, state }: any) => ({
                id,
                label,
                state,
                createdAt: "2026-10-15T00:00:00Z",
              })),
          });
        if (method === "operations.previewUndo") {
          const o = state.operations.find((o: any) => o.id === req.operationId);
          return ok({
            operationId: o.id,
            label: o.label,
            actions: o.actions,
            expectedRevisions: guards(),
            command: { kind: "operation.undo", operationId: o.id },
          });
        }
        if (method === "batch") {
          const c = req.commands[0];
          if (c.kind === "operation.apply") {
            const action = c.actions[0];
            if (
              !req.mutation.expectedRevisions.some(
                (g: any) => g.target.entityId === id(6),
              )
            )
              throw Error("Missing collection guard");
            const target =
              action.kind === "collection.layout.set" ? "collection" : "record";
            state.operations.push({
              id: c.operationId,
              label: c.label,
              state: "applied",
              pages: [target === "collection" ? id(3) : id(4)],
              actions: [
                { ...action, layout: structuredClone(state.layouts[target]) },
              ],
            });
            state.layouts[target] = action.layout;
          } else if (c.kind === "scheduling.apply") {
            if (
              req.mutation.idempotencyKey !== `schedule-${c.plan.id}` ||
              req.mutation.expectedRevisions.length !== 6
            )
              throw Error("Schedule receipt or guards missing");
            const actions = state.records.map((r: any) => ({
              kind: "record.setValue",
              recordId: r.id,
              propertyId: id(8),
              value: {
                type: "date",
                schemaVersion: 1,
                value: structuredClone(r.values[0].value),
              },
            }));
            state.operations.push({
              id: c.plan.id,
              label: "Reschedule dependencies",
              state: "applied",
              pages: [id(4), id(5)],
              actions,
            });
            c.plan.records.forEach((change: any) => {
              state.records.find(
                (r: any) => r.id === change.recordId,
              ).values[0].value = change.after;
            });
          } else if (c.kind === "operation.undo") {
            const operation = state.operations.find(
              (o: any) => o.id === c.operationId,
            );
            operation.actions.forEach((a: any) => {
              if (a.kind === "record.setValue")
                state.records.find(
                  (r: any) => r.id === a.recordId,
                ).values[0].value = a.value.value;
              else
                state.layouts[
                  a.kind === "collection.layout.set" ? "collection" : "record"
                ] = a.layout;
            });
            operation.state = "undone";
          } else throw Error("Unexpected mutation");
          state.mutations++;
          state.snapshotCursor = String(Number(state.snapshotCursor) + 1);
          state.pages.forEach((p: any) => (p.revision = state.snapshotCursor));
          state.records.forEach(
            (r: any) => (r.revision = state.snapshotCursor),
          );
          state.collections[0].revision = state.snapshotCursor;
          persist();
          return ok({
            revisions: [],
            eventCursor: state.snapshotCursor,
            replayed: false,
          });
        }
        throw Error(`Unexpected method ${method}`);
      },
    },
  },
};
function Fixture() {
  const [w, setW] = useState(structuredClone(state));
  const [mode, setMode] = useState("collection");
  const [error, setError] = useState("");
  const committed = async () => {
    setW(structuredClone(state));
  };
  return (
    <main>
      <button
        onClick={() => setMode(mode === "collection" ? "record" : "collection")}
      >
        {mode === "collection" ? "Open Plan record" : "Open collection"}
      </button>
      {error && <p role="alert">{error}</p>}
      <RecordLayout
        key={mode}
        workspace={w}
        collectionId={id(6)}
        recordId={mode === "record" ? id(10) : undefined}
        onCommitted={committed}
        onError={(e) => setError(String(e))}
      />
      {mode === "collection" && (
        <SchedulingPanel
          workspace={w}
          collection={w.collections[0]}
          records={w.records}
          onCommitted={committed}
          onError={(e) => setError(String(e))}
        />
      )}
      <OperationHistory
        workspace={w}
        pageId={mode === "record" ? id(4) : id(3)}
        onChange={committed}
        onError={(e) => setError(String(e))}
      />
    </main>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
