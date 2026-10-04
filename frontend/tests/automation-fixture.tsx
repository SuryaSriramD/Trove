import React from "react";
import { createRoot } from "react-dom/client";
import AutomationPanel from "../src/automation/AutomationPanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const w = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  contractVersion: "1.0.0",
  snapshotCursor: "5",
  contentMode: "shell",
  pages: [
    {
      id: id(3),
      title: "Rules",
      revision: "5",
      workspaceId: id(1),
      parentPageId: null,
      createdAt: "2026-10-01T00:00:00Z",
      updatedAt: "2026-10-01T00:00:00Z",
      deletedAt: null,
    },
    {
      id: id(4),
      title: "Project Alpha",
      revision: "3",
      workspaceId: id(1),
      parentPageId: null,
      createdAt: "2026-10-01T00:00:00Z",
      updatedAt: "2026-10-01T00:00:00Z",
      deletedAt: null,
    },
  ],
  collections: [
    {
      id: id(5),
      title: "Projects",
      revision: "2",
      owningPageId: id(3),
      titlePropertyId: id(6),
      systemRole: "none",
      workspaceId: id(1),
    },
  ],
  properties: [
    {
      id: id(7),
      collectionId: id(5),
      name: "Effort",
      type: "number",
      schemaVersion: 1,
      revision: "2",
      config: { format: "number" },
    },
    {
      id: id(8),
      collectionId: id(5),
      name: "Status",
      type: "status",
      schemaVersion: 1,
      revision: "2",
      config: {
        options: [
          { id: id(9), label: "Open", group: "todo" },
          { id: id(10), label: "Done", group: "done" },
        ],
      },
    },
    {
      id: id(11),
      collectionId: id(5),
      name: "Notes",
      type: "text",
      schemaVersion: 1,
      revision: "1",
      config: {},
    },
  ],
  records: [
    {
      id: id(12),
      pageId: id(4),
      collectionId: id(5),
      revision: "3",
      sortKey: "1",
      values: [],
    },
  ],
  taskFacets: [
    { recordId: id(12), statusPropertyId: id(8), duePropertyId: null },
  ],
  blocks: [],
  files: [],
  views: [],
  containers: [],
  containerCollections: [],
  meetings: [],
  transcriptSegments: [],
  memories: [],
  importMappings: [],
  projectScopes: [],
} as unknown as WorkspaceSnapshot;
const calls: any[] = [];
let rules: any[] = [];
let runs: any[] = [];
let serial = 100;
(window as any).automationCalls = calls;
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "allocateIds") return ok({ ids: [id(++serial)] });
        if (method === "automationOptions")
          return ok({
            templates: [
              {
                id: id(20),
                name: "Weekly project template",
                hash: "a".repeat(64),
                collectionId: id(5),
                sourcePageId: id(4),
              },
            ],
            templatesTruncated: false,
            events: [
              {
                id: id(30),
                origin: "user",
                at: "2026-10-04T10:00:00Z",
                revisions: [
                  { target: { entityType: "collection", entityId: id(5) } },
                ],
              },
            ],
          });
        if (method === "automationHistory") return ok({ rules, runs });
        if (method === "batch") {
          for (const c of request.commands) {
            if (c.kind === "automation.save") {
              const previous = rules.find((r) => r.rule.id === c.rule.id);
              rules = rules.filter((r) => r.rule.id !== c.rule.id);
              rules.push({
                rule: c.rule,
                version: (previous?.version ?? 0) + 1,
                nextAt: "2026-10-05T04:00:00Z",
                failures: 0,
              });
            } else if (c.kind === "automation.state") {
              const r = rules.find((r) => r.rule.id === c.ruleId);
              r.rule = { ...r.rule, enabled: c.enabled, paused: c.paused };
            }
          }
          return ok({
            transactionId: id(++serial),
            eventCursor: "6",
            revisions: [],
            replayed: false,
          });
        }
        if (method === "automationPreview") {
          if ((window as any).automationConflict)
            return JSON.stringify({
              ok: false,
              error: {
                code: "CONFLICT",
                message:
                  "Template properties need mapping; capture a compatible template for this database before scheduling",
                retryable: false,
                requestId: request.requestId,
              },
            });
          const r = rules.find((r) => r.rule.id === request.ruleId);
          const isEvent = r.rule.trigger.type === "event";
          return ok({
            eligible: !isEvent || request.eventId === id(30),
            diagnostic:
              !request.eventId && isEvent ? "Choose an eligible event" : "",
            commands: [
              {
                kind: "record.create",
                recordId: id(40),
                pageId: id(41),
                collectionId: id(5),
                title: "Weekly copy",
              },
              {
                kind: "block.insert",
                block: {
                  blockId: id(42),
                  pageId: id(41),
                  type: "paragraph",
                  schemaVersion: 1,
                  data: { richText: [{ text: "Independent body", marks: [] }] },
                },
              },
            ],
            expectedRevisions: [
              {
                target: { entityType: "page", entityId: id(3) },
                revision: "5",
              },
            ],
            occurrenceKey: isEvent
              ? `event:${request.eventId}`
              : "schedule:2026-10-05T04:00:00Z",
            at: "2026-10-05T04:00:00Z",
          });
        }
        if (method === "automation.execute") {
          runs = [
            {
              id: id(50),
              ruleId: request.ruleId,
              ruleVersion: rules.find((r) => r.rule.id === request.ruleId)
                .version,
              occurrenceKey: request.occurrenceKey,
              status: "succeeded",
              diagnostic: "",
              at: request.at,
            },
          ];
          return ok({});
        }
        throw new Error(`Unexpected fixture method ${method}`);
      },
    },
  },
};
createRoot(document.getElementById("root")!).render(
  <>
    <AutomationPanel
      workspace={w}
      onCommitted={async () => {}}
      onError={(e) => {
        document.getElementById("fixture-error")!.textContent = String(e);
      }}
    />
    <p role="alert" id="fixture-error" />
  </>,
);
