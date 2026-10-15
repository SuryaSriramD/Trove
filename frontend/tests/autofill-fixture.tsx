import { useState } from "react";
import { createRoot } from "react-dom/client";
import { AutofillPanel } from "../src/database/AutofillPanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const collection = {
  id: id(2),
  owningPageId: id(3),
  title: "Dates",
  revision: "1",
};
const properties = [
  {
    id: id(4),
    collectionId: id(2),
    name: "Title",
    type: "title",
    schemaVersion: 1,
    config: {},
    revision: "1",
  },
  {
    id: id(5),
    collectionId: id(2),
    name: "Summary",
    type: "text",
    schemaVersion: 1,
    config: {},
    revision: "1",
  },
];
const state: any = JSON.parse(
  localStorage.getItem("autofill-fixture") ?? "null",
) ?? { batches: [], applied: 0, undone: 0, phase: 0 };
const records = [
  { id: id(6), collectionId: id(2), pageId: id(7), revision: "1", values: [] },
  { id: id(8), collectionId: id(2), pageId: id(9), revision: "1", values: [] },
];
const w = {
  workspaceId: id(1),
  scopeGrantId: id(10),
  snapshotCursor: "1",
  collections: [collection],
  properties,
  records,
  pages: [
    { id: id(7), title: "Blank row", revision: "1" },
    { id: id(9), title: "Manual row", revision: "1" },
  ],
  blocks: [],
  taskFacets: [],
} as unknown as WorkspaceSnapshot;
const persist = () =>
  localStorage.setItem("autofill-fixture", JSON.stringify(state));
(window as any).autofillFixture = state;
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const r = JSON.parse(raw);
        let result: any = {};
        const b = state.batches.find((b: any) => b.id === r.batchId);
        if (method === "models.list")
          result = {
            packs: [
              {
                pack: { id: id(11), name: "Fixture local model", kind: "chat" },
                state: "ready",
              },
            ],
          };
        if (method === "autofill.list") result = state.batches;
        if (method === "autofill.get") result = b;
        if (method === "autofill.create") {
          result = {
            version: 1,
            id: id(12 + state.batches.length),
            input: r.input,
            promptVersion: 1,
            state: "draft",
            createdAt: new Date().toISOString(),
            expectedRevisions: [],
            appliedIds: [],
            cells: r.input.recordIds.map((rid: string, i: number) => ({
              id: id(30 + i),
              recordId: rid,
              pageId: records.find((x) => x.id === rid)!.pageId,
              property: properties[1],
              sources: [
                {
                  name: "Title",
                  value: i ? "Existing source" : "New source",
                  present: true,
                },
              ],
              before: i ? [{ text: "Manual fact" }] : null,
              beforePresent: !!i,
              overwrite: !!i,
              state: i && !r.input.includeNonempty ? "skipped" : "pending",
              value: null,
              reason: "",
            })),
          };
          state.batches.unshift(result);
        }
        if (method === "autofill.generate") {
          b.state = "running";
          result = b;
          if (state.phase === 0) {
            b.cells[0].state = "value";
            b.cells[0].value = [{ text: "Grounded summary" }];
            state.phase = 1;
          } else {
            b.cells.forEach((c: any) => {
              if (c.state === "pending") {
                c.state = "value";
                c.value = [{ text: "Reviewed replacement" }];
              }
            });
            b.state = "review";
          }
        }
        if (method === "autofill.cancel") {
          b.state = "interrupted";
          result = { state: "cancellation_requested" };
        }
        if (method === "autofill.apply") {
          for (const c of b.cells)
            if (
              r.cellIds.includes(c.id) &&
              c.overwrite &&
              !r.overwriteCellIds.includes(c.id)
            )
              throw new Error("Explicit overwrite required");
          b.state = "applied";
          b.appliedIds = r.cellIds;
          state.applied++;
          result = { replayed: false };
        }
        if (method === "operations.previewUndo")
          result = {
            command: { kind: "operation.undo", operationId: r.operationId },
            expectedRevisions: [],
          };
        if (method === "batch") {
          state.undone++;
          result = { replayed: false };
        }
        persist();
        return JSON.stringify({ ok: true, result });
      },
    },
  },
};
function Fixture() {
  const [error, setError] = useState("");
  return (
    <>
      <p role="alert">{error}</p>
      <AutofillPanel
        workspace={w}
        collection={collection as any}
        records={records as any}
        onCommitted={async () => {}}
        onError={(e) => setError(String(e))}
        openPage={() => {}}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
