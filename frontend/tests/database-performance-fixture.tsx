import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import DatabaseView from "../src/database/DatabaseView";
import type { WorkspaceSnapshot } from "../src/api";
import "../src/style.css";

const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const makePage = (n: number, title: string) => ({
  id: id(n),
  workspaceId: id(1),
  parentPageId: null,
  title,
  revision: "1",
  createdAt: "2026-10-08T00:00:00Z",
  updatedAt: "2026-10-08T00:00:00Z",
  deletedAt: null,
});
const initial: any = {
  contractVersion: "1.0.0",
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [makePage(3, "Paged database")],
  blocks: [],
  collections: [
    {
      id: id(10),
      workspaceId: id(1),
      owningPageId: id(3),
      title: "Paged database",
      titlePropertyId: id(11),
      systemRole: "none",
      revision: "1",
    },
  ],
  properties: [
    {
      id: id(11),
      collectionId: id(10),
      name: "Name",
      type: "title",
      schemaVersion: 1,
      config: {},
      revision: "1",
    },
    {
      id: id(12),
      collectionId: id(10),
      name: "Related",
      type: "relation",
      schemaVersion: 1,
      config: { targetCollectionId: id(20), bidirectionalPropertyId: null },
      revision: "1",
    },
    {
      id: id(13),
      collectionId: id(10),
      name: "Clock value",
      type: "formula",
      schemaVersion: 1,
      config: { expression: "now()" },
      revision: "1",
    },
  ],
  records: [],
  views: [],
  files: [],
  meetings: [],
  taskFacets: [],
  containers: [],
  containerCollections: [],
  memories: [],
  projectScopes: [],
  transcriptSegments: [],
  importMappings: [],
};
for (let n = 1; n <= 450; n++) {
  initial.pages.push(makePage(1000 + n, `Row ${String(n).padStart(3, "0")}`));
  initial.records.push({
    id: id(2000 + n),
    collectionId: id(10),
    pageId: id(1000 + n),
    sortKey: String(n).padStart(12, "0"),
    revision: "1",
    values: [
      { propertyId: id(11), schemaVersion: 1, value: { pageId: id(1000 + n) } },
      {
        propertyId: id(12),
        schemaVersion: 1,
        value: [id(30000 + 9500), id(30000 + 42)],
      },
    ],
  });
}
for (let n = 1; n <= 10000; n++) {
  initial.pages.push(
    makePage(
      10000 + n,
      `Target ${String(n).padStart(5, "0")}${n === 10000 ? " తెలుగు 中文 🌱" : ""}`,
    ),
  );
  initial.records.push({
    id: id(30000 + n),
    collectionId: id(20),
    pageId: id(10000 + n),
    sortKey: String(n).padStart(12, "0"),
    revision: "1",
    values: [],
  });
}
for (const [n, name] of [
  [14, "Table"],
  [15, "Filtered"],
] as const)
  initial.views.push({
    id: id(n),
    collectionId: id(10),
    owningPageId: id(3),
    containerId: null,
    name,
    type: "table",
    schemaVersion: 1,
    query: {
      filter:
        n === 15 ? { propertyId: id(11), op: "contains", value: "4" } : null,
      sorts: [],
      visiblePropertyIds: [id(11), id(12), id(13)],
      groupByPropertyId: null,
      subgroupByPropertyId: null,
    },
    config: { columnWidths: [] },
    revision: "1",
  });
let workspace = initial;
let refresh: () => void;
const state = {
  calls: [] as any[],
  holdQueries: false,
  failQueries: false,
  pending: [] as (() => void)[],
  limit: 450,
  errors: [] as string[],
  releaseAll() {
    state.holdQueries = false;
    for (const release of state.pending.splice(0)) release();
  },
  releaseOne() {
    state.pending.shift()?.();
  },
  changeSnapshot() {
    workspace = {
      ...workspace,
      snapshotCursor: String(Number(workspace.snapshotCursor) + 1),
    };
    refresh();
  },
  saved: [] as any[],
};
(window as any).databasePerformanceFixture = state;
window.go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const request = JSON.parse(raw);
        state.calls.push({ method, ...request });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "recordLayouts.get")
          return ok({
            collectionId: request.collectionId,
            ...(request.recordId ? { recordId: request.recordId } : {}),
            defaultLayout: null,
            overrideLayout: null,
            effectiveLayout: { version: 1, sections: [], hiddenPropertyIds: [] },
            missingPropertyIds: [],
          });
        if (method === "databaseP2")
          return ok({
            config: {
              buttons: [],
              chart: {},
              form: { enabled: false, requiredPropertyIds: [] },
            },
          });
        if (method === "queryRecords") {
          if (state.failQueries)
            return JSON.stringify({
              ok: false,
              error: {
                code: "CONFLICT",
                message: "Records, view or clock changed; restart the query",
              },
            });
          const minute = Math.floor(Date.now() / 60000);
          const [cursorMinute, offsetText] = (
            request.cursor ?? `${minute}:0`
          ).split(":");
          if (Number(cursorMinute) !== minute)
            return JSON.stringify({
              ok: false,
              error: {
                code: "CONFLICT",
                message: "Records, view or clock changed; restart the query",
              },
            });
          const offset = Number(offsetText);
          const candidates = workspace.records
            .filter((r: any) => r.collectionId === id(10))
            .slice(request.viewId === id(15) ? 400 : 0, state.limit);
          const records = candidates
            .slice(offset, offset + request.limit)
            .map((r: any) => ({
              ...r,
              values: [
                ...r.values,
                {
                  propertyId: id(13),
                  schemaVersion: 1,
                  value: {
                    raw: {
                      status: "computed",
                      value: `Minute ${minute}`,
                      computedAt: new Date().toISOString(),
                    },
                  },
                },
              ],
            }));
          const result = {
            records,
            recordIds: records.map((r: any) => r.id),
            snapshotCursor: workspace.snapshotCursor,
            ...(offset + request.limit < candidates.length
              ? { nextCursor: `${minute}:${offset + request.limit}` }
              : {}),
          };
          if (state.holdQueries)
            await new Promise<void>((resolve) => state.pending.push(resolve));
          return ok(result);
        }
        if (method === "batch") {
          state.saved.push(request);
          for (const c of request.commands)
            if (c.kind === "record.setValue") {
              const record = workspace.records.find(
                (r: any) => r.id === c.recordId,
              );
              record.values = record.values
                .filter((v: any) => v.propertyId !== c.propertyId)
                .concat({
                  propertyId: c.propertyId,
                  schemaVersion: 1,
                  value: c.value.value,
                });
              record.revision = String(Number(record.revision) + 1);
            }
          workspace = {
            ...workspace,
            snapshotCursor: String(Number(workspace.snapshotCursor) + 1),
          };
          return ok({
            revisions: [],
            eventCursor: workspace.snapshotCursor,
            transactionId: id(99),
          });
        }
        throw Error(`Unhandled fixture call ${method}`);
      },
    },
  },
} as any;
function Fixture() {
  const [current, setCurrent] = useState<WorkspaceSnapshot>(workspace);
  const [error, setError] = useState("");
  refresh = () => setCurrent({ ...workspace });
  return (
    <main>
      <DatabaseView
        workspace={current}
        collectionId={id(10)}
        onCommitted={async () => refresh()}
        onError={(e) => {
          state.errors.push(String(e));
          setError(String(e));
        }}
        openPage={() => {}}
      />
      <p aria-label="Fixture error">{error}</p>
    </main>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
