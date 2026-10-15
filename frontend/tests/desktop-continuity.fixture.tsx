import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { PageFreshness, ReviewQueue } from "../src/PageFreshness";
import { SearchPanel } from "../src/search/SearchPanel";
import { AIPanel } from "../src/ai/AIPanel";
import { P2DatabasePanel, P2RowButtons } from "../src/database/P2Database";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const config: any = {
  chart: {
    aggregation: "count",
    visualization: "bar",
    groupPropertyId: null,
    valuePropertyId: null,
  },
  form: { enabled: true, requiredPropertyIds: [] },
  buttons: [
    {
      id: id(20),
      label: "Approve",
      propertyId: id(9),
      value: { type: "checkbox", schemaVersion: 1, value: true },
      confirmationRequired: true,
    },
  ],
};
const state: any = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [
    { id: id(3), title: "Database", revision: "1" },
    { id: id(4), title: "Linked knowledge", revision: "1" },
  ],
  collections: [
    { id: id(5), owningPageId: id(3), title: "Database", revision: "1" },
  ],
  properties: [
    { id: id(6), name: "Title", type: "title" },
    { id: id(7), name: "Notes", type: "text" },
    { id: id(8), name: "Amount", type: "number" },
    { id: id(9), name: "Approved", type: "checkbox" },
  ].map((p) => ({
    ...p,
    collectionId: id(5),
    revision: "1",
    schemaVersion: 1,
    config: {},
  })),
  records: [
    {
      id: id(10),
      pageId: id(4),
      collectionId: id(5),
      revision: "1",
      values: [],
    },
  ],
  blocks: [],
  views: [],
  files: [],
  taskFacets: [],
  memories: [],
  containers: [],
  containerCollections: [],
  meetings: [],
  transcriptSegments: [],
};
let stale = false,
  queueCurrent = false,
  next = 100;
let releaseQueue = () => {};
let rerender = () => {};
const identity = { version: 1, digest: "a".repeat(64) };
const review = {
  id: id(30),
  pageId: id(4),
  actorId: id(2),
  reviewedAt: "2026-10-15T00:00:00Z",
  expiresAt: new Date(Date.now() + 60000).toISOString(),
  contentIdentity: identity,
};
const freshness = () =>
  new URLSearchParams(location.search).has("unavailable")
    ? {
        state: "unavailable",
        message:
          "Review coverage exceeds the bounded collection size. Review individual records.",
        review,
      }
    : {
        state: stale ? "stale" : "reviewed",
        contentIdentity: stale
          ? { ...identity, digest: "b".repeat(64) }
          : identity,
        review,
      };
const item = (title: string, n: number) => ({
  pageId: id(n),
  title,
  freshness: { state: "never_reviewed", contentIdentity: identity },
});
(window as any).go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const r = JSON.parse(raw),
          ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "models.list") return ok({ packs: [] });
        if (method === "documents.status") return ok({ files: [] });
        if (method === "search.semanticSettings")
          return ok({ enabled: false, modelPackId: "" });
        if (method === "search.query")
          return ok({
            hits: [
              {
                chunkId: id(50),
                title: "Linked knowledge",
                kind: "page",
                excerpt: "Reviewed source content",
                location: { pageId: id(4), sourceRevision: "1" },
                score: 1,
                freshness: freshness(),
              },
            ],
            partial: false,
            mode: "keyword",
          });
        if (method === "ai.runs" || method === "ai.run") {
          const run = {
            id: id(51),
            conversationId: id(52),
            state: "completed",
            request: {
              mode: "answer",
              prompt: "What is the knowledge?",
              useMemory: false,
            },
            text: "Reviewed source [1].",
            citations: [],
            sources: [
              {
                number: 1,
                title: "Linked knowledge",
                text: "Reviewed source content",
                location: { pageId: id(4), sourceRevision: "1" },
                state: "current",
                freshness: freshness(),
              },
            ],
            createdAt: "2026-10-15T00:00:00Z",
          };
          return ok(method === "ai.runs" ? [run] : run);
        }
        if (method === "page.freshness")
          return ok({
            pageId: r.pageId,
            freshness: freshness(),
            history: [review],
          });
        if (method === "page.reviewQueue") {
          if (r.afterPageId)
            return new Promise<string>((resolve) => {
              releaseQueue = () =>
                resolve(ok({ items: [item("Obsolete queue result", 40)] }));
            });
          return ok({
            items: [
              item(queueCurrent ? "Current review" : "Pending review", 41),
            ],
            nextPageId: id(41),
          });
        }
        if (method === "databaseP2")
          return ok({ config: structuredClone(config) });
        if (method === "databaseChart")
          return ok({
            config: config.chart,
            recordCount: 1,
            groups: [
              {
                key: "all",
                label: "All records",
                decimal: "1",
                recordIds: [id(10)],
              },
            ],
          });
        if (method === "allocateIds")
          return ok({ ids: Array.from({ length: r.count }, () => id(next++)) });
        if (method === "batch") {
          const c = r.commands[0];
          if (c.kind === "database.p2Configure") {
            if (
              r.mutation.expectedRevisions.find(
                (g: any) => g.target.entityId === id(5),
              )?.revision !== state.collections[0].revision
            )
              throw Error("Stale configuration guard");
            Object.assign(config, c.config);
            (window as any).__settingsSaves =
              ((window as any).__settingsSaves ?? 0) + 1;
          } else if (c.kind === "database.formSubmit")
            (window as any).__submission = c;
          else if (c.kind === "database.rowButtonRun")
            (window as any).__rowAction = c;
          else throw Error(`Unexpected ${c.kind}`);
          state.snapshotCursor = String(Number(state.snapshotCursor) + 1);
          state.collections[0].revision = state.snapshotCursor;
          return ok({
            revisions: [],
            eventCursor: state.snapshotCursor,
            replayed: false,
          });
        }
        throw Error(`Unexpected ${method}`);
      },
    },
  },
};
function App() {
  const [w, setW] = useState(structuredClone(state));
  const [error, setError] = useState("");
  rerender = () => setW(structuredClone(state));
  const refresh = () => {
    queueCurrent = true;
    state.snapshotCursor = String(Number(state.snapshotCursor) + 1);
    rerender();
  };
  return (
    <>
      <button
        onClick={() => {
          stale = true;
          refresh();
        }}
      >
        Change linked source
      </button>
      <button onClick={refresh}>Refresh snapshot</button>
      <button onClick={() => releaseQueue()}>
        Release older queue response
      </button>
      <button
        onClick={() => {
          config.chart.aggregation = "sum";
          config.chart.valuePropertyId = id(8);
          state.collections[0].revision = String(
            Number(state.collections[0].revision) + 1,
          );
          refresh();
        }}
      >
        Change saved settings
      </button>
      <button
        onClick={() => {
          state.properties.find((p: any) => p.id === id(7)).revision = "2";
          state.collections[0].revision = String(
            Number(state.collections[0].revision) + 1,
          );
          refresh();
        }}
      >
        Change form schema
      </button>
      <p role="alert">{error}</p>
      <PageFreshness
        workspace={w}
        page={w.pages[1]}
        onChange={async () => rerender()}
        onError={(e) => setError(String(e))}
      />
      <ReviewQueue workspace={w} onNavigate={() => {}} />
      <SearchPanel workspace={w} onNavigate={() => {}} />
      <AIPanel
        workspace={w}
        onNavigate={() => {}}
        onCommitted={async () => rerender()}
      />
      <P2DatabasePanel
        workspace={w}
        collection={w.collections[0]}
        records={w.records}
        openPage={() => {}}
        onCommitted={async () => rerender()}
        onError={(e) => setError(String(e))}
      />
      <P2RowButtons
        workspace={w}
        collection={w.collections[0]}
        record={w.records[0]}
        buttons={config.buttons}
        onCommitted={async () => rerender()}
        onError={(e) => setError(String(e))}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
