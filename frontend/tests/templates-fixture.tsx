import React from "react";
import { createRoot } from "react-dom/client";
import { TemplateLibrary } from "../src/advanced/TemplateLibrary";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const source = {
  id: id(2),
  workspaceId: id(1),
  parentPageId: id(3),
  title: "Weekly project",
  revision: "7",
  createdAt: "2026-10-04T10:00:00Z",
  updatedAt: "2026-10-04T10:00:00Z",
  deletedAt: null,
};
const property = {
  id: id(5),
  collectionId: id(4),
  name: "Effort",
  type: "number",
  schemaVersion: 1,
  config: { format: "number" },
  revision: "3",
};
const w = {
  workspaceId: id(1),
  scopeGrantId: id(6),
  snapshotCursor: "10",
  contractVersion: "1.0.0",
  contentMode: "shell",
  pages: [source],
  records: [
    {
      id: id(7),
      pageId: source.id,
      collectionId: id(4),
      revision: "7",
      sortKey: "1",
      values: [],
    },
  ],
  collections: [
    {
      id: id(4),
      title: "Projects",
      revision: "2",
      owningPageId: id(3),
      titlePropertyId: id(8),
      systemRole: "none",
      workspaceId: id(1),
    },
  ],
  properties: [property],
  blocks: [],
  files: [],
  views: [],
  containers: [],
  containerCollections: [],
  meetings: [],
  transcriptSegments: [],
  memories: [],
  taskFacets: [],
  importMappings: [],
} as unknown as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).templateCalls = calls;
let serial = 20;
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "editor.templates")
          return ok({
            templates: [
              {
                id: id(10),
                name: "Weekly combined template",
                sourcePageId: source.id,
              },
            ],
          });
        if (method === "editor.saveTemplate") return ok({ id: id(11) });
        if (method === "template.preview") {
          if ((window as any).staleTemplate) {
            (window as any).staleTemplate = false;
            return JSON.stringify({
              ok: false,
              error: {
                code: "CONFLICT",
                message: "Destination schema changed; preview again.",
                retryable: false,
                requestId: request.requestId,
              },
            });
          }
          const needMapping =
            (window as any).templateNeedsMapping &&
            !request.propertyDecisions.length;
          const previewId = id(++serial);
          return ok({
            previewId,
            pageId: id(30),
            recordId: id(31),
            expiresAt: "2030-10-04T11:00:00Z",
            expectedRevisions: [
              {
                target: { entityType: "collection", entityId: id(4) },
                revision: "2",
              },
              {
                target: { entityType: "page", entityId: id(30) },
                revision: "0",
              },
            ],
            command: { kind: "template.apply", previewId },
            blockCount: 5,
            valueCount: 2,
            taskCount: 1,
            warnings: [],
            propertyIssues: needMapping
              ? [
                  {
                    sourceProperty: property,
                    value: { decimal: "8", format: "number" },
                    reason: "Choose an explicit current typed value.",
                  },
                ]
              : [],
            targetProperties: [property],
            canApply: !needMapping,
          });
        }
        if (method === "batch")
          return ok({
            transactionId: id(50),
            eventCursor: "11",
            revisions: [],
            replayed: false,
          });
        throw new Error(`Unexpected template fixture method: ${method}`);
      },
    },
  },
};
createRoot(document.getElementById("root")!).render(
  <>
    <TemplateLibrary
      workspace={w}
      sourcePages={[source]}
      collectionId={id(4)}
      onCommitted={async () => {}}
      onError={(error) => {
        document.getElementById("fixture-error")!.textContent = String(error);
      }}
    />
    <p id="fixture-error" role="alert" />
  </>,
);
