import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { NotionCapturePanel } from "../src/imports/NotionCapturePanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
} as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).notionCalls = calls;
let capture: any;
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "imports.notion.list")
          return ok(capture ? [capture] : []);
        if (method === "imports.notion.start") {
          capture = {
            captureId: id(3),
            planId: id(4),
            revision: 1,
            state: "capturing",
            authorizedRoots: request.roots.map((r: any) => `${r.kind}:${r.id}`),
            apiVersion: "2026-03-11",
            completed: 0,
            bytes: 0,
            pending: [
              {
                kind: "database",
                id: request.roots[0].id,
                pageId: id(5),
                parentId: id(6),
                depth: 0,
              },
            ],
          };
          return ok(capture);
        }
        if (method === "imports.notion.step") {
          if (capture.revision === 1)
            capture = {
              ...capture,
              revision: 2,
              completed: 4,
              state: "blocked",
              error: "Rate limited; persisted property cursor can resume",
              pending: [
                {
                  kind: "property",
                  id: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                  propertyId: "r%3Ai",
                  cursor: "opaque-next",
                  pageId: id(5),
                  parentId: id(6),
                  depth: 0,
                },
              ],
            };
          else
            capture = {
              ...capture,
              revision: 3,
              completed: 8,
              state: "ready",
              error: undefined,
              pending: [],
            };
          return ok(capture);
        }
        if (method === "imports.notion.status") return ok(capture);
        if (method === "imports.notion.finish") {
          capture = {
            ...capture,
            state: "finished",
            revision: capture.revision + 1,
          };
          return ok({
            planId: id(4),
            state: "preview",
            sourceCompleteness: "partial",
            items: [
              { kind: "portable_collection", title: "Projects" },
              { kind: "portable_collection", title: "People" },
            ],
          });
        }
        throw new Error(`Unexpected fixture dispatch ${method}`);
      },
    },
  },
};
function Fixture() {
  const [plan, setPlan] = useState<any>();
  return (
    <main>
      <h1>Capture migration source</h1>
      <NotionCapturePanel
        workspace={workspace}
        namespace="migration-fixture"
        onPlan={setPlan}
      />
      {plan && (
        <output role="status">
          Review inventory: {plan.items.map((i: any) => i.title).join(", ")} ·{" "}
          {plan.sourceCompleteness}
        </output>
      )}
    </main>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
