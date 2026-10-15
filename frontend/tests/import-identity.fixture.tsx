import React from "react";
import { createRoot } from "react-dom/client";
import { ImportExportPanel } from "../src/imports/ImportExportPanel";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace: any = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [{ id: id(3), title: "Existing notes", revision: "4" }],
  collections: [],
  records: [],
  blocks: [],
  properties: [],
  files: [],
  taskFacets: [],
  memories: [],
  containers: [],
  containerCollections: [],
  meetings: [],
  transcriptSegments: [],
  views: [],
};
const initial = {
  planId: id(4),
  planHash: "a".repeat(64),
  sourceNamespace: "manual-import",
  sourceName: "renamed.zip",
  rootPageId: id(5),
  sourceFileId: id(6),
  state: "preview",
  cursor: 0,
  initialized: false,
  sourceCompleteness: "unknown",
  sourceOmissions: [],
  unknownCompleteness: [],
  items: [
    {
      sourceLocator: "Renamed.html",
      kind: "html",
      outcome: "partial",
      reason: "Identity requires review",
      hash: "b".repeat(64),
      identityKind: "derived_locator",
      externalId: "Renamed.html",
      pageId: id(7),
      title: "Renamed",
      conflict: "Review source identity",
      committed: false,
      identityReview: {
        version: 1,
        reason:
          "This locator has no accepted mapping in an existing source namespace",
        newPageId: id(7),
      },
    },
  ],
};
let plan: any =
  JSON.parse(localStorage.getItem("identity-plan") ?? "null") ?? initial;
const calls: any[] = [];
(window as any).identityCalls = calls;
(window as any).go = {
  main: {
    App: {
      PickFile: async () => id(6),
      Dispatch: async (method: string, raw: string) => {
        const req = JSON.parse(raw);
        calls.push({ method, req });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "imports.list")
          return ok([
            {
              planId: plan.planId,
              sourceName: plan.sourceName,
              state: plan.state,
              cursor: plan.cursor,
              total: 1,
              createdAt: "2026-10-15T00:00:00Z",
            },
          ]);
        if (method === "imports.plan") return ok(plan);
        if (method === "imports.configure") {
          if ((window as any).identityStale)
            return JSON.stringify({
              ok: false,
              error: {
                code: "CONFLICT",
                message:
                  "Selected local page changed; review its current revision",
              },
            });
          const item = plan.items[0];
          if (req.identityResolutions) {
            const r = req.identityResolutions[0],
              choice = r.identityChoice;
            item.identityReview.choice = choice.choice;
            item.identityReview.localId = choice.localId;
            item.conflict = "";
            item.choice = "";
            if (choice.choice === "link_existing") {
              item.pageId = choice.localId;
              item.conflict = "Review every field explicitly";
              item.merge = {
                baselineStatus: "unavailable",
                localDeleted: false,
                fields: [
                  {
                    path: "/title",
                    outcome: "conflict",
                    base: { present: false },
                    local: { present: true, value: "Existing notes" },
                    incoming: { present: true, value: "Renamed" },
                  },
                ],
                resolutions: [],
              };
            }
            if (choice.choice === "skip") item.choice = "keep_local";
          }
          if (req.resolutions) {
            item.merge.resolutions = req.resolutions[0].fields;
            item.choice = "merge";
          }
          if (req.choices) item.choice = req.choices[0].choice;
          plan.planHash = "c".repeat(64);
          localStorage.setItem("identity-plan", JSON.stringify(plan));
          return ok(plan);
        }
        if (method === "imports.commit") {
          plan.state = "complete";
          plan.cursor = 1;
          plan.initialized = true;
          localStorage.setItem("identity-plan", JSON.stringify(plan));
          return ok(plan);
        }
        if (method === "imports.notion.list") return ok([]);
        if (method === "imports.preview") return ok(initial);
        throw Error(`Unexpected fixture call ${method}`);
      },
    },
  },
};
createRoot(document.getElementById("root")!).render(
  <ImportExportPanel
    workspace={workspace}
    initialPlanId={id(4)}
    onChanged={() => {}}
  />,
);
