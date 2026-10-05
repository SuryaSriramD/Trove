import React from "react";
import { createRoot } from "react-dom/client";
import { AIPanel } from "../src/ai/AIPanel";
import { ProviderPanel } from "../src/providers/ProviderPanel";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace: any = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [{ id: id(3), title: "Launch", revision: "1" }],
  blocks: [],
  collections: [],
  properties: [],
  records: [],
  views: [],
  containers: [],
  memories: [],
  taskFacets: [],
};
const run = {
  id: id(4),
  conversationId: id(5),
  state: "completed",
  request: {
    mode: "workspace_agent",
    prompt: "Create a checklist under Launch",
    useMemory: false,
  },
  text: JSON.stringify({
    version: 1,
    actions: [
      {
        kind: "create_page",
        destination: "Launch",
        title: "Checklist",
        body: "Verify restored files.",
      },
    ],
  }),
  citations: [],
  sources: [],
  createdAt: "2026-10-04T10:00:00Z",
};
const change = {
  label: "Create page: Checklist",
  destination: "Launch",
  after: "Verify restored files.",
};
(window as any).proposalCalls = [];
(window as any).go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const request = JSON.parse(raw || "{}");
        (window as any).proposalCalls.push({ method, request });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "memory.list") return ok({ entries: [] });
        if (method === "models.list") return ok({ packs: [] });
        if (method === "ai.runs") return ok([run]);
        if (method === "ai.run") return ok(run);
        if (method === "ai.prepareAction")
          return ok({
            id: id(6),
            runId: run.id,
            state: "draft",
            kind: request.kind,
            title: "Checklist",
            body: run.text,
            after: "Create page: Checklist → Launch\nVerify restored files.",
            changes: [change],
            commands: [{ kind: "page.create", pageId: id(7) }],
            pageId: id(7),
          });
        if (method === "ai.applyAction")
          return ok({ pageId: "", state: "applied" });
        if (method === "providers.remote.status")
          return ok({
            enabled: false,
            origin: "",
            bindAddress: "",
            certificateExpiresAt: "",
            connections: [],
            authorizations: [],
            message: "",
          });
        if (method === "providers.status")
          return ok({ connections: [], capabilities: [] });
        if (method === "providers.proposals")
          return ok([
            {
              id: "paired-plan",
              connectionId: id(8),
              state: "pending",
              changes: [change],
              request: {
                commands: [
                  { kind: "page.create", pageId: id(7), parentPageId: id(3) },
                ],
              },
            },
          ]);
        if (method === "providers.approve" || method === "providers.reject")
          return ok({});
        return ok([]);
      },
    },
  },
};
createRoot(document.getElementById("root")!).render(
  <>
    <AIPanel
      workspace={workspace}
      onNavigate={() => {}}
      onCommitted={() => {}}
    />
    <ProviderPanel workspace={workspace} onCommitted={() => {}} />
  </>,
);
